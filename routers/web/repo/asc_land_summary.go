// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"slices"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
)

// The summary of a land on the confirmation page of "Merge when ready" and "Merge now" (board ReviewVersions, land
// panel): the order the pull requests land in, who signed them, the place the stack takes in the merge queue with
// asc's estimate, and how long asc keeps the move of the branch undoable. Each part comes from asc or the queue as
// it is now; a part that cannot be read is left out, and nothing here writes.

// AscLandSummary is the land summary of the confirmation page.
type AscLandSummary struct {
	// Order is the pull requests by number, in the order they land.
	Order []int64
	// Signed is true when asc said who signed the heads; Signers are the verified signers then, and Unverified
	// counts the heads without a verified signature.
	Signed     bool
	Signers    []string
	Unverified int
	// Position and Total place the top of the stack in the merge queue once it is queued (0 when it is not shown);
	// StartsIn is asc's estimate of when its land starts.
	Position, Total int
	StartsIn        string
	// Undo is how long asc keeps the move of the branch undoable ("" when unknown); UndoOff is true when it does not.
	Undo    string
	UndoOff bool
}

// ascLandSigners sums up the badges of heads: the verified signers, each once in the order met, and how many heads
// have no verified signature.
func ascLandSigners(heads []string, badges map[string]asctrust.Badge) (signers []string, unverified int) {
	for _, h := range heads {
		b, ok := badges[h]
		if !ok || b.Label != "Verified" {
			unverified++
			continue
		}
		if b.Signer != "" && !slices.Contains(signers, b.Signer) {
			signers = append(signers, b.Signer)
		}
	}
	return signers, unverified
}

// ascQueuePlace is the place the top pull request of a stack takes in the queue: its own place when it is queued,
// otherwise the last one after the pull requests of the stack that are not queued yet (adding counts them). change is
// the entry asc times for the estimate ("" when nothing waits).
func ascQueuePlace(entries []*automerge.AscStackQueueEntry, top int64, adding int) (position, total int, queued bool, change string) {
	var ahead int
	ahead, queued, change = ascQueueAhead(entries, top)
	for _, e := range entries {
		if ascActiveQueueState(e.State) {
			total++
		}
	}
	if queued {
		return ahead + 1, total, true, change
	}
	return total + adding, total + adding, false, change
}

// ascLandSummaryOf builds the summary for the dry run d of a land of pr. queue says the operation is "Merge when
// ready", which takes a place in the queue.
func ascLandSummaryOf(ctx *context.Context, pr *issues_model.PullRequest, d *automerge.AscQueueDryRun, queue bool) *AscLandSummary {
	s := &AscLandSummary{}
	heads := make([]string, 0, len(d.Checks))
	adding := 0
	for _, c := range d.Checks {
		if c.Item.PR == nil {
			continue
		}
		if c.Item.PR.Issue != nil {
			s.Order = append(s.Order, c.Item.PR.Issue.Index)
		} else {
			s.Order = append(s.Order, c.Item.PR.Index)
		}
		h := c.Item.Head
		if h == "" {
			h = ascinbox.HeadCommit(ctx, c.Item.PR)
		}
		if h != "" {
			heads = append(heads, h)
		}
		if e := d.Queued[c.Item.PR.ID]; e == nil || !ascActiveQueueState(e.State) {
			adding++
		}
	}
	if badges, ok := ascCommitBadges(ctx, pr.BaseRepo, heads); ok {
		s.Signed = true
		s.Signers, s.Unverified = ascLandSigners(heads, badges)
	}
	if queue {
		if entries, err := automerge.AscQueueList(ctx, pr.BaseRepoID, pr.BaseBranch); err == nil {
			var queued bool
			var change string
			s.Position, s.Total, queued, change = ascQueuePlace(entries, pr.ID, adding)
			if change != "" {
				callCtx, cancel := gocontext.WithTimeout(ctx, ascLandingTimeout)
				s.StartsIn = ascQueueStartsIn(callCtx, pr, queued, change)
				cancel()
			}
		}
	}
	if page := ascRefLog(ctx, pr.BaseRepo, "refs/heads/"+pr.BaseBranch, 1); page != nil {
		if page.UndoWindow == 0 {
			s.UndoOff = true
		} else {
			s.Undo = ascagentui.FormatDuration(page.UndoWindow)
		}
	}
	return s
}
