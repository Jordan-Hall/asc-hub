// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The header strip of a stacked change (board Review, "Change header"): the change id and the short hash of its newest
// version, the Signed badge of that version, the change it is based on, the agent run it was opened for and where the
// review stands. It is drawn under the title (templates/repo/pulls/asc_change_header.tmpl) and is advice only: every
// part that cannot be read is left out, and none of it blocks the page.

// Where the review of a change stands, for the banner. The words are locale keys (asc_review.change_state_<state>).
const (
	ascChangeWaitingOnYou     = "waiting_on_you"
	ascChangeWaitingForReview = "waiting_for_review"
	ascChangeChangesRequested = "changes_requested"
	ascChangeApproved         = "approved"
	ascChangeDraft            = "draft"
	ascChangeMerged           = "merged"
	ascChangeClosed           = "closed"
)

// AscChangeHeader is what the header strip of a stacked change shows.
type AscChangeHeader struct {
	// ChangeID is the start of the change id and Commit the short hash of the newest version, which is Version.
	ChangeID, Commit string
	Version          uint32
	// Signature is the badge of the newest version; an empty Label draws nothing.
	Signature asctrust.Badge
	// BasedOn is the change below this one in its stack, nil at the bottom.
	BasedOn *pull_service.AscStackEntry
	// Run is the agent run the pull request was opened for, nil for a change a person made.
	Run *AscRunPull
	// State is where the review stands (the constants above).
	State                   string
	ChecksPass, ChecksTotal int
}

const (
	ascChangeIDShown = 12
	ascCommitShown   = 8
)

// ascChangeState says where the review of a change stands. A change that needs a decision from the viewer comes
// before the general waiting state, so the banner can say "waiting on you".
func ascChangeState(merged, closed, draft, changesRequested, approved, askedViewer bool) string {
	switch {
	case merged:
		return ascChangeMerged
	case closed:
		return ascChangeClosed
	case draft:
		return ascChangeDraft
	case changesRequested:
		return ascChangeChangesRequested
	case askedViewer:
		return ascChangeWaitingOnYou
	case approved:
		return ascChangeApproved
	}
	return ascChangeWaitingForReview
}

// prepareAscChangeHeader puts the header strip of a stacked pull request into ctx.Data["AscChangeHeader"]. It runs
// after the stack panel, the brief and the run line, whose answers it reuses, so it makes no call of its own except the
// signature check of one commit.
func prepareAscChangeHeader(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil || !setting.Asc.Enabled || ctx.Data["AscStackPanel"] == nil {
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil {
		return
	}
	if err := pull.LoadIssue(ctx); err != nil {
		return
	}
	h := &AscChangeHeader{}
	if issues_model.IsAscChange(link.AscChange) {
		h.ChangeID = link.AscChange[:ascChangeIDShown]
	}
	if panel, ok := ctx.Data["AscStackPanel"].(*pull_service.AscStackPanel); ok && panel != nil {
		for i := range panel.Entries {
			if panel.Entries[i].Current && i > 0 {
				h.BasedOn = &panel.Entries[i-1]
			}
		}
	}
	if run, ok := ctx.Data["AscRunPull"].(*AscRunPull); ok {
		h.Run = run
	}
	if versions, err := ascVersionsOnce(ctx, pull); err == nil && len(versions.Versions) > 0 {
		newest := versions.Versions[len(versions.Versions)-1]
		h.Version = newest.N
		if len(newest.GitCommit) >= ascCommitShown {
			h.Commit = newest.GitCommit[:ascCommitShown]
		}
		h.Signature = ascVersionBadges(ctx, &pull_service.AscVersionList{Versions: versions.Versions[len(versions.Versions)-1:]})[newest.N]
	}
	if pull.BaseRepo != nil {
		if sha := ascinbox.HeadCommit(ctx, pull); sha != "" {
			if statuses, _, err := git_model.GetLatestCommitStatus(ctx, pull.BaseRepo.ID, sha, db.ListOptionsAll); err == nil {
				h.ChecksPass, h.ChecksTotal = relChecks(statuses)
			}
		}
	}
	var changesRequested, approved bool
	if b, ok := ctx.Data["AscBrief"].(*AscBrief); ok && b != nil {
		changesRequested = b.ChangesRequested > 0
		approved = b.ApprovalsCurrent > 0 && b.PendingReviewers == 0
	}
	asked := false
	if ctx.Doer != nil {
		if r, err := issues_model.GetReviewByIssueIDAndUserID(ctx, pull.IssueID, ctx.Doer.ID); err == nil && r != nil {
			asked = r.Type == issues_model.ReviewTypeRequest
		}
	}
	h.State = ascChangeState(pull.HasMerged, pull.Issue.IsClosed && !pull.HasMerged, pull.IsWorkInProgress(ctx), changesRequested, approved, asked)
	ctx.Data["AscChangeHeader"] = h
}
