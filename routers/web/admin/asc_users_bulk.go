// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	user_service "forgejo.org/services/user"
)

const tplUsersBulk base.TplName = "admin/user/bulk"

const (
	bulkRequire2FA = "require_2fa"
	bulkSuspend    = "suspend"
	// bulkMax is the most people one bulk action changes.
	bulkMax = 200
)

// UsersBulk serves POST /admin/users/bulk: the people ticked on the list (ids) and an action. Without confirm it shows the
// people the action would change and asks for a reason; with confirm=1 and a reason it changes them, each written to the
// audit log. The doer is never among them.
func UsersBulk(ctx *context.Context) {
	action := ctx.FormString("action")
	if action != bulkRequire2FA && action != bulkSuspend {
		ctx.Error(http.StatusBadRequest, "unknown action")
		return
	}
	people, err := bulkPeople(ctx)
	if err != nil {
		ctx.ServerError("bulkPeople", err)
		return
	}
	if len(people) == 0 {
		ctx.Flash.Error(ctx.Tr("asc_admin.bulk_none"))
		ctx.Redirect(setting.AppSubURL + "/admin/users")
		return
	}
	reason := strings.TrimSpace(ctx.FormString("reason"))
	if ctx.FormString("confirm") != "1" || reason == "" || utf8.RuneCountInString(reason) > adminReasonMax {
		ctx.Data["Title"] = ctx.Tr("admin.users")
		ctx.Data["PageIsAdminUsers"] = true
		ctx.Data["BulkAction"] = action
		ctx.Data["BulkPeople"] = people
		ctx.Data["BulkReasonMax"] = adminReasonMax
		if ctx.FormString("confirm") == "1" {
			ctx.Data["BulkReasonNeeded"] = true
		}
		ctx.HTML(http.StatusOK, tplUsersBulk)
		return
	}
	for _, u := range people {
		switch action {
		case bulkRequire2FA:
			u.RequireTwoFactor = true
			if err := user_model.UpdateUserCols(ctx, u, "require_two_factor"); err != nil {
				ctx.ServerError("UpdateUserCols", err)
				return
			}
			recordAdminAction(ctx, asctrust.AuditHostUser2FARequired, "Require 2FA", u, reason)
		case bulkSuspend:
			if err := user_service.UpdateAuth(ctx, u, &user_service.UpdateAuthOptions{ProhibitLogin: optional.Some(true)}); err != nil {
				ctx.ServerError("UpdateAuth", err)
				return
			}
			recordAdminAction(ctx, asctrust.AuditHostUserSuspended, "Suspend", u, reason)
		}
	}
	log.Info("admin %s: %s for %d people", ctx.Doer.Name, action, len(people))
	ctx.Flash.Success(ctx.TrN(len(people), "asc_admin.bulk_done_"+action+"_1", "asc_admin.bulk_done_"+action+"_n", len(people)))
	ctx.Redirect(setting.AppSubURL + "/admin/users")
}

// bulkPeople loads the people of the ids field: individuals only, the doer left out, at most bulkMax.
func bulkPeople(ctx *context.Context) ([]*user_model.User, error) {
	if err := ctx.Req.ParseForm(); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(ctx.Req.PostForm["ids"]))
	for _, raw := range ctx.Req.PostForm["ids"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || id == ctx.Doer.ID || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) == bulkMax {
			break
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var people []*user_model.User
	err := db.GetEngine(ctx).In("id", ids).And("type = ?", user_model.UserTypeIndividual).OrderBy("lower_name").Find(&people)
	return people, err
}
