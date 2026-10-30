// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The account pages that asc serve answers for (docs/BUILDOUT.md sections 26 and 27):
//
//	GET  /user/settings/asc/sessions             the tokens, SSH keys and agent tokens of the signed-in person
//	POST /user/settings/asc/sessions/revoke      end one of them (field id, op)
//	POST /user/settings/asc/sessions/revoke-all  end all of them (field op)
//	GET  /user/settings/asc/export               the exports of the data of the person
//	POST /user/settings/asc/export               ask for a new export (field op)
//	GET  /user/settings/asc/export/{id}          download a ready archive
//	POST /user/settings/asc/export/{id}/delete   delete one (field op)
//
// All of it is about the account of the signed-in person and has no way to name another one: the login comes from
// ctx.Doer, never from the request, and the hub signs a claim for "user:<login>" (asctrust.CallAccount). A page
// whose service the server does not advertise says so, and shows nothing half drawn.
const (
	tplAscSessions base.TplName = "user/settings/asc_sessions"
	tplAscExport   base.TplName = "user/settings/asc_export"

	maxAscAccountForm = 8 << 10
)

// ascAccountGuard fails closed: [asc] on, and a signed-in person who is not an organisation.
func ascAccountGuard(ctx *context.Context) bool {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc account", nil)
		return false
	}
	if !ctx.IsSigned || ctx.Doer == nil || ctx.Doer.IsOrganization() {
		ctx.Error(http.StatusForbidden)
		return false
	}
	return true
}

// ascAccountCall returns the connection to asc and a context whose calls are made as the signed-in person. When it
// returns false the answer is written (or, for a redirecting action, problem holds the sentence).
func ascAccountCall(ctx *context.Context, op string) (conn grpc.ClientConnInterface, callCtx gocontext.Context, release func(), problem string, status int) {
	c, err := asctrust.Conn()
	if err != nil {
		log.Error("asc %s: no connection to asc serve: %v", op, err)
		return nil, nil, nil, ctx.Locale.TrString("asc_shell.acct_unreachable"), http.StatusBadGateway
	}
	cc, rel, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	if err != nil {
		f := trustweb.FailureOf(op+" credential", err)
		return nil, nil, nil, f.Localized(ctx), f.Status
	}
	return c, cc, rel, "", 0
}

// ascAccountBack redirects with a flash.
func ascAccountBack(ctx *context.Context, to string, success, problem string) {
	if problem != "" {
		ctx.Flash.Error(problem)
	} else if success != "" {
		ctx.Flash.Success(success)
	}
	ctx.Redirect(to)
}

// ------------------------------------------------------------------------------------------------ sessions

type ascSessionsPage struct {
	Rows        []asctrust.SessionRow
	Problem     string
	Unsupported bool
	URL         string
	RevokeOps   map[string]string
	RevokeAllOp string
	Now         time.Time
}

