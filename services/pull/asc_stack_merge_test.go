// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ascMergeBottom = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	ascMergeTop    = "llllllllllllllllllllllllllllllll"
	headBottom     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headTop        = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	landedBottom   = "cccccccccccccccccccccccccccccccccccccccc"
	landedTop      = "dddddddddddddddddddddddddddddddddddddddd"
	newTip         = landedTop
	restackedHead  = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

type fakeAscStack struct {
	lands    []AscLandRequest
	restacks []AscRestackRequest
	landErr  error
	outcome  []AscRestackOutcome
}

func (f *fakeAscStack) Land(_ context.Context, req AscLandRequest) (*AscLandResult, error) {
	f.lands = append(f.lands, req)
	if f.landErr != nil {
		return nil, f.landErr
	}
	all := []string{landedBottom, landedTop}
	return &AscLandResult{Landed: all[len(all)-len(req.Heads):], Tip: newTip}, nil
}

func (f *fakeAscStack) Restack(_ context.Context, req AscRestackRequest) ([]AscRestackOutcome, error) {
	f.restacks = append(f.restacks, req)
	return f.outcome, nil
}

// ascMergeFixture links PR 2 (bottom) and PR 5 (top) of repository 1 into a stack on master.
func ascMergeFixture(t *testing.T) (bottom, top *issues_model.PullRequest, fake *fakeAscStack) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	bottom = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	top = unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	top.BaseBranch = "master"
	_, err := db.GetEngine(ctx).ID(top.ID).Cols("base_branch").Update(top)
	require.NoError(t, err)
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, bottom, ascMergeBottom, 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, top, ascMergeTop, bottom.ID))

	heads := map[int64]string{bottom.ID: headBottom, top.ID: headTop}
	prev := ascStackHead
	ascStackHead = func(_ context.Context, pr *issues_model.PullRequest) (string, error) { return heads[pr.ID], nil }
	t.Cleanup(func() { ascStackHead = prev })
	fake = &fakeAscStack{}
	t.Cleanup(UseAscStackBackend(fake))
	return bottom, top, fake
}

func ascMergePerm(t *testing.T, pr *issues_model.PullRequest, userID int64) (*user_model.User, access_model.Permission) {
	t.Helper()
	require.NoError(t, pr.LoadBaseRepo(t.Context()))
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: userID})
	p, err := access_model.GetUserRepoPermission(t.Context(), pr.BaseRepo, u)
	require.NoError(t, err)
	return u, p
}

func TestAscStackMergePrefix(t *testing.T) {
	bottom, top, _ := ascMergeFixture(t)
	items, err := AscStackPrefix(t.Context(), top)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, bottom.ID, items[0].PR.ID)
	assert.Equal(t, top.ID, items[1].PR.ID)

	// A partial merge: N is the bottom, so the prefix is just it.
	items, err = AscStackPrefix(t.Context(), bottom)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// A cycle ends the walk with an error, never a loop.
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), bottom, ascMergeBottom, top.ID))
	_, err = AscStackPrefix(t.Context(), top)
	require.ErrorIs(t, err, ErrAscStackNotMergable)
}

func TestAscStackMergeLandsPrefixAtomically(t *testing.T) {
	_, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	res, err := MergeAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	require.Len(t, fake.lands, 1, "one atomic land call")
	assert.Equal(t, []string{headBottom, headTop}, fake.lands[0].Heads)
	assert.Equal(t, "refs/heads/master", fake.lands[0].Target)
	require.Len(t, res.Merged, 2)
	assert.Equal(t, landedBottom, res.Merged[0].MergedCommitID)
	assert.Equal(t, landedTop, res.Merged[1].MergedCommitID)
	for _, id := range []int64{2, 5} {
		assert.True(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: id}).HasMerged)
	}
	assert.Empty(t, fake.restacks, "no open child, no restack call")
}

func TestAscStackMergePartialRestacksChildren(t *testing.T) {
	bottom, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, bottom, 2)
	fake.outcome = []AscRestackOutcome{{Change: ascMergeTop, NewHead: restackedHead, PatchUnchanged: true}}

	// An approval of the top on its old head.
	rev := &issues_model.Review{IssueID: top.IssueID, ReviewerID: 1, Type: issues_model.ReviewTypeApprove, CommitID: headTop}
	_, err := db.GetEngine(t.Context()).Insert(rev)
	require.NoError(t, err)
	// A stale approval of the same head is not carried forward and stays stale.
	staleRev := &issues_model.Review{IssueID: top.IssueID, ReviewerID: 2, Type: issues_model.ReviewTypeApprove, CommitID: headTop, Stale: true}
	_, err = db.GetEngine(t.Context()).Insert(staleRev)
	require.NoError(t, err)

	res, err := MergeAscStack(t.Context(), doer, perm, bottom)
	require.NoError(t, err)
	require.Len(t, res.Merged, 1, "exactly 1..N lands")
	assert.Equal(t, []string{headBottom}, fake.lands[0].Heads)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5}).HasMerged)

	require.Len(t, fake.restacks, 1)
	rs := fake.restacks[0]
	assert.Equal(t, newTip, rs.Onto)
	assert.Equal(t, doer.GetEmail(), rs.CommitterEmail)
	assert.NotZero(t, rs.CommitterTime)
	require.Len(t, rs.Items, 1)
	assert.Equal(t, ascMergeTop, rs.Items[0].Change)
	assert.Equal(t, top.GetGitRefName(), rs.Items[0].UpdateRef)
	assert.Equal(t, headTop, rs.Items[0].ExpectHead)

	// patch_unchanged: the live approval of the old head follows the new head.
	got := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: rev.ID})
	assert.Equal(t, restackedHead, got.CommitID)
	assert.False(t, got.Stale)
	gotStale := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: staleRev.ID})
	assert.Equal(t, headTop, gotStale.CommitID, "a stale approval does not follow")
	assert.True(t, gotStale.Stale, "and is not made live again")
}

