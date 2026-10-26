// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascbridge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	revertChangeBottom = "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm"
	revertChangeTop    = "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
	revertGitBottom    = "1111111111111111111111111111111111111111"
	revertGitTop       = "2222222222222222222222222222222222222222"
)

// fakeStackOps is the Stacks client of the pull tests.
type fakeStackOps struct {
	restackReqs []ascbridge.RestackStackRequest
	restackDry  *ascbridge.RestackStackResult
	restackRun  *ascbridge.RestackStackResult
	restackErr  error // returned by the call that is not a dry run
	dryErr      error

	revertReqs []ascbridge.RevertLandedRequest
	revertDry  *ascbridge.RevertLandedResult
	revertRun  *ascbridge.RevertLandedResult
	revertErr  error
}

func (f *fakeStackOps) RestackStack(_ context.Context, req ascbridge.RestackStackRequest) (*ascbridge.RestackStackResult, error) {
	f.restackReqs = append(f.restackReqs, req)
	if req.DryRun {
		return f.restackDry, f.dryErr
	}
	return f.restackRun, f.restackErr
}

func (f *fakeStackOps) RevertLanded(_ context.Context, req ascbridge.RevertLandedRequest) (*ascbridge.RevertLandedResult, error) {
	f.revertReqs = append(f.revertReqs, req)
	if req.DryRun {
		return f.revertDry, f.revertErr
	}
	return f.revertRun, f.revertErr
}

func useStackOps(t *testing.T, ops ascbridge.StackOps) {
	t.Helper()
	t.Cleanup(ascbridge.UseStackOps(ops))
	prev := ascStackBacked
	ascStackBacked = func(*repo_model.Repository) (bool, error) { return true, nil }
	t.Cleanup(func() { ascStackBacked = prev })
}

type openedRevert struct {
	plan   AscRevertPlan
	rv     ascbridge.RevertedChange
	parent int64
	branch string
}

// ascRevertFixture links PR 2 (bottom) and PR 5 (top) as a landed stack and fakes the Stacks client and the
// opening of pull requests.
func ascRevertFixture(t *testing.T) (bottom, top *issues_model.PullRequest, ops *fakeStackOps, opened *[]openedRevert) {
	t.Helper()
	bottom, top, _ = ascMergeFixture(t)
	ctx := t.Context()
	for _, p := range []struct {
		pr *issues_model.PullRequest
		id string
	}{{bottom, landedBottom}, {top, landedTop}} {
		p.pr.HasMerged, p.pr.MergedCommitID = true, p.id
		_, err := db.GetEngine(ctx).ID(p.pr.ID).Cols("has_merged", "merged_commit_id").Update(p.pr)
		require.NoError(t, err)
	}
	ops = &fakeStackOps{}
	ops.revertRun = &ascbridge.RevertLandedResult{
		Ref: "refs/users/user2/reverts", Applied: true, StackTop: revertChangeTop,
		Reverts: []ascbridge.RevertedChange{
			{Original: ascMergeTop, RevertChange: revertChangeBottom, RevertGit: revertGitBottom},
			{Original: ascMergeBottom, RevertChange: revertChangeTop, RevertGit: revertGitTop},
		},
	}
	ops.revertDry = &ascbridge.RevertLandedResult{Reverts: []ascbridge.RevertedChange{{Original: ascMergeTop}, {Original: ascMergeBottom}}}
	useStackOps(t, ops)

	made := []*issues_model.PullRequest{
		unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 3}),
		unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 4}),
	}
	opened = &[]openedRevert{}
	prev := ascOpenRevertPull
	ascOpenRevertPull = func(_ context.Context, doer *user_model.User, _ *repo_model.Repository, base string, plan AscRevertPlan, rv ascbridge.RevertedChange, parent int64) (*issues_model.PullRequest, error) {
		*opened = append(*opened, openedRevert{plan: plan, rv: rv, parent: parent, branch: base})
		if len(*opened) > len(made) {
			return nil, errors.New("no more")
		}
		return made[len(*opened)-1], nil
	}
	t.Cleanup(func() { ascOpenRevertPull = prev })
	return bottom, top, ops, opened
}

