// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	rev := &issues_model.Review{IssueID: top.IssueID, ReviewerID: 1, Type: issues_model.ReviewTypeApprove, CommitID: headTop, Stale: true}
	_, err := db.GetEngine(t.Context()).Insert(rev)
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

	// patch_unchanged: the approval follows the new head and is no longer stale.
	got := unittest.AssertExistsAndLoadBean(t, &issues_model.Review{ID: rev.ID})
	assert.Equal(t, restackedHead, got.CommitID)
	assert.False(t, got.Stale)
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
