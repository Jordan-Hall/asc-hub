// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"

	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/context"
)

// AscRequestAccess records the request of the signed-in person for more access to the repository, from the 403
// page, and goes back to that page (redirect_to), which then says the request was sent. The admins of the repository
// see it on the Collaborators page. A person the owner blocked is told the same, and nothing is recorded.
func AscRequestAccess(ctx *context.Context) {
	repo := ctx.Repo.Repository
	mode := repo_model.AscAccessRequestMode(ctx.Repo.Permission.AccessMode)
	if mode == perm.AccessModeNone {
		ctx.Flash.Info(ctx.Tr("asc_err.request_access_admin"))
		ctx.RedirectToFirst(ctx.FormString("redirect_to"), repo.Link())
		return
	}
	if ctx.Doer.IsRestricted {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !user_model.IsBlocked(ctx, repo.OwnerID, ctx.Doer.ID) {
		if err := repo_model.AscRequestAccess(ctx, repo.ID, ctx.Doer.ID, mode, ctx.FormTrim("message")); err != nil {
			ctx.ServerError("AscRequestAccess", err)
			return
		}
	}
	ctx.Flash.Success(ctx.Tr("asc_err.request_access_sent", repo.FullName()))
	ctx.RedirectToFirst(ctx.FormString("redirect_to"), repo.Link())
}

// AscWithdrawAccessRequest removes the request of the signed-in person for the repository.
func AscWithdrawAccessRequest(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if err := repo_model.DeleteAscAccessRequest(ctx, repo.ID, ctx.Doer.ID); err != nil {
		ctx.ServerError("DeleteAscAccessRequest", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_err.request_access_withdrawn"))
	ctx.RedirectToFirst(ctx.FormString("redirect_to"), repo.Link())
}