func TestAscStackMergeApprovalDroppedWhenPatchChanged(t *testing.T) {
	bottom, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, bottom, 2)
	fake.outcome = []AscRestackOutcome{{Change: ascMergeTop, NewHead: restackedHead}}
	rev := &issues_model.Review{IssueID: top.IssueID, ReviewerID: 1, Type: issues_model.ReviewTypeApprove, CommitID: headTop}
	_, err := db.GetEngine(t.Context()).Insert(rev)
	require.NoError(t, err)

	_, err = MergeAscStack(t.Context(), doer, perm, bottom)
	require.NoError(t, err)
	got := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: rev.ID})
	assert.Equal(t, headTop, got.CommitID, "a changed patch does not keep the approval")
}

// Security: a user without write access cannot merge a stack, and asc is never called.
func TestAscStackMergeRefusesUserWithoutWrite(t *testing.T) {
	_, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 5)
	require.False(t, perm.CanWrite(unit.TypeCode))

	_, err := MergeAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscStackNotAllowed)
	assert.Empty(t, fake.lands)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2}).HasMerged)

	_, err = MergeAscStack(t.Context(), nil, perm, top)
	require.ErrorIs(t, err, ErrAscStackNotAllowed)
}

// Security: a head that moved after the review is refused, so unreviewed content never lands.
func TestAscStackMergeRefusesMovedHead(t *testing.T) {
	_, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	top.HeadCommitID = "ffffffffffffffffffffffffffffffffffffffff"
	_, err := db.GetEngine(t.Context()).ID(top.ID).Cols("head_commit_id").Update(top)
	require.NoError(t, err)

	_, err = MergeAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscStackHeadMoved)
	assert.Empty(t, fake.lands)
}

func TestAscStackMergeRefusesClosedAndLandFailure(t *testing.T) {
	bottom, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	fake.landErr = errors.New("LAND_CONFLICT")
	_, err := MergeAscStack(t.Context(), doer, perm, top)
	require.ErrorContains(t, err, "LAND_CONFLICT")
	for _, id := range []int64{bottom.ID, top.ID} {
		assert.False(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: id}).HasMerged, "nothing is marked merged when asc refuses")
	}

	fake.landErr = nil
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `issue` SET is_closed=? WHERE id=?", true, bottom.IssueID)
	require.NoError(t, err)
	fake.lands = nil
	_, err = MergeAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscStackNotMergable)
	assert.Empty(t, fake.lands)
}

func TestAscStackMergeNeedsBackend(t *testing.T) {
	_, top, _ := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	t.Cleanup(UseAscStackBackend(nil))
	_, err := MergeAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscStackDisabled)
}

func TestAscChangeBytes(t *testing.T) {
	b, err := ascChangeBytes("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 16), b)
	b, err = ascChangeBytes("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	require.NoError(t, err)
	assert.Equal(t, byte(0xff), b[0])
	_, err = ascChangeBytes("abc")
	require.Error(t, err)
}

func TestAscStackMergeCarriesNoContentMerge(t *testing.T) {
	_, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	_, err := MergeAscStackWith(t.Context(), doer, perm, top, AscMergeOptions{NoContentMerge: true})
	require.NoError(t, err)
	require.Len(t, fake.lands, 1)
	assert.True(t, fake.lands[0].NoContentMerge)
}

func TestAscStackMergePlainEntryDoesNotSetTheFlag(t *testing.T) {
	_, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	_, err := MergeAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	require.Len(t, fake.lands, 1)
	assert.False(t, fake.lands[0].NoContentMerge)
}

// A land that needs a content merge lands nothing and is reported as such; no pull request is marked merged.
func TestAscStackMergeNeedsContentMerge(t *testing.T) {
	bottom, top, fake := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	fake.landErr = ErrAscLandNeedsContentMerge
	_, err := MergeAscStackWith(t.Context(), doer, perm, top, AscMergeOptions{NoContentMerge: true})
	require.ErrorIs(t, err, ErrAscLandNeedsContentMerge)
	for _, id := range []int64{bottom.ID, top.ID} {
		assert.False(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: id}).HasMerged)
	}
}

