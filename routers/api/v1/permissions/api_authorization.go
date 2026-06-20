// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"net/http"

	"forgejo.org/services/authz"
)

func APIAuthorization(ctx Context) {
	if hasScope, scope := ctx.GetAuthentication().Scope().Get(); hasScope {
		publicOnly, err := scope.PublicOnly()
		if err != nil {
			ctx.Error(http.StatusForbidden, "tokenRequiresScope", "parsing public resource scope failed: "+err.Error())
			return
		}
		ctx.SetPublicOnly(publicOnly)
	}

	reducer := ctx.GetAuthentication().Reducer()
	if reducer != nil {
		ctx.SetReducer(reducer)
	} else {
		// No Reducer will be populated if the auth method wasn't an PAT.  In this case, we populate `ctx.Reducer` so no
		// nil checks are needed, and we respect the scope `PublicOnly()` so that it it's safe to just rely on
		// `ctx.Reducer` to account for public-only access:
		if ctx.GetPublicOnly() {
			ctx.SetReducer(&authz.PublicReposAuthorizationReducer{})
		} else {
			ctx.SetReducer(&authz.AllAccessAuthorizationReducer{})
		}
	}
}
