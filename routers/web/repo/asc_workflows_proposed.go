// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/context"
)

// "Proposed by the agent" on a workflow run page: the changes that the agent run of an agent step produced, each with its
// size, its state and the link to its review. They come from the agent run of the step (Agents.GetRun) and are matched to
// the pull requests of this repository by change id, never by anything a request names. A step the viewer may not read
// (Private) shows nothing, and a part that cannot be read is left out; none of it blocks the page.

const (
	// ascWorkflowProposedShown is how many changes a step lists; the rest is a count.
	ascWorkflowProposedShown = 12
	// ascWorkflowProposedSized is how many of them are measured against git (the diff of each is a read of the repository).
	ascWorkflowProposedSized = 8
)

// ascWorkflowProposedChange is one change an agent step proposed.
type ascWorkflowProposedChange struct {
	Short    string
	URL      string // the review page, "" without a pull request or without the right to read pull requests
	PullID   int64
	State    string // "landed", "closed", "open", or "" when the change was never submitted for review
	Review   string // for an open one: "approved", "changes", "draft" or ""
	Add, Del int
	HasSize  bool
}

// ascWorkflowStepProposed fills s.Proposed from the agent run of the step.
func ascWorkflowStepProposed(ctx *context.Context, callCtx gocontext.Context, env *ascAgentsEnv, api ascv1.AgentsClient, s *ascWorkflowStepRun, canPulls bool) {
	if s.Private || s.AgentRunID == 0 {
		return
	}
	got, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: s.AgentRunID})
	if err != nil || got.GetRun().GetRepo() != env.name || got.GetRun().GetId() != s.AgentRunID {
		return
	}
	links, err := ascagentui.ChangeLinks(ctx, ctx.Repo.Repository, env.link, got.GetRun().GetChanges(), canPulls)
	if err != nil {
		log.Warn("asc workflows: changes of agent run %d: %v", s.AgentRunID, err)
		return
	}
	sized := 0
	for _, l := range links {
		if len(s.Proposed) >= ascWorkflowProposedShown {
			s.ProposedMore++
			continue
		}
		c := ascWorkflowProposedChange{Short: l.Short}
		if l.HasPull {
			c.URL, c.PullID = l.URL, l.PullID
			switch {
			case l.Merged:
				c.State = "landed"
			case l.Closed:
				c.State = "closed"
			default:
				c.State = "open"
			}
			if c.State == "open" {
				c.Review, c.Add, c.Del, c.HasSize = ascWorkflowPullFacts(ctx, l.PullID, sized < ascWorkflowProposedSized)
				if c.HasSize {
					sized++
				}
			}
		}
		s.Proposed = append(s.Proposed, c)
	}
}

// ascWorkflowPullFacts reads the review state of an open pull request and, when size is true, its size. Advice: a part
// that cannot be read comes back empty.
func ascWorkflowPullFacts(ctx *context.Context, index int64, size bool) (review string, add, del int, hasSize bool) {
	repo := ctx.Repo.Repository
	pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, index)
	if err != nil {
		return "", 0, 0, false
	}
	pr.BaseRepo = repo
	if pr.LoadIssue(ctx) != nil {
		return "", 0, 0, false
	}
	if size && ctx.Repo.GitRepo != nil {
		if sha := ascinbox.HeadCommit(ctx, pr); sha != "" {
			base := pr.MergeBase
			if base == "" {
				base = pr.BaseBranch
			}
			if _, a, d, err := ctx.Repo.GitRepo.GetShortStat(base, sha, base == pr.BaseBranch); err == nil {
				add, del, hasSize = a, d, true
			}
		}
	}
	reviews, err := issues_model.FindLatestReviews(ctx, issues_model.FindReviewOptions{
		IssueID: pr.IssueID, Dismissed: optional.Some(false),
		Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
	})
	if err != nil {
		log.Warn("asc workflows: reviews of pull request %d: %v", pr.ID, err)
	}
	rejected, approved := false, false
	for _, r := range reviews {
		rejected = rejected || r.Type == issues_model.ReviewTypeReject
		approved = approved || r.Type == issues_model.ReviewTypeApprove
	}
	return ascStackReview(pr.IsWorkInProgress(ctx), rejected, approved), add, del, hasSize
}
