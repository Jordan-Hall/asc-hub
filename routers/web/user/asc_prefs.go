// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
	user_service "forgejo.org/services/user"
)

// ascPrefThemes are the colour modes of the theme menu (board Shell) and the account theme each one saves.
var ascPrefThemes = map[string]string{"system": "asc-auto", "light": "asc-light", "dark": "asc-dark"}

// AscPrefsPost answers `POST /-/asc/prefs`: the colour mode (theme=system, light or dark) or the density
// (density=comfy or compact) the person just chose in the header menu or on Settings, Appearance, saved with the
// account so every browser opens the same way. 204, or 400 for a value it does not know.
func AscPrefsPost(ctx *context.Context) {
	_ = ctx.Req.ParseForm()
	mode, density := ctx.Req.PostForm.Get("theme"), ctx.Req.PostForm.Get("density")
	theme, ok := ascPrefThemes[mode]
	if (mode == "" && density == "") || (mode != "" && (!ok || !user_model.IsThemeValid(theme))) ||
		(density != "" && density != user_model.AscDensityComfy && density != user_model.AscDensityCompact) {
		ctx.Error(http.StatusBadRequest)
		return
	}
	if mode != "" && ctx.Doer.Theme != theme {
		if err := user_service.UpdateUser(ctx, ctx.Doer, &user_service.UpdateOptions{Theme: optional.Some(theme)}); err != nil {
			ctx.ServerError("UpdateUser", err)
			return
		}
	}
	if density != "" {
		if err := user_model.SetAscDensity(ctx, ctx.Doer.ID, density); err != nil {
			ctx.ServerError("SetAscDensity", err)
			return
		}
	}
	ctx.Status(http.StatusNoContent)
}
