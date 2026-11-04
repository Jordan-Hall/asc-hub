// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscLandSummaryParts(t *testing.T) {
	a, b, c := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	badges := map[string]asctrust.Badge{
		a: {Label: "Verified", Signer: "Priya Raman"},
		b: {Label: "Unverified", Signer: "coder-2"},
	}
	signers, unverified := ascLandSigners([]string{a, b, c, a}, badges)
	assert.Equal(t, []string{"Priya Raman"}, signers, "a signer is named once, and only for a verified signature")
	assert.Equal(t, 2, unverified, "an unverified and an unknown head have no verified signature")

	entries := []*automerge.AscStackQueueEntry{
		{PullID: 7, State: automerge.AscQueueMerged, AscChange: "c7"},
		{PullID: 8, State: automerge.AscQueueLanding, AscChange: "c8"},
		{PullID: 9, State: automerge.AscQueueQueued, AscChange: "c9"},
	}
	pos, total, queued, change := ascQueuePlace(entries, 9, 0)
	assert.Equal(t, []int{2, 2}, []int{pos, total})
	assert.True(t, queued)
	assert.Equal(t, "c9", change)
	pos, total, queued, change = ascQueuePlace(entries, 11, 2)
	assert.Equal(t, []int{4, 4}, []int{pos, total}, "the top of a new stack lands after its own changes")
	assert.False(t, queued)
	assert.Equal(t, "c9", change)
	pos, total, _, change = ascQueuePlace(nil, 11, 1)
	assert.Equal(t, []int{1, 1}, []int{pos, total})
	assert.Empty(t, change, "nothing waits: nothing to time")
}

func TestAscLandSummaryCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	page := func(s *AscLandSummary) string {
		return renderPart(t, "repo/asc_confirm", func(d map[string]any) {
			d["Title"] = "Merge when ready"
			d["AscOp"] = "queue"
			d["AscBack"] = "/user2/repo1/pulls/3"
			d["AscLandSummary"] = s
		})
	}
	assert.NotContains(t, page(nil), "asc-land-summary")
	assert.NotContains(t, page(&AscLandSummary{}), "asc-land-summary", "nothing known: no list")

	html := page(&AscLandSummary{Order: []int64{11, 12}, Signed: true, Signers: []string{"Priya Raman", "coder-2"}, Unverified: 1, Position: 3, Total: 3, StartsIn: "6 min", Undo: "30 min"})
	assert.Contains(t, html, `data-testid="asc-land-summary"`)
	assert.Contains(t, html, `data-testid="asc-land-order">#11 asc_review.land_then #12<`)
	assert.Contains(t, html, `data-testid="asc-land-signers">Priya Raman, coder-2 · <span class="rv-muted">asc_review.land_unverified_one</span>`)
	assert.Contains(t, html, `data-testid="asc-land-queue">asc_review.land_queue_place · asc_review.land_queue_starts<`)
	assert.Contains(t, html, `data-testid="asc-land-undo">30 min<`)

	html = page(&AscLandSummary{Order: []int64{11}, Signed: true, Unverified: 1, UndoOff: true})
	assert.Contains(t, html, `data-testid="asc-land-signers"><span class="rv-muted">`, "no verified signer: only the count")
	assert.NotContains(t, html, "asc-land-queue", "merge now takes no place in the queue")
	assert.Contains(t, html, `data-testid="asc-land-undo">asc_review.land_undo_off<`)

	html = page(&AscLandSummary{Order: []int64{11}, Position: 1, Total: 1})
	assert.NotContains(t, html, "asc-land-signers", "asc could not say who signed: no line")
	assert.NotContains(t, html, "asc_review.land_queue_starts", "no estimate is guessed")
	assert.NotContains(t, html, "asc-land-undo")
}

