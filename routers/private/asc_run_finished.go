// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	gocontext "context"
	"net/http"
	"strings"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// ascRunFinishedRequest is the body of `POST /api/internal/asc/agent-run-finished`. asc serve sends it from its outbox
// when an agent run has ended with changes (at least once, so it can arrive twice). Like every internal call it carries
// the internal token. The hub does not trust the body for anything but which run to look at: it reads the run from asc
// again (services/ascagentui/runfinished.go).
type ascRunFinishedRequest struct {
	Repo    string `json:"repo"`    // owner/name
	Run     uint64 `json:"run"`     // the run id
	State   string `json:"state"`   // the state the run ended in
	Changes uint64 `json:"changes"` // how many changes the run made
}

// ascRunFinishedResponse says what the hub did, for the log of asc's outbox.
type ascRunFinishedResponse struct {
	Outcome string `json:"outcome"`
	Opened  int    `json:"opened"`
	Linked  int    `json:"linked"`
}

// ascOpenFinishedRun opens the run for review. Tests replace it.
var ascOpenFinishedRun = func(ctx gocontext.Context, repo *repo_model.Repository, id uint64) (*ascagentui.RunFinishedResult, error) {
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, err
	}
	return ascagentui.OpenFinishedRun(ctx, conn, repo, id, ascagentui.DefaultRunPullSeams, time.Now())
}

// AscRunFinished answers `POST /api/internal/asc/agent-run-finished`: a run that succeeded with changes is opened for
// review, as the "Open for review" button of its page does. It answers 200 when there is nothing more to do (also when
// the hub chose not to) and 503 when a change is not in the git view yet, which makes asc's outbox deliver the event
// again after a pause.
func AscRunFinished(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}
	req := &ascRunFinishedRequest{}
	if err := decodeAscStrict(ctx, req); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}
	owner, name, ok := strings.Cut(req.Repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || len(req.Repo) > ascRequestMergeMaxRepo ||
		req.Run == 0 || req.Run >= 1<<53 || len(req.State) == 0 || len(req.State) > 32 {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: "invalid request"})
		return
	}
	if !setting.Asc.AutoOpenRunPulls || req.State != "succeeded" || req.Changes == 0 {
		ctx.JSON(http.StatusOK, ascRunFinishedResponse{Outcome: ascagentui.RunFinishedSkipped})
		return
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			ctx.JSON(http.StatusOK, ascRunFinishedResponse{Outcome: ascagentui.RunFinishedSkipped})
			return
		}
		log.Error("asc run finished: repository %q: %v", req.Repo, err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	res, err := ascOpenFinishedRun(ctx, repo, req.Run)
	if err != nil {
		log.Error("asc run finished: opening run %d of %s for review: %v", req.Run, req.Repo, err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	if res.Outcome == ascagentui.RunFinishedSkipped {
		log.Info("asc run finished: run %d of %s is not opened: %s", req.Run, req.Repo, res.Reason)
	}
	if res.Failed > 0 {
		log.Error("asc run finished: %d change(s) of run %d of %s could not be opened; the run page has the button", res.Failed, req.Run, req.Repo)
	}
	status := http.StatusOK
	if res.Outcome == ascagentui.RunFinishedWaiting {
		status = http.StatusServiceUnavailable
	}
	ctx.JSON(status, ascRunFinishedResponse{Outcome: res.Outcome, Opened: res.Opened, Linked: res.Linked})
}
