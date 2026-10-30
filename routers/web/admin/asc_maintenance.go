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
	"forgejo.org/services/ascmaintenance"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

const tplMaintenance base.TplName = "admin/asc_maintenance"

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
	ctx.HTML(http.StatusOK, tplMaintenance)
}

// MaintenancePost saves the page. The time is read as UTC.
func MaintenancePost(ctx *context.Context) {
	_ = ctx.Req.ParseForm()
	st := ascmaintenance.State{
		On:        slices.Contains(ctx.Req.Form["on"], "1"),
		Message:   ctx.FormTrim("message"),
		Window:    ctx.FormTrim("window"),
		StatusURL: ctx.FormTrim("status_url"),
	}
	if v := ctx.FormTrim("until"); v != "" {
		t, err := time.Parse("2006-01-02T15:04", v)
		if err != nil {
			ctx.Flash.Error(ctx.Tr("asc_admin.maintenance_bad_time"))
			ctx.Redirect(setting.AppSubURL + "/admin/maintenance")
			return
		}
		st.Until = t.UTC().Format(time.RFC3339)
	}
	before := ascmaintenance.Load(ctx)
	if err := ascmaintenance.Save(ctx, st); err != nil {
		ctx.Flash.Error(err.Error())
	} else {
		log.Info("maintenance mode set to %v by %s", st.On, ctx.Doer.Name)
		asctrust.RecordHostAudit(ctx, asctrust.HostAudit{
			Action: asctrust.AuditHostMaintenanceUpdated, Actor: ctx.Doer.Name, Target: "maintenance", Request: ctx.Req,
			Details: map[string]string{
				"enabled": strconv.FormatBool(st.On), "old_enabled": strconv.FormatBool(before.On), "message": st.Message, "until": st.Until,
			},
		})
		ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
	}
	ctx.Redirect(setting.AppSubURL + "/admin/maintenance")
}
