// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/ascmaintenance"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

const tplMaintenance base.TplName = "admin/asc_maintenance"

// ascMaintenanceBoard is the asc serve part of the page: whether asc serve follows the hub's switch, and its state.
type ascMaintenanceBoard struct {
	State       *asctrust.AscMaintenance
	Problem     string
	Unsupported bool
	Op          string
}

// ascMaintenanceLinked reports whether the hub's switch also switches asc serve.
func ascMaintenanceLinked(ctx *context.Context) bool {
	return setting.Asc.Enabled && asctrust.HasFeature(ctx, asctrust.FeatureMaintenance)
}

// Maintenance shows the page where an administrator turns maintenance mode on and off.
func Maintenance(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_admin.maintenance")
	ctx.Data["PageIsAdminMaintenance"] = true
	st := ascmaintenance.Load(ctx)
	ctx.Data["Maintenance"] = st
	if t, ok := st.UntilTime(); ok {
		ctx.Data["MaintenanceUntilInput"] = t.UTC().Format("2006-01-02T15:04")
	}
	ctx.Data["MaintenanceLink"] = setting.AppSubURL + "/admin/maintenance"
	if setting.Asc.Enabled {
		b := &ascMaintenanceBoard{}
		ctx.Data["AscMaintenanceBoard"] = b
		if !asctrust.HasFeature(ctx, asctrust.FeatureMaintenance) {
			b.Unsupported = true
		} else if conn, callCtx, release, problem, _ := ascBackupsCall(ctx, "maintenance"); problem != "" {
			b.Problem = problem
		} else {
			defer release()
			if s, err := asctrust.GetMaintenance(callCtx, conn); err != nil {
				b.Problem = trustweb.FailureOf("maintenance load", err).Localized(ctx)
			} else {
				b.State, b.Op = s, asctrust.NewOpID()
			}
		}
	}
	ctx.HTML(http.StatusOK, tplMaintenance)
}

// MaintenancePost saves the page. The time is read as UTC. When asc serve has the Maintenance service it is switched
// first: if it refuses, nothing changes, so the hub and asc serve never disagree.
func MaintenancePost(ctx *context.Context) {
	back := setting.AppSubURL + "/admin/maintenance"
	if !trustweb.ParseForm(ctx, 8<<10) {
		return
	}
	st := ascmaintenance.State{
		On:        slices.Contains(ctx.Req.Form["on"], "1"),
		Message:   ctx.FormTrim("message"),
		Window:    ctx.FormTrim("window"),
		StatusURL: ctx.FormTrim("status_url"),
		Version:   ctx.FormTrim("version"),
	}
	var until time.Time
	if v := ctx.FormTrim("until"); v != "" {
		t, err := time.Parse("2006-01-02T15:04", v)
		if err != nil {
			ctx.Flash.Error(ctx.Tr("asc_admin.maintenance_bad_time"))
			ctx.Redirect(back)
			return
		}
		until = t.UTC()
		st.Until = until.Format(time.RFC3339)
	}
	if err := st.Validate(); err != nil {
		ctx.Flash.Error(err.Error())
		ctx.Redirect(back)
		return
	}
	if st.On && !until.IsZero() && until.Before(time.Now()) {
		ctx.Flash.Error(ctx.Tr("asc_admin.maintenance_past"))
		ctx.Redirect(back)
		return
	}
	if ascMaintenanceLinked(ctx) {
		conn, callCtx, release, problem, _ := ascBackupsCall(ctx, "maintenance")
		if problem != "" {
			ctx.Flash.Error(problem)
			ctx.Redirect(back)
			return
		}
		_, err := asctrust.SetMaintenance(callCtx, conn, st.On, st.Message, until, st.Version, ctx.Req.PostFormValue("op"))
		release()
		if err != nil {
			ctx.Flash.Error(trustweb.FailureOf("maintenance change", err).Localized(ctx))
			ctx.Redirect(back)
			return
		}
	}
	before := ascmaintenance.Load(ctx)
	if err := ascmaintenance.Save(ctx, st); err != nil {
		ctx.Flash.Error(err.Error())
		ctx.Redirect(back)
		return
	}
	log.Info("maintenance mode set to %v by %s", st.On, ctx.Doer.Name)
	asctrust.RecordHostAudit(ctx, asctrust.HostAudit{
		Action: asctrust.AuditHostMaintenanceUpdated, Actor: ctx.Doer.Name, Target: "maintenance", Request: ctx.Req,
		Details: map[string]string{
			"enabled": strconv.FormatBool(st.On), "old_enabled": strconv.FormatBool(before.On), "message": st.Message, "until": st.Until,
			"version": st.Version,
		},
	})
	ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
	ctx.Redirect(back)
}