func TestAscLandedStackNewestFirst(t *testing.T) {
	bottom, top, _, _ := ascRevertFixture(t)
	items, err := AscLandedStack(t.Context(), top)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, top.ID, items[0].PR.ID)
	assert.Equal(t, ascMergeTop, items[0].Change)
	assert.Equal(t, bottom.ID, items[1].PR.ID)
	items, err = AscLandedStack(t.Context(), bottom)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestAscRevertStackOpensAStackOfPullRequests(t *testing.T) {
	bottom, top, ops, opened := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	res, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.NoError(t, err)
	require.NoError(t, res.Err)
	require.Len(t, ops.revertReqs, 1)
	req := ops.revertReqs[0]
	assert.Equal(t, "user2/repo1", req.Repo)
	assert.Equal(t, ascMergeTop, req.Change, "asc is asked from the top change down")
	assert.Empty(t, req.Bottom)
	assert.False(t, req.DryRun)
	assert.False(t, req.Squash)

	// One pull request per revert, bottom of the new stack first, each stacked on the one below.
	require.Len(t, *opened, 2)
	assert.Equal(t, revertChangeBottom, (*opened)[0].rv.RevertChange)
	assert.EqualValues(t, 0, (*opened)[0].parent)
	assert.Equal(t, []int64{top.ID}, []int64{(*opened)[0].plan.Original[0].ID})
	assert.Equal(t, revertChangeTop, (*opened)[1].rv.RevertChange)
	assert.EqualValues(t, 3, (*opened)[1].parent, "the second revert stacks on the first pull request")
	assert.Equal(t, bottom.ID, (*opened)[1].plan.Original[0].ID)
	assert.Equal(t, "master", (*opened)[0].branch, "the pull requests target the branch that was reverted; it is not written")
	require.Len(t, res.Created, 2)
	assert.Equal(t, "refs/users/user2/reverts", res.Ref)
	assert.True(t, res.Applied)
}

func TestAscRevertStackScopeSquashAndBottom(t *testing.T) {
	bottom, top, ops, opened := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	// Only the top pull request.
	ops.revertRun = &ascbridge.RevertLandedResult{Applied: true, Ref: "r", Reverts: []ascbridge.RevertedChange{
		{Original: ascMergeTop, RevertChange: revertChangeBottom, RevertGit: revertGitBottom}}}
	res, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{Bottom: top})
	require.NoError(t, err)
	assert.Equal(t, ascMergeTop, ops.revertReqs[0].Bottom, "the bottom of the selection is the top change itself")
	assert.Len(t, res.Selected, 1)
	assert.Len(t, *opened, 1)

	// From the top down to the bottom pull request, explicitly.
	*opened = nil
	ops.revertRun = &ascbridge.RevertLandedResult{Applied: true, Ref: "r", Reverts: []ascbridge.RevertedChange{
		{Original: ascMergeTop, RevertChange: revertChangeBottom, RevertGit: revertGitBottom},
		{Original: ascMergeBottom, RevertChange: revertChangeTop, RevertGit: revertGitTop}}}
	res, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{Bottom: bottom})
	require.NoError(t, err)
	assert.Equal(t, ascMergeBottom, ops.revertReqs[1].Bottom)
	assert.Len(t, res.Selected, 2)

	// A pull request that is not part of the landed stack is refused before asc is called.
	other := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 3})
	n := len(ops.revertReqs)
	_, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{Bottom: other})
	require.ErrorIs(t, err, ErrAscRevertBottom)
	assert.Len(t, ops.revertReqs, n)

	// Squash: one revert for the whole selection, titled after the newest pull request.
	*opened = nil
	ops.revertRun = &ascbridge.RevertLandedResult{Applied: true, Ref: "r", Reverts: []ascbridge.RevertedChange{
		{Original: ascMergeTop, RevertChange: revertChangeBottom, RevertGit: revertGitBottom}}}
	res, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{Squash: true})
	require.NoError(t, err)
	assert.True(t, ops.revertReqs[len(ops.revertReqs)-1].Squash)
	require.Len(t, *opened, 1)
	require.Len(t, (*opened)[0].plan.Original, 2)
	assert.Equal(t, bottom.ID, (*opened)[0].plan.Original[0].ID, "bottom first")
	assert.Equal(t, top.ID, (*opened)[0].plan.Original[1].ID)
	require.Len(t, res.Created, 1)
}

