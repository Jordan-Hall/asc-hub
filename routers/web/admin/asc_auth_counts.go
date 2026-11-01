// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// authSourceCounts is how many people (individual accounts) sign in through each source; source 0 is the local password.
func authSourceCounts(ctx *context.Context) map[int64]int64 {
	var rows []struct {
		LoginSource int64
		N           int64
	}
	err := db.GetEngine(ctx).Table("user").
		Select("login_source, COUNT(*) AS n").
		Where("type = ?", user_model.UserTypeIndividual).
		GroupBy("login_source").
		Find(&rows)
	if err != nil {
		log.Warn("admin authentication: people per source: %v", err)
		return nil
	}
	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.LoginSource] = r.N
	}
	return out
}

// LocalPasswordsPost serves `POST /admin/auths/local` with the field admins_only: the Local passwords row is kept for
// the site admins, and everyone else signs in through another source.
func LocalPasswordsPost(ctx *context.Context) {
	if err := auth_model.SetLocalPasswordsAdminsOnly(ctx, ctx.FormBool("admins_only")); err != nil {
		ctx.ServerError("SetLocalPasswordsAdminsOnly", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_admin.auth_local_saved"))
	ctx.Redirect(setting.AppSubURL + "/admin/auths")
}
