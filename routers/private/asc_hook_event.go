// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"net/http"
	"regexp"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/context"
	webhook_service "forgejo.org/services/webhook"
)

var (
	ascHookCommitRe = regexp.MustCompile(`^[0-9a-f]{1,128}$`)
	ascHookStateRe  = regexp.MustCompile(`^[a-z_]{1,32}$`)
	ascHookLoginRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
)

// ascHookEventMaxSummary is the longest summary of a policy change.
const ascHookEventMaxSummary = 200

// The calls that queue the events. Tests replace them.
var (
	ascHookRun    = webhook_service.AscRun
	ascHookPolicy = webhook_service.AscPolicy
)

// ascHookEventRequest is the body of `POST /api/internal/asc/hook-event`. asc serve sends it from its outbox when an
// agent run started or finished, or when the policy on trunk changed, so the webhooks of the repository that send the
// event are told. The X-Asc-Event-Id header names the event: a redelivery of it queues nothing new.
type ascHookEventRequest struct {
	Event   string `json:"event"` // agent_run.started, agent_run.finished or policy.changed
	Repo    string `json:"repo"`  // owner/name
	Run     uint64 `json:"run,omitempty"`
	State   string `json:"state,omitempty"` // how the run ended
	Changes uint64 `json:"changes,omitempty"`
	Before  string `json:"before,omitempty"` // the trunk commits around a policy change
	After   string `json:"after,omitempty"`
	Summary string `json:"summary,omitempty"`
	Sender  string `json:"sender,omitempty"` // who caused it, a login of this hub
}

func (r *ascHookEventRequest) valid() bool {
	if r.Sender != "" && !ascHookLoginRe.MatchString(r.Sender) {
		return false
	}
	switch webhook_module.HookEventType(r.Event) {
	case webhook_module.HookEventAgentRunStarted:
		return r.Run > 0 && r.Run < 1<<53 && r.State == "" && r.Before == "" && r.After == "" && r.Summary == ""
	case webhook_module.HookEventAgentRunFinished:
		return r.Run > 0 && r.Run < 1<<53 && ascHookStateRe.MatchString(r.State) && r.Before == "" && r.After == "" && r.Summary == ""
	case webhook_module.HookEventPolicyChanged:
		return r.Run == 0 && r.State == "" && r.Changes == 0 && ascHookCommitRe.MatchString(r.Before) && ascHookCommitRe.MatchString(r.After) &&
			r.Summary != "" && len(r.Summary) <= ascHookEventMaxSummary && !strings.ContainsAny(r.Summary, "\r\n<>&")
	}
	return false
}

// AscHookEvent answers `POST /api/internal/asc/hook-event`: it queues the event for the hooks of the repository, and
// for those of its owner and the system ones, that send it. It answers 200 also when the repository is gone.
func AscHookEvent(ctx *context.PrivateContext) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotFound)
		return
	}
	req := &ascHookEventRequest{}
	if err := decodeAscStrict(ctx, req); err != nil {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: err.Error()})
		return
	}
	owner, name, ok := strings.Cut(req.Repo, "/")
	eventID := ctx.Req.Header.Get("X-Asc-Event-Id")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || len(req.Repo) > ascRequestMergeMaxRepo ||
		!req.valid() || !ascEventIDRe.MatchString(eventID) {
		ctx.JSON(http.StatusBadRequest, private.Response{UserMsg: "invalid request"})
		return
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		if repo_model.IsErrRepoNotExist(err) {
			ctx.JSON(http.StatusOK, private.Response{})
			return
		}
		log.Error("asc hook event: repository %q: %v", req.Repo, err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	event := webhook_module.HookEventType(req.Event)
	if event == webhook_module.HookEventPolicyChanged {
		err = ascHookPolicy(ctx, repo, eventID, req.Before, req.After, req.Summary, req.Sender)
	} else {
		err = ascHookRun(ctx, repo, eventID, event, req.Run, req.State, req.Changes)
	}
	if err != nil {
		log.Error("asc hook event: %s of %s: %v", req.Event, req.Repo, err)
		ctx.JSON(http.StatusInternalServerError, private.Response{Err: "internal error"})
		return
	}
	ctx.JSON(http.StatusOK, private.Response{})
}
