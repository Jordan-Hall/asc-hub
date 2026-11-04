// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"net/http"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascbabysit"
	"forgejo.org/services/context"
)

// AscBabysitPull is the line of the change page that says an agent watches the checks of the change.
type AscBabysitPull struct {
	Agent   string
	Left    int    // runs the hub may still start
	StopURL string // "" when the person may not stop the watch
}

// ascBabysitMayStop: the person who asked, someone who may change pull requests here, or an admin.
func ascBabysitMayStop(ctx *context.Context, w *ascbabysit.AscBabysit) bool {
	return ctx.IsSigned && (ctx.Doer.ID == w.DoerID || ctx.Doer.IsAdmin || ctx.Repo.CanWrite(unit.TypePullRequests))
}

// prepareAscBabysitPull puts the watch of an open change into ctx.Data["AscBabysitPull"].
func prepareAscBabysitPull(ctx *context.Context, pull *issues_model.PullRequest) {
	if !setting.Asc.Enabled || pull == nil || pull.HasMerged || (pull.Issue != nil && pull.Issue.IsClosed) {
		return
	}
	w, err := ascbabysit.Get(ctx, pull.ID)
	if err != nil || w == nil {
		return
	}
	out := &AscBabysitPull{Agent: w.Agent, Left: w.Left()}
	if ascBabysitMayStop(ctx, w) {
		out.StopURL = fmt.Sprintf("%s/pulls/%d/asc/babysit/stop", ctx.Repo.RepoLink, pull.Index)
	}
	ctx.Data["AscBabysitPull"] = out
}

// AscStopBabysit ends the watch of the checks of a change.
func AscStopBabysit(ctx *context.Context) {
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	w, err := ascbabysit.Get(ctx, issue.PullRequest.ID)
	if err != nil {
		ctx.ServerError("ascbabysit.Get", err)
		return
	}
	if w != nil {
		if !ascBabysitMayStop(ctx, w) {
			ctx.Error(http.StatusForbidden)
			return
		}
		if err := ascbabysit.Stop(ctx, issue.PullRequest.ID); err != nil {
			ctx.ServerError("ascbabysit.Stop", err)
			return
		}
		log.Info("asc audit: babysit: repository %d, pull request %d: %s stopped by user %d", ctx.Repo.Repository.ID, issue.PullRequest.ID, w.Agent, ctx.Doer.ID)
	}
	ctx.Redirect(issue.Link())
}
