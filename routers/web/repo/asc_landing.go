// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The Landing card of the files page of a stacked change (board Review): how many entries of the merge queue of the
// target are ahead, when asc expects this stack to start, the queue button named after the changes it takes, and the
// dry run of a land of the stack up to this change. The estimate is asc's (MergeQueue timing, asc_queue_live.go); the
// dry run is the one of the confirmation page (automerge.AscQueueDryRunOf). Nothing here writes, and a part that
// cannot be read is left out of the card.

// Dry run outcomes of the Landing card.
const (
	ascDryClean        = "clean"
	ascDryConflict     = "conflict"
	ascDryNotReady     = "not_ready"
	ascDryContentMerge = "content_merge"
	ascDryPaused       = "paused"
)

// ascLandingTimeout bounds the dry run and the estimate, so a slow asc only leaves them out.
const ascLandingTimeout = 5 * time.Second

// AscLanding is the Landing card.
type AscLanding struct {
	Branch string
	// Queued is true when this pull request waits in the queue; Ahead counts the entries before it then, and every
	// waiting entry otherwise.
	Queued bool
	Ahead  int
	// StartsIn is asc's estimate of when the land of this stack starts ("" when asc has none).
	StartsIn string
	// From and To are the positions in the stack a queue of this change takes (0 when it is not in the rail).
	From, To int
	// Dry is the outcome of the dry run ("" when it could not run); NotReady counts the changes that are not ready.
	Dry      string
	NotReady int
}

// Span is the number of positions from From to To, which names the queue button.
func (l *AscLanding) Span() int {
	if l.To == 0 {
		return 0
	}
	return l.To - l.From + 1
}

// ascLandRange is the first and the last position of the open changes a land up to the current change takes.
func ascLandRange(panel *pull_service.AscStackPanel) (from, to int) {
	to, _ = ascLandPlace(panel)
	if panel == nil || to == 0 {
		return 0, 0
	}
	for _, e := range panel.Entries {
		if e.State == pull_service.AscStackStateOpen && e.Position > 0 && e.Position <= to && (from == 0 || e.Position < from) {
			from = e.Position
		}
	}
	return from, to
}

// ascQueueAhead is the place of pullID among the waiting entries (in the order they land): the entries ahead of it and
// whether it is one of them. An entry that is not waiting has every waiting entry ahead.
func ascQueueAhead(entries []*automerge.AscStackQueueEntry, pullID int64) (ahead int, queued bool, change string) {
	for _, e := range entries {
		if !ascActiveQueueState(e.State) {
			continue
		}
		if e.PullID == pullID {
			return ahead, true, e.AscChange
		}
		ahead++
		change = e.AscChange
	}
	return ahead, false, change
}

// ascDryOutcome sums up a dry run for one line: a paused queue, then a conflict, then changes that are not ready, then a
// land that needs a content merge; otherwise it is clean. "" when the restack half could not run and nothing else is wrong.
func ascDryOutcome(d *automerge.AscQueueDryRun) (outcome string, notReady int) {
	if d == nil {
		return "", 0
	}
	for _, c := range d.Checks {
		if c.Err != nil {
			notReady++
		}
	}
	var conflict *ascbridge.ConflictError
	switch {
	case d.Paused:
		return ascDryPaused, notReady
	case d.Restack != nil && errors.As(d.Restack.Blocker(), &conflict):
		return ascDryConflict, notReady
	case notReady > 0:
		return ascDryNotReady, notReady
	case d.RestackErr != nil || d.Restack == nil:
		return "", 0
	case d.NeedsContentMerge && d.NoContentMerge:
		return ascDryContentMerge, 0
	}
	return ascDryClean, 0
}

// Seams of the tests: asc's queue timing of a branch and the dry run.
var (
	ascLandingTimings = func(ctx gocontext.Context, repo *repo_model.Repository, branch string) (map[string]ascQueueTiming, error) {
		name, err := ascGraphRepoName(repo)
		if err != nil {
			return nil, err
		}
		snap, err := ascQueueRead(ctx, name, branch)
		if err != nil {
			return nil, err
		}
		out := make(map[string]ascQueueTiming, len(snap.Entries))
		for _, t := range snap.Entries {
			out[t.Change] = t
		}
		return out, nil
	}
	ascLandingDryRun = automerge.AscQueueDryRunOf
)

// ascQueueStartsIn is asc's estimate of when the land of pull starts, given what ascQueueAhead says: queued, asc
// times the entry of change itself; not queued, the stack starts when the last waiting entry (change) has landed. ""
// when asc has no estimate.
func ascQueueStartsIn(ctx gocontext.Context, pull *issues_model.PullRequest, queued bool, change string) string {
	timings, err := ascLandingTimings(ctx, pull.BaseRepo, pull.BaseBranch)
	if err != nil {
		return ""
	}
	t, ok := timings[change]
	switch {
	case !ok:
		return ""
	case queued:
		return t.StartsIn
	}
	return t.ETA
}

// prepareAscLanding puts the Landing card into ctx.Data["AscLanding"] on the files page of an open stacked change whose
// viewer may queue it or has it queued. It needs AscActions and AscStackPanel (prepareAscStackPanel).
func prepareAscLanding(ctx *context.Context, pull *issues_model.PullRequest) {
	a, _ := ctx.Data["AscActions"].(*AscStackActions)
	if a == nil || pull == nil || pull.HasMerged || (!a.CanQueue && a.QueueState == "") {
		return
	}
	panel, _ := ctx.Data["AscStackPanel"].(*pull_service.AscStackPanel)
	l := &AscLanding{Branch: pull.BaseBranch}
	l.From, l.To = ascLandRange(panel)
	entries, err := automerge.AscQueueList(ctx, pull.BaseRepoID, pull.BaseBranch)
	if err != nil {
		log.Warn("asc landing: queue of %s: %v", pull.BaseBranch, err)
		return
	}
	var change string
	l.Ahead, l.Queued, change = ascQueueAhead(entries, pull.ID)
	callCtx, cancel := gocontext.WithTimeout(ctx, ascLandingTimeout)
	defer cancel()
	if l.Ahead > 0 || l.Queued {
		l.StartsIn = ascQueueStartsIn(callCtx, pull, l.Queued, change)
	}
	if a.CanMerge {
		if d, err := ascLandingDryRun(callCtx, ctx.Doer, pull); err == nil {
			l.Dry, l.NotReady = ascDryOutcome(d)
		}
	}
	ctx.Data["AscLanding"] = l
}
