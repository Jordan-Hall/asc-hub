// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/url"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/setting"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// AscStackActions is what the viewer may start from the stack panel of a pull request. Each action is a link to
// the confirmation page (asc_confirm.go), which runs the dry run; none of them writes. The service checks the
// permission again when the form is posted, so these flags only decide which buttons show.
type AscStackActions struct {
	CanQueue   bool // merge when ready
	CanUnqueue bool
	CanMerge   bool // merge the stack up to this pull request now
	CanRestack bool
	CanRevert  bool
	// QueueState is the state of this pull request's queue entry ("" when it has none).
	QueueState string
	// ConfirmURL is the confirmation page of this pull request; append ?op=...
	ConfirmURL string
	// QueueURL is the queue page of the base branch.
	QueueURL string
}

// ascLoadIssue loads the issue of pr and its repository, which Issue.Link needs.
func ascLoadIssue(ctx gocontext.Context, pr *issues_model.PullRequest) error {
	if err := pr.LoadIssue(ctx); err != nil {
		return err
	}
	return pr.Issue.LoadRepo(ctx)
}

// ascActiveQueueState reports whether a queue state is one a pull request waits in.
func ascActiveQueueState(state string) bool {
	return state == automerge.AscQueueQueued || state == automerge.AscQueueBlocked || state == automerge.AscQueueLanding
}

// prepareAscStackActions puts AscActions and AscQueueStates into ctx.Data for a stacked pull request of a
// repository stored in asc, for a signed-in viewer. It does nothing otherwise. A failure to read the state
// only hides the buttons: the pull request page still renders.
func prepareAscStackActions(ctx *context.Context, pull *issues_model.PullRequest) {
	if !ctx.IsSigned || !setting.Asc.Enabled || pull == nil {
		return
	}
	if err := pull.LoadBaseRepo(ctx); err != nil {
		return
	}
	if backed, err := automerge.AscQueueBackedBy(pull.BaseRepo); err != nil || !backed {
		return
	}
	if err := ascLoadIssue(ctx, pull); err != nil {
		return
	}
	a := &AscStackActions{
		ConfirmURL: pull.Issue.Link() + "/asc/confirm",
		QueueURL:   ctx.Repo.RepoLink + "/asc/queue?branch=" + url.QueryEscape(pull.BaseBranch),
	}
	archived := pull.BaseRepo.IsArchived
	switch {
	case pull.HasMerged:
		a.CanRevert = !archived && pull_service.AscMayRevert(ctx.Doer, ctx.Repo.Permission)
	case !pull.Issue.IsClosed && !archived:
		if ok, err := pull_service.IsUserAllowedToMerge(ctx, pull, ctx.Repo.Permission, ctx.Doer); err == nil && ok {
			a.CanQueue, a.CanMerge = true, true
		}
		if ok, err := pull_service.AscMayRewrite(ctx, ctx.Doer, pull); err == nil && ok {
			a.CanRestack = true
		}
		entries, err := automerge.AscQueueEntries(ctx, []int64{pull.ID})
		if err != nil {
			return
		}
		if e := entries[pull.ID]; e != nil && ascActiveQueueState(e.State) {
			a.QueueState = e.State
			a.CanUnqueue = e.DoerID == ctx.Doer.ID || a.CanMerge
			a.CanQueue = false
		}
	}
	ctx.Data["AscActions"] = a
	ctx.Data["AscQueueStates"] = ascQueueStatesByIndex(ctx, pull)
}

// ascQueueStatesByIndex maps the index of each pull request waiting in the queue of pull's base branch to its
// state, for the labels of the stack panel.
func ascQueueStatesByIndex(ctx *context.Context, pull *issues_model.PullRequest) map[int64]string {
	out := map[int64]string{}
	entries, err := automerge.AscQueueList(ctx, pull.BaseRepoID, pull.BaseBranch)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !ascActiveQueueState(e.State) {
			continue
		}
		p, err := issues_model.GetPullRequestByID(ctx, e.PullID)
		if err != nil {
			continue
		}
		out[p.Index] = e.State
	}
	return out
}