// fakeRepoAPI answers what a land of a chain of heads asks of asc.Repo.
type fakeRepoAPI struct {
	ascv1.RepoClient // nil: any other call panics

	updateErr error
	updates   []*ascv1.UpdateRefsRequest
}

func ascOfGit(g []byte) []byte { return append(bytes.Clone(g), make([]byte, 12)...) }

func (f *fakeRepoAPI) GitMap(_ context.Context, in *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	resp := &ascv1.GitMapResponse{}
	for _, g := range in.GetGit() {
		resp.GitToAsc = append(resp.GitToAsc, ascOfGit(g))
	}
	for _, a := range in.GetAsc() {
		resp.AscToGit = append(resp.AscToGit, bytes.Clone(a[:20]))
	}
	return resp, nil
}

func (f *fakeRepoAPI) MergeBase(_ context.Context, in *ascv1.MergeBaseRequest, _ ...grpc.CallOption) (*ascv1.MergeBaseResponse, error) {
	return &ascv1.MergeBaseResponse{Bases: [][]byte{in.GetA()}}, nil
}

func (f *fakeRepoAPI) UpdateRefs(_ context.Context, in *ascv1.UpdateRefsRequest, _ ...grpc.CallOption) (*ascv1.UpdateRefsResponse, error) {
	f.updates = append(f.updates, in)
	return &ascv1.UpdateRefsResponse{}, f.updateErr
}

func TestAscClientBackendLandSendsTheFlagAndMapsTheAnswer(t *testing.T) {
	api := &fakeRepoAPI{}
	b := &ascClientBackend{repo: api}
	req := AscLandRequest{Repo: "o/r", Target: "refs/heads/master", Heads: []string{headBottom, headTop}, NoContentMerge: true}

	res, err := b.Land(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, api.updates, 1)
	u := api.updates[0]
	assert.True(t, u.GetNoContentMerge())
	assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_LAND, u.GetMode())
	assert.Equal(t, "refs/heads/master", u.GetUpdates()[0].GetName())
	topAsc, err := hex.DecodeString(headTop)
	require.NoError(t, err)
	assert.Equal(t, ascOfGit(topAsc), u.GetUpdates()[0].GetNew().GetChangeset())
	assert.Equal(t, []string{headBottom, headTop}, res.Landed)

	req.NoContentMerge = false
	_, err = b.Land(t.Context(), req)
	require.NoError(t, err)
	assert.False(t, api.updates[1].GetNoContentMerge())

	// LAND_NEEDS_CONTENT_MERGE is its own error; any other failure is not mistaken for it.
	st, err := status.New(codes.FailedPrecondition, "secret").WithDetails(&ascv1.ErrorDetail{Code: "LAND_NEEDS_CONTENT_MERGE"})
	require.NoError(t, err)
	api.updateErr = st.Err()
	req.NoContentMerge = true
	_, err = b.Land(t.Context(), req)
	require.ErrorIs(t, err, ErrAscLandNeedsContentMerge)

	api.updateErr = status.Error(codes.Aborted, "something else")
	_, err = b.Land(t.Context(), req)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrAscLandNeedsContentMerge)
}

func TestInitAscStackFailsClosed(t *testing.T) {
	// With asc off, nothing is registered.
	prev := ascStackBackend
	t.Cleanup(func() { ascStackBackend = prev })
	ascStackBackend = nil
	require.NoError(t, InitAscStack())
	assert.Nil(t, ascStackBackend)

	// With asc on but no internal token the backend stays unset (every stack operation then fails closed)
	// and the hub still starts.
	oldEnabled, oldToken := setting.Asc.Enabled, setting.InternalToken
	t.Cleanup(func() { setting.Asc.Enabled, setting.InternalToken = oldEnabled, oldToken })
	setting.Asc.Enabled, setting.InternalToken = true, ""
	require.NoError(t, InitAscStack())
	assert.Nil(t, ascStackBackend)
	assert.False(t, AscStackMergeReady(&issues_model.PullRequest{}))
}

func TestAscStackMergeWaitsForTheGitDirectoryBeforeNotifying(t *testing.T) {
	_, top, _ := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	var seen []string
	prev := ascWaitLanded
	ascWaitLanded = func(_ context.Context, repo *repo_model.Repository, ref, tip string) {
		// the pull requests are marked merged with the landed ids asc reported, and the wait names the landed tip
		seen = append(seen, ref+"="+tip)
		assert.True(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: top.ID}).HasMerged)
		assert.NotNil(t, repo)
	}
	defer func() { ascWaitLanded = prev }()

	_, err := MergeAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/master=" + landedTop}, seen)
}
