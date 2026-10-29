// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"time"

	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"

	"github.com/google/uuid"
)

// The agents and workflows pages (PLAN unit H5) share one gate and one way to fail.
//
// Trust. Every page of the area is reached through the repository's code-read check, which the gate repeats,
// and then asks for what the action needs:
//
//   - read pages (lists, run pages, the event feed): a signed-in person who can read the code. Anonymous visitors
//     of a public repository get 404, because the task text and the tool output of a run are not public pages.
//   - changing pages (start, stop, steer, trigger, approve): code write access, and the repository not archived
//     where the action starts new work.
//   - token pages: repository admin.
//
// The agent parts also need the person's "agent features" switch (ascagentui.FeaturesEnabled), which is off by
// default; with it off they answer 404 exactly as if they did not exist. The hub is the first gate. asc then
// authorises the same action again: reads carry the internal token after the checks above, and every call that
// changes something carries a one-use token of the signed-in person (ascagentui.UserCall), so asc decides and
// audits it as that person. A run, token or workflow run named in a URL is accepted only when the answer says it
// belongs to this repository.
//
// What asc sends back is untrusted. Text is cleaned (ascagentui.CleanText) and escaped by the template, the
// browser script assigns it to textContent, ids are checked for their shape, and the text of an asc error is
// never shown: only its codes are used (ascagentui.Classify).

type ascAgentsLevel int

const (
	ascAgentsRead ascAgentsLevel = iota
	ascAgentsWrite
	ascAgentsAdmin
)

// ascAgentsOpts says what a page needs besides its level.
type ascAgentsOpts struct {
	level ascAgentsLevel
	// noFeatures lets a page that is not an agent surface (the workflow lists and details) be served when the
	// person has not turned agent features on.
	noFeatures bool
	// notArchived refuses the action for an archived repository.
	notArchived bool
}

// ascAgentsEnv is what a handler gets once the gate is passed.
type ascAgentsEnv struct {
	name     string // the asc repository name, "owner/repo"
	link     string // the repository's path, without trailing slash
	features bool   // the person's agent features switch
}

func (e *ascAgentsEnv) ref() *ascv1.RepoRef { return &ascv1.RepoRef{Name: e.name} }

// Time limits of the calls of these pages.
const (
	ascAgentsCallTimeout = ascclient.DefaultCallTimeout
	// ascAgentsMaxFormBytes bounds a form body the handlers read into memory.
	ascAgentsMaxFormBytes = ascagentui.MaxTaskBytes + 8192
)

// agentsReadAllowed is the rule for seeing anything about an agent run, as a function of its inputs so that it can be
// tested over every case: a signed-in person who can read the code and the Actions of the repository and for whom the
// agent features are on.
func agentsReadAllowed(signedIn, hasDoer, canReadCode, canReadActions, features bool) bool {
	return signedIn && hasDoer && canReadCode && canReadActions && features
}

// ascAgentsCanReadRuns says whether the viewer may see the agent runs of the repository: the agents pages and every
// card that shows data of a run (the brief of a change names the run behind it) use this one rule, so they cannot
// drift apart.
func ascAgentsCanReadRuns(ctx *context.Context) bool {
	if ctx.Doer == nil {
		return false
	}
	return agentsReadAllowed(ctx.IsSigned, true, ctx.Repo.CanRead(unit.TypeCode), ctx.Repo.CanRead(unit.TypeActions), ascagentui.FeaturesEnabled(ctx, ctx.Doer))
}

// ascAgentsGate checks who may use the page and returns the repository's asc name. It answers the request itself
// (and returns false) when the check fails. Every refusal that could tell a stranger something exists is a 404.
func ascAgentsGate(ctx *context.Context, o ascAgentsOpts) (*ascAgentsEnv, bool) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("asc agents", nil)
		return nil, false
	}
	if ctx.Doer == nil || !ctx.IsSigned || !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("asc agents", nil)
		return nil, false
	}
	features := ascagentui.FeaturesEnabled(ctx, ctx.Doer)
	if !features && !o.noFeatures {
		ctx.NotFound("asc agents", nil)
		return nil, false
	}
	if features && !ascAgentsCanReadRuns(ctx) {
		// the agents area shows runs, which belong to the Actions unit
		ctx.NotFound("asc agents", nil)
		return nil, false
	}
	switch o.level {
	case ascAgentsWrite:
		if !ctx.Repo.CanWrite(unit.TypeCode) {
			ctx.Error(http.StatusForbidden)
			return nil, false
		}
	case ascAgentsAdmin:
		if !ctx.Repo.IsAdmin() {
			ctx.NotFound("asc agents", nil)
			return nil, false
		}
	}
	if o.notArchived && repo.IsArchived {
		ctx.Error(http.StatusForbidden)
		return nil, false
	}
	name, err := ascGraphRepoName(repo)
	if err != nil {
		ctx.NotFound("asc agents", nil)
		return nil, false
	}
	ctx.Data["AscAgentFeatures"] = features
	ctx.Data["AscAgentsCanStart"] = features && ctx.Repo.CanWrite(unit.TypeCode) && !repo.IsArchived
	ctx.Data["AscAgentsCanAdmin"] = features && ctx.Repo.IsAdmin()
	ctx.Data["AscAgentsBase"] = ctx.Repo.RepoLink + "/asc/agents"
	ctx.Data["AscWorkflowsBase"] = ctx.Repo.RepoLink + "/asc/workflows"
	return &ascAgentsEnv{name: name, link: ctx.Repo.RepoLink, features: features}, true
}

