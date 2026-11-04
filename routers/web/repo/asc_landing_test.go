// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/automerge"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscLandingParts(t *testing.T) {
	open := pull_service.AscStackStateOpen
	panel := &pull_service.AscStackPanel{Entries: []pull_service.AscStackEntry{
		{Position: 1, State: pull_service.AscStackStateMerged},
		{Position: 2, State: open},
		{Position: 3, State: open, Current: true},
		{Position: 4, State: open},
	}}
	from, to := ascLandRange(panel)
	assert.Equal(t, 2, from, "a landed change is not queued again")
	assert.Equal(t, 3, to)
	assert.Equal(t, 2, (&AscLanding{From: from, To: to}).Span())
	from, to = ascLandRange(nil)
	assert.Zero(t, from+to)
	assert.Zero(t, (&AscLanding{}).Span(), "a change outside the rail names no positions")

	entries := []*automerge.AscStackQueueEntry{
		{PullID: 7, State: automerge.AscQueueMerged, AscChange: "c7"},
		{PullID: 8, State: automerge.AscQueueLanding, AscChange: "c8"},
		{PullID: 9, State: automerge.AscQueueQueued, AscChange: "c9"},
		{PullID: 10, State: automerge.AscQueueBlocked, AscChange: "c10"},
	}
	ahead, queued, change := ascQueueAhead(entries, 9)
	assert.Equal(t, 1, ahead)
	assert.True(t, queued)
	assert.Equal(t, "c9", change, "a queued entry is timed by its own change")
	ahead, queued, change = ascQueueAhead(entries, 11)
	assert.Equal(t, 3, ahead, "a finished entry is not ahead")
	assert.False(t, queued)
	assert.Equal(t, "c10", change, "a new entry starts when the last waiting one has landed")

	notReady := errors.New("not ready")
	conflict := &pull_service.AscRestackStackResult{Items: []pull_service.AscRestackStackItem{{State: ascbridge.RestackConflict, Conflicts: []string{"a.go"}}}}
	rewritten := &pull_service.AscRestackStackResult{Items: []pull_service.AscRestackStackItem{{State: ascbridge.RestackRewritten}}}
	clean := &pull_service.AscRestackStackResult{UpToDate: true}
	twoChecks := func(err error) []pull_service.AscStackCheck {
		return []pull_service.AscStackCheck{{}, {Err: err}}
	}
	for _, c := range []struct {
		name     string
		d        *automerge.AscQueueDryRun
		outcome  string
		notReady int
	}{
		{"none", nil, "", 0},
		{"paused first", &automerge.AscQueueDryRun{Paused: true, Restack: conflict, Checks: twoChecks(notReady)}, ascDryPaused, 1},
		{"conflict before readiness", &automerge.AscQueueDryRun{Restack: conflict, Checks: twoChecks(notReady)}, ascDryConflict, 1},
		{"not ready", &automerge.AscQueueDryRun{Restack: clean, Checks: twoChecks(notReady)}, ascDryNotReady, 1},
		{"not ready without asc", &automerge.AscQueueDryRun{RestackErr: errors.New("down"), Checks: twoChecks(notReady)}, ascDryNotReady, 1},
		{"asc down says nothing", &automerge.AscQueueDryRun{RestackErr: errors.New("down"), Checks: twoChecks(nil)}, "", 0},
		{"content merge refused", &automerge.AscQueueDryRun{Restack: rewritten, NeedsContentMerge: true, NoContentMerge: true, Checks: twoChecks(nil)}, ascDryContentMerge, 0},
		{"content merge allowed", &automerge.AscQueueDryRun{Restack: rewritten, NeedsContentMerge: true, Checks: twoChecks(nil)}, ascDryClean, 0},
		{"clean", &automerge.AscQueueDryRun{Restack: clean, Checks: twoChecks(nil)}, ascDryClean, 0},
	} {
		outcome, n := ascDryOutcome(c.d)
		assert.Equal(t, c.outcome, outcome, c.name)
		assert.Equal(t, c.notReady, n, c.name)
	}
}

func TestAscLandingCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	card := func(l *AscLanding, a *AscStackActions) string {
		return renderPart(t, "repo/pulls/asc_panel", func(d map[string]any) {
			d["AscStackPanel"] = &pull_service.AscStackPanel{}
			d["AscLanding"] = l
			d["AscActions"] = a
		})
	}
	acts := &AscStackActions{CanQueue: true, ConfirmURL: "/user2/repo1/pulls/3/asc/confirm"}

	assert.NotContains(t, card(nil, acts), `data-testid="asc-landing"`, "no card without its data")

	html := card(&AscLanding{Branch: "main", Ahead: 2, StartsIn: "6 min", From: 1, To: 2, Dry: ascDryClean}, acts)
	assert.Contains(t, html, `data-testid="asc-landing"`)
	assert.Contains(t, html, `aria-labelledby="asc-landing-title"`)
	assert.Contains(t, html, "asc_review.landing_ahead")
	assert.Contains(t, html, `data-testid="asc-landing-starts"`)
	assert.Contains(t, html, "asc_review.landing_starts")
	assert.Contains(t, html, `data-asc-confirm-open href="/user2/repo1/pulls/3/asc/confirm?op=queue" data-testid="asc-landing-queue">asc_review.landing_queue_two<`)
	assert.Contains(t, html, `data-dry="clean">asc_review.landing_dry_clean<`)
	assert.NotContains(t, html, "asc-landing-unqueue")

	html = card(&AscLanding{Branch: "main", From: 3, To: 3, Dry: ascDryNotReady, NotReady: 2}, acts)
	assert.Contains(t, html, "asc_review.landing_empty")
	assert.NotContains(t, html, "asc-landing-starts", "no estimate is guessed")
	assert.Contains(t, html, ">asc_review.landing_queue_one<")
	assert.Contains(t, html, "asc_review.landing_dry_not_ready_one", "TrN picks the form by count")

	assert.Contains(t, card(&AscLanding{From: 1, To: 4}, acts), ">asc_review.landing_queue_range<")
	assert.Contains(t, card(&AscLanding{}, acts), ">repo.pulls.asc_op.queue<", "outside the rail the button keeps its plain name")
	assert.NotContains(t, card(&AscLanding{}, acts), "asc-landing-dry", "no line when the dry run could not run")

	queued := &AscStackActions{CanUnqueue: true, QueueState: automerge.AscQueueQueued, ConfirmURL: acts.ConfirmURL}
	html = card(&AscLanding{Queued: true, Ahead: 1, Dry: ascDryConflict}, queued)
	assert.Contains(t, html, "asc_review.landing_queued")
	assert.Contains(t, html, `href="/user2/repo1/pulls/3/asc/confirm?op=unqueue" data-testid="asc-landing-unqueue"`)
	assert.NotContains(t, html, "asc-landing-queue\"")
	assert.Contains(t, html, "asc_review.landing_dry_conflict")
	assert.Contains(t, card(&AscLanding{Queued: true}, queued), "asc_review.landing_queued_next")
	for dry, key := range map[string]string{ascDryContentMerge: "landing_dry_content_merge", ascDryPaused: "landing_dry_paused"} {
		assert.Contains(t, card(&AscLanding{Dry: dry}, acts), "asc_review."+key)
	}
}

