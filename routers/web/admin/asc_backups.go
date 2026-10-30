// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The backups of asc serve (docs/BUILDOUT.md section 26), served on the Backups page of the administration. Site
// administrators only; the calls are made as the person with a claim for the whole server (asctrust.CallInstance).
//
//	GET  /admin/asc/backups                 the hub's own backup instructions (overview.go) and, above them, the board of asc serve
//	POST /admin/asc/backups/run             back up now (field target, empty is the target of the schedule)
//	POST /admin/asc/backups/schedule        set the schedule
//	POST /admin/asc/backups/{id}/verify     check a backup
//	GET  /admin/asc/backups/{id}/restore    what a restore would do, and the form that asks for the word RESTORE
//	POST /admin/asc/backups/{id}/restore    stage the restore (field confirm)
const tplAscBackupRestore base.TplName = "admin/asc_backup_restore"

type ascBackupsBoard struct {
	Base        string
	Page        *asctrust.BackupsPage
	Problem     string
	Unsupported bool
	RunOp       string
	ScheduleOp  string
	VerifyOps   map[string]string
}

func ascBackupsGuard(ctx *context.Context) bool {
	if !setting.Asc.Enabled {
		ctx.NotFound("asc backups", nil)
		return false
	}
	if !ctx.IsSigned || ctx.Doer == nil || !ctx.Doer.IsAdmin {
		ctx.Error(http.StatusForbidden)
		return false
	}
	return true
}

func ascBackupsCall(ctx *context.Context, what string) (grpc.ClientConnInterface, gocontext.Context, func(), string, int) {
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc %s: no connection to asc serve: %v", what, err)
		return nil, nil, nil, ctx.Locale.TrString("asc_trust.err.unavailable"), http.StatusBadGateway
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		f := trustweb.FailureOf(what+" credential", err)
		return nil, nil, nil, f.Localized(ctx), f.Status
	}
	return conn, callCtx, release, "", 0
}

// ascBackupsLoad fills the board of asc serve for the Backups page. It never fails the page: a problem is shown inside
// the board, because the instructions below it do not need asc.
func ascBackupsLoad(ctx *context.Context) {
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Doer == nil || !ctx.Doer.IsAdmin {
		return
	}
	b := &ascBackupsBoard{Base: setting.AppSubURL + "/admin/asc/backups", RunOp: asctrust.NewOpID(), ScheduleOp: asctrust.NewOpID(), VerifyOps: map[string]string{}}
	ctx.Data["AscBackups"] = b
	if !asctrust.HasFeature(ctx, asctrust.FeatureBackups) {
		b.Unsupported = true
		return
	}
	conn, callCtx, release, problem, _ := ascBackupsCall(ctx, "backups")
	if problem != "" {
		b.Problem = problem
		return
	}
	defer release()
	page, err := asctrust.LoadBackups(callCtx, conn)
	if err != nil {
		b.Problem = trustweb.FailureOf("backups load", err).Localized(ctx)
		return
	}
	b.Page = page
	for _, r := range page.Backups {
		b.VerifyOps[r.ID] = asctrust.NewOpID()
	}
}

func ascBackupsBack(ctx *context.Context, to, success, problem string) {
	if problem != "" {
		ctx.Flash.Error(problem)
	} else if success != "" {
		ctx.Flash.Success(success)
	}
	ctx.Redirect(to)
}

func ascBackupsPost(ctx *context.Context, to string, run func(gocontext.Context, grpc.ClientConnInterface) (string, error)) {
	if !ascBackupsGuard(ctx) || !trustweb.ParseForm(ctx, 8<<10) {
		return
	}
	conn, callCtx, release, problem, _ := ascBackupsCall(ctx, "backups")
	if problem != "" {
		ascBackupsBack(ctx, to, "", problem)
		return
	}
	defer release()
	done, err := run(callCtx, conn)
	if err != nil {
		ascBackupsBack(ctx, to, "", trustweb.FailureOf("backups change", err).Localized(ctx))
		return
	}
	ascBackupsBack(ctx, to, done, "")
}

func ascBackupsURL() string { return setting.AppSubURL + "/admin/asc/backups" }