func TestAscRevertStackDryRunWritesNothing(t *testing.T) {
	_, top, ops, opened := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	ops.revertDry = &ascbridge.RevertLandedResult{Reverts: []ascbridge.RevertedChange{
		{Original: ascMergeTop, Conflicts: []string{"a.go"}}, {Original: ascMergeBottom}}}
	res, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{DryRun: true})
	require.NoError(t, err)
	assert.True(t, ops.revertReqs[0].DryRun)
	assert.Empty(t, *opened)
	assert.Empty(t, res.Created)
	assert.False(t, res.Applied)
	assert.True(t, res.HasConflicts())
	require.Len(t, res.Plans, 2)
	assert.Equal(t, []string{"a.go"}, res.Plans[0].Conflicts)

	// A dry run does not take the in-flight guard, so a preview works while a revert runs.
	ascRevertRunning.Store(top.ID, struct{}{})
	t.Cleanup(func() { ascRevertRunning.Delete(top.ID) })
	_, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{DryRun: true})
	require.NoError(t, err)
	_, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertBusy)
	assert.Empty(t, *opened)
}

// Security: asc's answer is checked against the selection before any pull request is opened.
func TestAscRevertStackVerifiesTheAnswer(t *testing.T) {
	_, top, ops, opened := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	good := ops.revertRun.Reverts
	for name, rs := range map[string][]ascbridge.RevertedChange{
		"too few":                   good[:1],
		"too many":                  append(append([]ascbridge.RevertedChange{}, good...), good[0]),
		"unknown change":            {good[0], {Original: ascMergeTop[:31] + "z", RevertChange: revertChangeTop, RevertGit: revertGitTop}},
		"repeated change":           {good[0], {Original: ascMergeTop, RevertChange: revertChangeTop, RevertGit: revertGitTop}},
		"revert is a landed change": {good[0], {Original: ascMergeBottom, RevertChange: ascMergeBottom, RevertGit: revertGitTop}},
		"same new change twice":     {good[0], {Original: ascMergeBottom, RevertChange: revertChangeBottom, RevertGit: revertGitTop}},
		"short commit":              {good[0], {Original: ascMergeBottom, RevertChange: revertChangeTop, RevertGit: "abc"}},
		"bad new change":            {good[0], {Original: ascMergeBottom, RevertChange: "short", RevertGit: revertGitTop}},
	} {
		ops.revertRun = &ascbridge.RevertLandedResult{Applied: true, Ref: "r", Reverts: rs}
		_, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
		require.ErrorIs(t, err, ErrAscRevertBadAnswer, name)
	}
	assert.Empty(t, *opened, "nothing is opened from a bad answer")

	ops.revertRun = &ascbridge.RevertLandedResult{Applied: false, Reverts: good}
	_, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertBadAnswer, "an answer that was not applied")
}