func TestAscLandSummaryOf(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ft := startFakeTrust(t, asctrust.FeatureStacks) // before the loads: it moves repo 1 into asc
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	below := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	for _, p := range []*issues_model.PullRequest{pull, below} {
		require.NoError(t, p.LoadBaseRepo(db.DefaultContext))
		require.NoError(t, p.LoadIssue(db.DefaultContext))
	}
	headBelow, headTop := strings.Repeat("a", 40), strings.Repeat("b", 40)

	var refs []string
	window := uint64(1800)
	ft.listRefLog = func(r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		refs = append(refs, r.GetRef())
		return &ascv1.ListRefLogResponse{UndoWindowSeconds: window}, nil
	}
	oldB, oldT := ascCommitBadges, ascLandingTimings
	t.Cleanup(func() { ascCommitBadges, ascLandingTimings = oldB, oldT })
	var verified []string
	ascCommitBadges = func(_ *context.Context, _ *repo_model.Repository, commits []string) (map[string]asctrust.Badge, bool) {
		verified = commits
		return map[string]asctrust.Badge{headBelow: {Label: "Verified", Signer: "Priya Raman"}}, true
	}
	ascLandingTimings = func(gocontext.Context, *repo_model.Repository, string) (map[string]ascQueueTiming, error) {
		return map[string]ascQueueTiming{"cother": {StartsIn: "1 min", ETA: "6 min"}}, nil
	}

	dry := &automerge.AscQueueDryRun{Checks: []pull_service.AscStackCheck{
		{Item: &pull_service.AscStackItem{PR: below, Head: headBelow}},
		{Item: &pull_service.AscStackItem{PR: pull, Head: headTop}},
	}}
	run := func(queue bool) *AscLandSummary {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/pulls/3/asc/confirm")
		ctx.Doer = doer
		return ascLandSummaryOf(ctx, pull, dry, queue)
	}

	s := run(false)
	assert.Equal(t, []int64{below.Issue.Index, pull.Issue.Index}, s.Order, "bottom first")
	assert.Equal(t, []string{headBelow, headTop}, verified, "the heads the dry run checked are verified")
	assert.True(t, s.Signed)
	assert.Equal(t, []string{"Priya Raman"}, s.Signers)
	assert.Equal(t, 1, s.Unverified)
	assert.Zero(t, s.Position, "merge now takes no place in the queue")
	assert.Equal(t, "30 min", s.Undo)
	assert.False(t, s.UndoOff)
	assert.Equal(t, []string{"refs/heads/" + pull.BaseBranch}, refs, "the window is the one of the target branch")

	s = run(true)
	assert.Equal(t, []int{2, 2}, []int{s.Position, s.Total}, "an empty queue: the top lands after the change below")
	assert.Empty(t, s.StartsIn, "nothing waits: no estimate")

	other := &automerge.AscStackQueueEntry{RepoID: pull.BaseRepoID, Branch: pull.BaseBranch, PullID: 999002, AscChange: "cother", DoerID: 1, State: automerge.AscQueueQueued}
	require.NoError(t, db.Insert(db.DefaultContext, other))
	dry.Queued = map[int64]*automerge.AscStackQueueEntry{below.ID: {State: automerge.AscQueueMerged}}
	s = run(true)
	assert.Equal(t, []int{3, 3}, []int{s.Position, s.Total}, "a finished entry of the stack is queued again")
	assert.Equal(t, "6 min", s.StartsIn, "it starts when the last waiting entry has landed")

	window = 0
	ascCommitBadges = func(*context.Context, *repo_model.Repository, []string) (map[string]asctrust.Badge, bool) {
		return nil, false
	}
	s = run(false)
	assert.True(t, s.UndoOff, "asc keeps no undo of the branch")
	assert.Empty(t, s.Undo)
	assert.False(t, s.Signed, "asc could not be asked: no signers line")

	window = asctrust.MaxUndoWindow + 1
	s = run(false)
	assert.False(t, s.UndoOff)
	assert.Empty(t, s.Undo, "a window longer than asc allows is a bad answer, not shown")
}