// AscBackupsRun serves POST /admin/asc/backups/run.
func AscBackupsRun(ctx *context.Context) {
	ascBackupsPost(ctx, ascBackupsURL(), func(callCtx gocontext.Context, conn grpc.ClientConnInterface) (string, error) {
		_, err := asctrust.RunBackup(callCtx, conn, ctx.Req.PostFormValue("target"), ctx.Req.PostFormValue("op"))
		return ctx.Locale.TrString("asc_shell.bk_started"), err
	})
}

// AscBackupsSchedule serves POST /admin/asc/backups/schedule.
func AscBackupsSchedule(ctx *context.Context) {
	ascBackupsPost(ctx, ascBackupsURL(), func(callCtx gocontext.Context, conn grpc.ClientConnInterface) (string, error) {
		s, err := asctrust.ScheduleFromForm(ctx.Req.PostFormValue)
		if err != nil {
			return "", err
		}
		_, err = asctrust.SetSchedule(callCtx, conn, s, ctx.Req.PostFormValue("op"))
		return ctx.Locale.TrString("asc_shell.bk_schedule_saved"), err
	})
}

// AscBackupsVerify serves POST /admin/asc/backups/{id}/verify.
func AscBackupsVerify(ctx *context.Context) {
	ascBackupsPost(ctx, ascBackupsURL(), func(callCtx gocontext.Context, conn grpc.ClientConnInterface) (string, error) {
		_, err := asctrust.VerifyBackup(callCtx, conn, ctx.Params(":id"), ctx.Req.PostFormValue("op"))
		return ctx.Locale.TrString("asc_shell.bk_verify_started"), err
	})
}

type ascBackupRestorePage struct {
	Base    string
	ID      string
	Preview *asctrust.RestorePreview
	Problem string
	Op      string
	Word    string
}

// AscBackupsRestoreForm serves GET /admin/asc/backups/{id}/restore.
func AscBackupsRestoreForm(ctx *context.Context) {
	if !ascBackupsGuard(ctx) {
		return
	}
	trustweb.NoStore(ctx)
	p := &ascBackupRestorePage{Base: ascBackupsURL(), ID: ctx.Params(":id"), Op: asctrust.NewOpID(), Word: asctrust.RestoreWord}
	ctx.Data["Title"] = ctx.Tr("asc_shell.bk_restore_title")
	ctx.Data["PageIsAdminBackups"] = true
	ctx.Data["AscBackupRestore"] = p
	status := http.StatusOK
	defer func() { ctx.HTML(status, tplAscBackupRestore) }()

	if _, err := asctrust.CheckBackupID(p.ID); err != nil {
		ctx.NotFound("asc backup", nil)
		status = http.StatusNotFound
		return
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureBackups) {
		p.Problem = ctx.Locale.TrString("asc_shell.bk_unsupported")
		return
	}
	conn, callCtx, release, problem, st := ascBackupsCall(ctx, "backup restore")
	if problem != "" {
		p.Problem, status = problem, st
		return
	}
	defer release()
	pre, err := asctrust.PreviewRestore(callCtx, conn, p.ID)
	if err != nil {
		f := trustweb.FailureOf("backup preview", err)
		p.Problem, status = f.Localized(ctx), f.Status
		return
	}
	p.Preview = pre
}

// AscBackupsRestore serves POST /admin/asc/backups/{id}/restore with the fields confirm and op.
func AscBackupsRestore(ctx *context.Context) {
	if !ascBackupsGuard(ctx) || !trustweb.ParseForm(ctx, 8<<10) {
		return
	}
	id := ctx.Params(":id")
	form := ascBackupsURL() + "/" + id + "/restore"
	if _, err := asctrust.CheckBackupID(id); err != nil {
		ctx.NotFound("asc backup", nil)
		return
	}
	conn, callCtx, release, problem, _ := ascBackupsCall(ctx, "backup restore")
	if problem != "" {
		ascBackupsBack(ctx, form, "", problem)
		return
	}
	defer release()
	res, err := asctrust.Restore(callCtx, conn, id, ctx.Req.PostFormValue("confirm"), ctx.Req.PostFormValue("op"))
	if err != nil {
		ascBackupsBack(ctx, form, "", trustweb.FailureOf("backup restore", err).Localized(ctx))
		return
	}
	// The restore is staged and applied when the server next starts: the board says so in a banner.
	ascBackupsBack(ctx, ascBackupsURL(), ctx.Locale.TrString("asc_shell.bk_restore_staged", res.BackupID), "")
}