// ascAgentsReadCtx is the context of a read call: the internal token, after the gate.
func ascAgentsReadCtx(ctx *context.Context) (gocontext.Context, gocontext.CancelFunc) {
	return gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascAgentsCallTimeout)
}

// ascAgentsWriteCtx is the context of a call that changes something: a one-use token of the signed-in person.
// The cleanup must always run. When it fails the request is answered with 502 here and ok is false.
func ascAgentsWriteCtx(ctx *context.Context, opID string) (callCtx gocontext.Context, done func(), ok bool) {
	uctx, cleanup, err := ascagentui.UserCall(ctx.Req.Context(), ctx.Doer, ctx.Repo.Repository)
	if err != nil {
		log.Error("asc agents: no user credential for user %d: %v", ctx.Doer.ID, err)
		ctx.Error(http.StatusBadGateway)
		return nil, func() {}, false
	}
	if id, err := uuid.Parse(opID); err == nil && id.Version() == 7 {
		uctx = ascclient.WithOpID(uctx, id)
	}
	tctx, cancel := gocontext.WithTimeout(uctx, ascAgentsCallTimeout)
	return tctx, func() { cancel(); cleanup() }, true
}

// ascAgentsNewOpID makes the id a form carries so a double submit is one action (asc answers a retried id with the
// first answer).
func ascAgentsNewOpID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return ""
	}
	return id.String()
}

// ascAgentsFail answers a page whose call failed. A feature that is off is explained with 200; everything else
// uses the status of the failure. Only fixed sentences and cleaned details reach the page.
func ascAgentsFail(ctx *context.Context, tpl base.TplName, what string, err error) {
	f := ascagentui.Classify(err)
	if f == nil {
		return
	}
	if ctx.Req.Context().Err() != nil {
		return // the browser went away
	}
	if f.Unavailable {
		log.Error("asc agents: %s failed: %s", what, ascagentuiLogLine(f))
	}
	ctx.Data["AscAgentsFailure"] = f
	status := f.HTTP
	if f.Disabled {
		ctx.Data["AscAgentsDisabled"] = true
		status = http.StatusOK
	}
	ctx.HTML(status, tpl)
}

// ascagentuiLogLine is what the log keeps of a failure: the classification, never asc's text.
func ascagentuiLogLine(f *ascagentui.Failure) string {
	if f.Code != "" {
		return f.Code
	}
	return f.Message
}

// ascAgentsFlashFail puts a failure of a changing action into the flash and returns to the page.
func ascAgentsFlashFail(ctx *context.Context, redirect, what string, err error) {
	f := ascagentui.Classify(err)
	if f == nil || ctx.Req.Context().Err() != nil {
		return
	}
	if f.Unavailable {
		log.Error("asc agents: %s failed: %s", what, ascagentuiLogLine(f))
	}
	msg := f.Message
	for _, d := range f.Details {
		msg += " " + d + "."
	}
	ctx.Flash.Error(msg)
	ctx.Redirect(redirect)
}

// ascAgentsPageToken accepts the opaque token of asc's paging and nothing else.
func ascAgentsPageToken(s string) (string, bool) {
	if len(s) > 512 {
		return "", false
	}
	for i := range len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '=' || c == '.' {
			continue
		}
		return "", false
	}
	return s, true
}

// ascAgentsParseID reads a run id from the URL: digits only, no leading zero, within uint32 range.
func ascAgentsParseID(s string) (uint64, bool) {
	if s == "" || len(s) > 10 || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	var n uint64
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + uint64(s[i]-'0')
	}
	if n == 0 || n > 1<<32-1 {
		return 0, false
	}
	return n, true
}

// ascAgentsMaySeeRun is the rule for what a person typed or an agent wrote on a run: the task text, the plan, the
// requests, the checkpoints, the log and the failure text. The person who started the run and an administrator of the
// repository see them; everyone else who may read the code sees the run's metadata (state, agent, counts, meters) and a
// neutral label.
//
// The creator is the hub's immutable user id that asc keeps on the run (AgentRun.created_by_id), never a user name: a renamed
// person keeps their runs, and a new person who takes the old name does not get them. It fails closed: no signed-in person,
// or a run without an id (one a token started), is seen by administrators only.
func ascAgentsMaySeeRun(ctx *context.Context, createdByID uint64) bool {
	if ctx.Doer == nil || !ctx.IsSigned || ctx.Repo.Repository == nil {
		return false
	}
	if ctx.Repo.IsAdmin() {
		return true
	}
	return createdByID != 0 && ctx.Doer.ID > 0 && createdByID == uint64(ctx.Doer.ID)
}

func ascAgentsNoStore(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set("Pragma", "no-cache")
}

// ascAgentsClock is replaced by tests.
var ascAgentsClock = time.Now
