// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"context"
	"fmt"
	"strconv"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/util"
	webhook_module "forgejo.org/modules/webhook"
)

// prepareAsc queues the ASC event p of repo for every hook that sends it, once for each hook when eventID names it.
func prepareAsc(ctx context.Context, repo *repo_model.Repository, p *api.AscHookPayload, eventID string) error {
	p.Repository = repo.FullName()
	return PrepareWebhooksOnce(ctx, EventSource{Repository: repo}, webhook_module.HookEventType(p.Event), p, eventID)
}

// prepareAscLogged is prepareAsc for the events of this hub, which only log a failure.
func prepareAscLogged(ctx context.Context, repo *repo_model.Repository, p *api.AscHookPayload) {
	if err := prepareAsc(ctx, repo, p, ""); err != nil {
		log.Error("PrepareWebhooks [%s %s]: %v", p.Event, p.Repository, err)
	}
}

// prepareAscChange queues a change.opened, change.landed or review.requested event of the pull request pr.
func prepareAscChange(ctx context.Context, event webhook_module.HookEventType, pr *issues_model.PullRequest, doer, reviewer *user_model.User) {
	if err := pr.LoadIssue(ctx); err != nil {
		log.Error("LoadIssue: %v", err)
		return
	}
	if err := pr.Issue.LoadRepo(ctx); err != nil {
		log.Error("LoadRepo: %v", err)
		return
	}
	change := &api.AscHookChange{Number: pr.Index, Title: pr.Issue.Title, Base: pr.BaseBranch, Head: pr.HeadBranch}
	if reviewer != nil {
		change.Reviewer = reviewer.Name
	}
	p := &api.AscHookPayload{Event: string(event), Change: change, URL: pr.Issue.HTMLURL()}
	if doer != nil {
		p.Sender = doer.Name
	}
	prepareAscLogged(ctx, pr.Issue.Repo, p)
}

// prepareAscRelease queues the release.published event of rel. Changes counts the commits since the published release
// before it, or all of them when it is the first.
func prepareAscRelease(ctx context.Context, rel *repo_model.Release) {
	if rel.IsDraft || rel.IsTag {
		return
	}
	if err := rel.LoadAttributes(ctx); err != nil {
		log.Error("LoadAttributes: %v", err)
		return
	}
	changes := rel.NumCommits
	prev := &repo_model.Release{}
	has, err := db.GetEngine(ctx).Where("repo_id = ? AND is_draft = ? AND is_tag = ? AND id <> ? AND created_unix <= ?",
		rel.RepoID, false, false, rel.ID, rel.CreatedUnix).Desc("created_unix", "id").Get(prev)
	if err != nil {
		log.Error("previous release of %d: %v", rel.ID, err)
	} else if has && prev.NumCommits <= rel.NumCommits {
		changes = rel.NumCommits - prev.NumCommits
	}
	base := rel.Target
	if len(rel.Sha1) >= 7 {
		base += "@" + rel.Sha1[:7]
	}
	p := &api.AscHookPayload{
		Event:   string(webhook_module.HookEventReleasePublished),
		Release: &api.AscHookRelease{Tag: rel.TagName, Base: base, Changes: changes},
		URL:     rel.HTMLURL(),
	}
	if rel.Publisher != nil {
		p.Sender = rel.Publisher.Name
	}
	prepareAscLogged(ctx, rel.Repo, p)
}

// AscRun queues an agent_run.started or agent_run.finished event that asc serve reported with the internal call
// (routers/private/asc_hook_event.go) under the id eventID.
func AscRun(ctx context.Context, repo *repo_model.Repository, eventID string, event webhook_module.HookEventType, id uint64, state string, changes uint64) error {
	return prepareAsc(ctx, repo, &api.AscHookPayload{
		Event: string(event),
		Run:   &api.AscHookRun{ID: id, State: state, Changes: changes},
		URL:   repo.HTMLURL() + "/asc/agents/runs/" + strconv.FormatUint(id, 10),
	}, eventID)
}

// AscPolicy queues a policy.changed event that asc serve reported under the id eventID: trunk moved from before to
// after and the policy in force changed as summary says. sender is who moved it, empty when asc does not know.
func AscPolicy(ctx context.Context, repo *repo_model.Repository, eventID, before, after, summary, sender string) error {
	return prepareAsc(ctx, repo, &api.AscHookPayload{
		Event:  string(webhook_module.HookEventPolicyChanged),
		Policy: &api.AscHookPolicy{Before: before, After: after, Summary: summary},
		Sender: sender,
		URL:    repo.HTMLURL() + "/settings/asc/policy",
	}, eventID)
}

// getAscPayloadInfo is the one-line summary of an ASC event that the chat hooks post, and its colour.
func (wpf webhookPayloadFormatter) getAscPayloadInfo(p *api.AscHookPayload) (text string, color int) {
	in := " in " + p.Repository
	switch webhook_module.HookEventType(p.Event) {
	case webhook_module.HookEventChangeOpened, webhook_module.HookEventChangeLanded, webhook_module.HookEventReviewRequested:
		if p.Change == nil {
			return "", 0
		}
		title := wpf.linkFormatter(p.URL, fmt.Sprintf("#%d %s", p.Change.Number, p.Change.Title))
		switch webhook_module.HookEventType(p.Event) {
		case webhook_module.HookEventChangeOpened:
			text, color = fmt.Sprintf("Change %s opened%s", title, in), yellowColor
		case webhook_module.HookEventChangeLanded:
			// The branch goes through the link formatter, which escapes it for the chat it is posted to.
			base := wpf.linkFormatter(setting.AppURL+p.Repository+"/src/branch/"+util.PathEscapeSegments(p.Change.Base), p.Change.Base)
			text, color = fmt.Sprintf("Change %s landed on %s%s", title, base, in), purpleColor
		default:
			text, color = fmt.Sprintf("Review of %s requested from %s%s", title, wpf.nameFormatter(p.Change.Reviewer), in), yellowColor
		}
	case webhook_module.HookEventAgentRunStarted, webhook_module.HookEventAgentRunFinished:
		if p.Run == nil {
			return "", 0
		}
		run := wpf.linkFormatter(p.URL, fmt.Sprintf("Agent run #%d", p.Run.ID))
		if webhook_module.HookEventType(p.Event) == webhook_module.HookEventAgentRunStarted {
			text, color = fmt.Sprintf("%s started%s", run, in), yellowColor
		} else {
			text = fmt.Sprintf("%s %s with %s%s", run, p.Run.State, changesCount(int64(p.Run.Changes)), in)
			color = greenColor
			if p.Run.State != "succeeded" {
				color = redColor
			}
		}
	case webhook_module.HookEventReleasePublished:
		if p.Release == nil {
			return "", 0
		}
		text = fmt.Sprintf("Release %s published%s, %s since the last one", wpf.linkFormatter(p.URL, p.Release.Tag), in, changesCount(p.Release.Changes))
		color = greenColor
	case webhook_module.HookEventPolicyChanged:
		if p.Policy == nil {
			return "", 0
		}
		text, color = fmt.Sprintf("%s%s: %s", wpf.linkFormatter(p.URL, "Policy changed"), in, p.Policy.Summary), orangeColor
	default:
		return "", 0
	}
	if wpf.withSender && p.Sender != "" {
		text += " by " + wpf.nameFormatter(p.Sender)
	}
	return text, color
}

func changesCount(n int64) string {
	if n == 1 {
		return "1 change"
	}
	return fmt.Sprintf("%d changes", n)
}