func TestAscRevertStackReportsPartialOpening(t *testing.T) {
	_, top, _, opened := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	prev := ascOpenRevertPull
	n := 0
	ascOpenRevertPull = func(ctx context.Context, d *user_model.User, r *repo_model.Repository, base string, p AscRevertPlan, rv ascbridge.RevertedChange, parent int64) (*issues_model.PullRequest, error) {
		n++
		if n == 2 {
			return nil, errors.New("disk full")
		}
		return prev(ctx, d, r, base, p, rv, parent)
	}
	t.Cleanup(func() { ascOpenRevertPull = prev })
	res, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.NoError(t, err, "the reverts exist in asc: the result carries the failure")
	require.Error(t, res.Err)
	assert.Len(t, res.Created, 1)
	assert.Len(t, *opened, 1)
	assert.Contains(t, res.Err.Error(), "revert 2")
}

// Security: who may revert, and when asc is not called at all.
func TestAscRevertStackRefusals(t *testing.T) {
	bottom, top, ops, _ := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	reader, readerPerm := ascMergePerm(t, top, 5)
	require.False(t, readerPerm.CanWrite(unit.TypeCode))
	_, err := RevertAscStack(t.Context(), reader, readerPerm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
	_, err = RevertAscStack(t.Context(), nil, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
	inactive := *doer
	inactive.IsActive = false
	_, err = RevertAscStack(t.Context(), &inactive, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
	prohibited := *doer
	prohibited.ProhibitLogin = true
	_, err = RevertAscStack(t.Context(), &prohibited, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
	assert.False(t, AscMayRevert(nil, perm))

	// An archived repository takes no new pull requests.
	require.NoError(t, top.LoadBaseRepo(t.Context()))
	top.BaseRepo.IsArchived = true
	_, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
	top.BaseRepo.IsArchived = false

	// A pull request of the stack that is not merged.
	bottom.HasMerged = false
	_, err = db.GetEngine(t.Context()).ID(bottom.ID).Cols("has_merged").Update(bottom)
	require.NoError(t, err)
	_, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertNotLanded)
	bottom.HasMerged = true
	_, err = db.GetEngine(t.Context()).ID(bottom.ID).Cols("has_merged").Update(bottom)
	require.NoError(t, err)

	// Not stacked.
	lone := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 3})
	_, err = RevertAscStack(t.Context(), doer, perm, lone, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscStackNotStacked)

	// A repository that is not in asc.
	ascStackBacked = func(*repo_model.Repository) (bool, error) { return false, nil }
	_, err = RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
	require.ErrorIs(t, err, ErrAscRevertDisabled)

	assert.Empty(t, ops.revertReqs, "asc was not called by any refused request")
}

func TestAscRevertStackPassesAscErrorsOn(t *testing.T) {
	_, top, ops, opened := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	for _, want := range []error{ascbridge.ErrRevertConflict, ascbridge.ErrStackNotLanded, ascbridge.ErrStacksUnavailable, ascbridge.ErrStackDenied} {
		ops.revertErr = want
		_, err := RevertAscStack(t.Context(), doer, perm, top, AscRevertOptions{})
		require.ErrorIs(t, err, want)
		// The in-flight guard is released after a failure.
		_, busy := ascRevertRunning.Load(top.ID)
		assert.False(t, busy)
	}
	assert.Empty(t, *opened)
}

func TestAscRevertTitle(t *testing.T) {
	_, top, _, _ := ascRevertFixture(t)
	bottom := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, top.LoadIssue(t.Context()))
	assert.Equal(t, `Revert "`+top.Issue.Title+`"`, ascRevertTitle(t.Context(), []*issues_model.PullRequest{top}))
	assert.Equal(t, `Revert "`+top.Issue.Title+`" (and 1 more)`, ascRevertTitle(t.Context(), []*issues_model.PullRequest{bottom, top}))
	assert.Equal(t, "Revert", ascRevertTitle(t.Context(), nil))

	top.Issue.Title = strings.Repeat("é", 400)
	got := ascRevertTitle(t.Context(), []*issues_model.PullRequest{top})
	assert.Equal(t, 255, len([]rune(got)))
	assert.True(t, strings.HasSuffix(got, "…"))
}
