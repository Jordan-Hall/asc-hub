// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The operation log and the undo of an operation (Stacks.ListRefLog, Stacks.UndoRef).
//
//	GET  /{owner}/{repo}/asc/ops                 the operations on the repository, newest first
//	GET  /{owner}/{repo}/asc/ops/undo?ref&seq    what undoing one operation would do (a dry run), and the confirmation
//	POST /{owner}/{repo}/asc/ops/undo            undo it
//
// Everything is made as the signed-in person (asctrust.UserCall), so asc applies its own rules to the same request.
// The log is the person's history, so a visitor who is not signed in is refused. An undo needs the right to write
// the code, and the person's own operation: the row is read again before anything is written, and the undo is made
// only when it is still the newest row of its ref and carries the two heads the person was shown. A change of state
// is a POST behind the router's CSRF check, and the form carries the token.
const (
	tplAscOps     base.TplName = "repo/asc_ops"
	tplAscOpsUndo base.TplName = "repo/asc_ops_undo"

	maxOpsForm = 8 << 10
)

// ascOpsPage is the template data under .AscOps.
type ascOpsPage struct {
	Rows        []asctrust.OpRow
	Ref         string
	Mine        bool
	NextURL     string
	OpsURL      string
	CanUndo     bool
	Problem     string
	Unsupported bool
	Kind        string
	Total       int
	Chips       []ascOpsKindChip
	Actors      []ascOpsActorBar
	AllURL      string
	ExportURL   string
}

// ascUndoPage is the template data under .AscUndo.
type ascUndoPage struct {
	Preview *asctrust.UndoPreview
	OpsURL  string
	Problem string
}

func ascOpsURL(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/asc/ops"
}

func ascUndoWho(ctx *context.Context) asctrust.UndoWho {
	return asctrust.UndoWho{ID: ctx.Doer.ID, Name: ctx.Doer.Name}
}

// AscOps serves GET /{owner}/{repo}/asc/ops.
func AscOps(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, false)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.ops.title")
	ctx.Data["PageIsAscOps"] = true
	p := &ascOpsPage{
		OpsURL:  ascOpsURL(repo),
		Ref:     strings.TrimSpace(ctx.FormString("ref")),
		Mine:    ctx.FormBool("mine"),
		CanUndo: ctx.Repo.CanWrite(unit.TypeCode),
	}
	ctx.Data["AscOps"] = p
	if !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscOps)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc ops: no connection to asc serve: %v", err)
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscOps)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("ops credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscOps)
		return
	}
	defer release()
	page, err := asctrust.LoadOpLog(callCtx, conn, name, ascUndoWho(ctx), asctrust.OpLogQuery{
		Ref: p.Ref, Mine: p.Mine, PageToken: ctx.FormString("page"),
	})
	if err != nil {
		f := trustweb.FailureOf("ops load", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc ops", nil)
			return
		}
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscOps)
		return
	}
	kind := strings.TrimSpace(ctx.FormString("kind"))
	if !ascOpsKindKnown(kind) {
		ctx.NotFound("asc ops", nil)
		return
	}
	p.Rows = ascOpsBoard(p, kind, page.Rows)
	p.ExportURL = p.OpsURL + "/export"
	if p.Ref != "" || p.Mine {
		eq := url.Values{}
		if p.Ref != "" {
			eq.Set("ref", p.Ref)
		}
		if p.Mine {
			eq.Set("mine", "1")
		}
		p.ExportURL += "?" + eq.Encode()
	}
	if page.Next != "" {
		q := url.Values{"page": {page.Next}}
		if p.Ref != "" {
			q.Set("ref", p.Ref)
		}
		if p.Mine {
			q.Set("mine", "1")
		}
		if kind != "" {
			q.Set("kind", kind)
		}
		p.NextURL = p.OpsURL + "?" + q.Encode()
	}
	ctx.HTML(http.StatusOK, tplAscOps)
}

// ascUndoFail answers a failed undo or preview. A refusal of the person (403, 404) is the status of the page; a state
// that moved is a message on the log, where the person can look again.
func ascUndoFail(ctx *context.Context, op string, err error) {
	f := trustweb.FailureOf(op, err)
	switch f.Status {
	case http.StatusForbidden:
		log.Warn("asc %s refused for user %d: %v", op, ctx.Doer.ID, err)
		ctx.Error(http.StatusForbidden, f.Localized(ctx))
	case http.StatusNotFound:
		ctx.NotFound("asc "+op, nil)
	default:
		ctx.Flash.Error(f.Localized(ctx))
		ctx.Redirect(ascOpsURL(ctx.Repo.Repository))
	}
}

// AscOpsUndoPreview serves GET /{owner}/{repo}/asc/ops/undo: the dry run and the confirmation form.
func AscOpsUndoPreview(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, true)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	seq, err := strconv.ParseUint(ctx.FormString("seq"), 10, 64)
	if err != nil || seq == 0 {
		ctx.NotFound("asc undo", nil)
		return
	}
	ref := ctx.FormString("ref")
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.ops.undo_title")
	ctx.Data["PageIsAscOps"] = true
	p := &ascUndoPage{OpsURL: ascOpsURL(repo)}
	ctx.Data["AscUndo"] = p
	if !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		ctx.NotFound("asc undo", nil)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc undo: no connection to asc serve: %v", err)
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscOpsUndo)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		f := trustweb.FailureOf("undo credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscOpsUndo)
		return
	}
	defer release()
	pv, err := asctrust.PreviewUndo(callCtx, conn, name, ascUndoWho(ctx), ref, seq)
	if err != nil {
		ascUndoFail(ctx, "undo preview", err)
		return
	}
	p.Preview = pv
	ctx.HTML(http.StatusOK, tplAscOpsUndo)
}

// AscOpsUndo handles POST /{owner}/{repo}/asc/ops/undo.
func AscOpsUndo(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, true)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	if !trustweb.ParseForm(ctx, maxOpsForm) {
		return
	}
	seq, err := strconv.ParseUint(ctx.FormString("seq"), 10, 64)
	if err != nil || seq == 0 {
		ctx.Error(http.StatusBadRequest)
		return
	}
	conf := asctrust.UndoConfirmation{
		Ref: ctx.FormString("ref"), Seq: seq,
		Current: ctx.FormString("current"), Restore: ctx.FormString("restore"),
		Reason: strings.TrimSpace(trustweb.Lines(ctx.FormString("reason"))), Op: ctx.FormString("op"),
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		ctx.NotFound("asc undo", nil)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc undo: no connection to asc serve: %v", err)
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(ascOpsURL(repo))
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		ascUndoFail(ctx, "undo credential", err)
		return
	}
	defer release()
	done, err := asctrust.ConfirmUndo(callCtx, conn, name, ascUndoWho(ctx), conf)
	if err != nil {
		ascUndoFail(ctx, "undo", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_trust.ops.undone", done.Ref))
	ctx.Redirect(ascOpsURL(repo))
}