// AscSessions serves GET /user/settings/asc/sessions.
func AscSessions(ctx *context.Context) {
	if !ascAccountGuard(ctx) {
		return
	}
	trustweb.NoStore(ctx)
	p := &ascSessionsPage{URL: setting.AppSubURL + "/user/settings/asc/sessions", RevokeOps: map[string]string{}, RevokeAllOp: asctrust.NewOpID(), Now: time.Now()}
	ctx.Data["Title"] = ctx.Tr("asc_shell.sn_sessions")
	ctx.Data["PageIsSettingsAscSessions"] = true
	ctx.Data["AscSessions"] = p
	status := http.StatusOK
	defer func() { ctx.HTML(status, tplAscSessions) }()

	if !asctrust.HasFeature(ctx, asctrust.FeatureSessions) {
		p.Unsupported = true
		return
	}
	conn, callCtx, release, problem, st := ascAccountCall(ctx, "sessions")
	if problem != "" {
		p.Problem, status = problem, st
		return
	}
	defer release()
	rows, err := asctrust.LoadSessions(callCtx, conn, ctx.Doer.LowerName)
	if err != nil {
		f := trustweb.FailureOf("sessions load", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	p.Rows = rows
	for _, r := range rows {
		p.RevokeOps[r.ID] = asctrust.NewOpID()
	}
}

// AscSessionsRevoke serves POST /user/settings/asc/sessions/revoke with the fields id and op.
func AscSessionsRevoke(ctx *context.Context) {
	if !ascAccountGuard(ctx) || !trustweb.ParseForm(ctx, maxAscAccountForm) {
		return
	}
	to := setting.AppSubURL + "/user/settings/asc/sessions"
	conn, callCtx, release, problem, _ := ascAccountCall(ctx, "sessions")
	if problem != "" {
		ascAccountBack(ctx, to, "", problem)
		return
	}
	defer release()
	if err := asctrust.RevokeSession(callCtx, conn, ctx.Doer.LowerName, ctx.Req.PostFormValue("id"), ctx.Req.PostFormValue("op")); err != nil {
		ascAccountBack(ctx, to, "", trustweb.FailureOf("sessions revoke", err).Localized(ctx))
		return
	}
	ascAccountBack(ctx, to, ctx.Locale.TrString("asc_shell.sess_revoked"), "")
}

// AscSessionsRevokeAll serves POST /user/settings/asc/sessions/revoke-all with the field op.
func AscSessionsRevokeAll(ctx *context.Context) {
	if !ascAccountGuard(ctx) || !trustweb.ParseForm(ctx, maxAscAccountForm) {
		return
	}
	to := setting.AppSubURL + "/user/settings/asc/sessions"
	conn, callCtx, release, problem, _ := ascAccountCall(ctx, "sessions")
	if problem != "" {
		ascAccountBack(ctx, to, "", problem)
		return
	}
	defer release()
	n, err := asctrust.RevokeOtherSessions(callCtx, conn, ctx.Doer.LowerName, "", ctx.Req.PostFormValue("op"))
	if err != nil {
		ascAccountBack(ctx, to, "", trustweb.FailureOf("sessions revoke all", err).Localized(ctx))
		return
	}
	ascAccountBack(ctx, to, ctx.Locale.TrString("asc_shell.sess_revoked_n", n), "")
}

// ------------------------------------------------------------------------------------------------ data export

type ascExportPage struct {
	Rows        []asctrust.ExportRow
	Problem     string
	Unsupported bool
	URL         string
	RequestOp   string
	DeleteOps   map[string]string
	Now         time.Time
	// Busy is set when an export is still being made, so the page does not offer a second one.
	Busy bool
}

// AscExport serves GET /user/settings/asc/export.
func AscExport(ctx *context.Context) {
	if !ascAccountGuard(ctx) {
		return
	}
	trustweb.NoStore(ctx)
	p := &ascExportPage{URL: setting.AppSubURL + "/user/settings/asc/export", RequestOp: asctrust.NewOpID(), DeleteOps: map[string]string{}, Now: time.Now()}
	ctx.Data["Title"] = ctx.Tr("asc_shell.sn_export")
	ctx.Data["PageIsSettingsAscExport"] = true
	ctx.Data["AscExport"] = p
	status := http.StatusOK
	defer func() { ctx.HTML(status, tplAscExport) }()

	if !asctrust.HasFeature(ctx, asctrust.FeatureDataExport) {
		p.Unsupported = true
		return
	}
	conn, callCtx, release, problem, st := ascAccountCall(ctx, "export")
	if problem != "" {
		p.Problem, status = problem, st
		return
	}
	defer release()
	rows, err := asctrust.ListExports(callCtx, conn)
	if err != nil {
		f := trustweb.FailureOf("export list", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	p.Rows = rows
	for _, r := range rows {
		p.DeleteOps[r.ID] = asctrust.NewOpID()
		p.Busy = p.Busy || r.Waiting()
	}
}

// AscExportRequest serves POST /user/settings/asc/export with the field op.
func AscExportRequest(ctx *context.Context) {
	if !ascAccountGuard(ctx) || !trustweb.ParseForm(ctx, maxAscAccountForm) {
		return
	}
	to := setting.AppSubURL + "/user/settings/asc/export"
	conn, callCtx, release, problem, _ := ascAccountCall(ctx, "export")
	if problem != "" {
		ascAccountBack(ctx, to, "", problem)
		return
	}
	defer release()
	if _, err := asctrust.RequestExport(callCtx, conn, ctx.Req.PostFormValue("op")); err != nil {
		ascAccountBack(ctx, to, "", trustweb.FailureOf("export request", err).Localized(ctx))
		return
	}
	ascAccountBack(ctx, to, ctx.Locale.TrString("asc_shell.export_started"), "")
}

// AscExportDownload serves GET /user/settings/asc/export/{id}.
//
// The archive is copied to a scratch file first, like the audit export, so that a stream that breaks, or that ends
// before its last chunk, is an error page and never a short file with a 200.
func AscExportDownload(ctx *context.Context) {
	if !ascAccountGuard(ctx) {
		return
	}
	id := ctx.Params(":id")
	conn, callCtx, release, problem, status := ascAccountCall(ctx, "export")
	if problem != "" {
		ctx.Error(status, problem)
		return
	}
	defer release()
	tmp, err := os.CreateTemp("", "asc-export-*")
	if err != nil {
		log.Error("asc export download: scratch file: %v", err)
		ctx.Error(http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	size, err := asctrust.CopyDataExport(callCtx, conn, id, tmp)
	if err != nil {
		f := trustweb.FailureOf("export download", err)
		ctx.Error(f.Status, f.Localized(ctx))
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		ctx.Error(http.StatusInternalServerError)
		return
	}
	ctx.SetServeHeaders(&context.ServeHeaderOptions{
		ContentType: "application/x-ndjson", ContentTypeCharset: "utf-8", Disposition: "attachment",
		Filename: "asc-export-" + strings.TrimPrefix(id, "ex_") + ".jsonl", ContentLength: &size,
	})
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.Resp.Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.WriteHeader(http.StatusOK)
	if ctx.Req.Method != http.MethodHead {
		if _, err := io.Copy(ctx.Resp, tmp); err != nil {
			log.Warn("asc export download: the browser went away: %v", err)
		}
	}
}

// AscExportDelete serves POST /user/settings/asc/export/{id}/delete with the field op.
func AscExportDelete(ctx *context.Context) {
	if !ascAccountGuard(ctx) || !trustweb.ParseForm(ctx, maxAscAccountForm) {
		return
	}
	to := setting.AppSubURL + "/user/settings/asc/export"
	conn, callCtx, release, problem, _ := ascAccountCall(ctx, "export")
	if problem != "" {
		ascAccountBack(ctx, to, "", problem)
		return
	}
	defer release()
	if err := asctrust.DeleteExport(callCtx, conn, ctx.Params(":id"), ctx.Req.PostFormValue("op")); err != nil {
		ascAccountBack(ctx, to, "", trustweb.FailureOf("export delete", err).Localized(ctx))
		return
	}
	ascAccountBack(ctx, to, ctx.Locale.TrString("asc_shell.export_deleted"), "")
}