func TestPrepareAscLanding(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, pull.LoadBaseRepo(db.DefaultContext))

	var timed, dried int
	oldT, oldD := ascLandingTimings, ascLandingDryRun
	t.Cleanup(func() { ascLandingTimings, ascLandingDryRun = oldT, oldD })
	ascLandingTimings = func(_ gocontext.Context, _ *repo_model.Repository, _ string) (map[string]ascQueueTiming, error) {
		timed++
		return map[string]ascQueueTiming{"cother": {ETA: "6 min", StartsIn: "1 min"}, "cmine": {StartsIn: "4 min", ETA: "9 min"}}, nil
	}
	ascLandingDryRun = func(_ gocontext.Context, _ *user_model.User, _ *issues_model.PullRequest) (*automerge.AscQueueDryRun, error) {
		dried++
		return &automerge.AscQueueDryRun{Restack: &pull_service.AscRestackStackResult{UpToDate: true}}, nil
	}

	run := func(a *AscStackActions) *AscLanding {
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/pulls/3/files")
		ctx.Doer = doer
		ctx.Data["AscActions"] = a
		ctx.Data["AscStackPanel"] = &pull_service.AscStackPanel{Entries: []pull_service.AscStackEntry{
			{Position: 1, State: pull_service.AscStackStateOpen},
			{Position: 2, State: pull_service.AscStackStateOpen, Current: true},
		}}
		prepareAscLanding(ctx, pull)
		l, _ := ctx.Data["AscLanding"].(*AscLanding)
		return l
	}

	assert.Nil(t, run(nil), "no card without the actions")
	assert.Nil(t, run(&AscStackActions{CanRestack: true}), "no card for a viewer who can neither queue nor wait")

	l := run(&AscStackActions{CanQueue: true})
	require.NotNil(t, l)
	assert.Equal(t, pull.BaseBranch, l.Branch)
	assert.Zero(t, l.Ahead)
	assert.Empty(t, l.StartsIn)
	assert.Zero(t, timed, "an empty queue asks asc for no estimate")
	assert.Equal(t, 1, l.From)
	assert.Equal(t, 2, l.To)
	assert.Empty(t, l.Dry, "the dry run is for those who may merge")
	assert.Zero(t, dried)

	other := &automerge.AscStackQueueEntry{RepoID: pull.BaseRepoID, Branch: pull.BaseBranch, PullID: 999001, AscChange: "cother", DoerID: 1, State: automerge.AscQueueQueued}
	require.NoError(t, db.Insert(db.DefaultContext, other))
	l = run(&AscStackActions{CanQueue: true, CanMerge: true})
	assert.Equal(t, 1, l.Ahead)
	assert.False(t, l.Queued)
	assert.Equal(t, "6 min", l.StartsIn, "not queued, it starts when the last waiting entry has landed")
	assert.Equal(t, ascDryClean, l.Dry)
	assert.Equal(t, 1, dried)

	mine := &automerge.AscStackQueueEntry{RepoID: pull.BaseRepoID, Branch: pull.BaseBranch, PullID: pull.ID, AscChange: "cmine", DoerID: doer.ID, State: automerge.AscQueueQueued}
	require.NoError(t, db.Insert(db.DefaultContext, mine))
	l = run(&AscStackActions{QueueState: automerge.AscQueueQueued, CanUnqueue: true})
	assert.True(t, l.Queued)
	assert.Equal(t, 1, l.Ahead)
	assert.Equal(t, "4 min", l.StartsIn, "queued, asc times the entry itself")

	ascLandingTimings = func(gocontext.Context, *repo_model.Repository, string) (map[string]ascQueueTiming, error) {
		return nil, errors.New("asc down")
	}
	l = run(&AscStackActions{QueueState: automerge.AscQueueQueued, CanUnqueue: true})
	require.NotNil(t, l, "asc down leaves the estimate out, not the card")
	assert.Empty(t, l.StartsIn)

	pull.HasMerged = true
	assert.Nil(t, run(&AscStackActions{CanQueue: true}), "a merged change has nothing to land")
}
