// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"errors"

	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/mailer"
)

// AscAccessRequestRow is a request for access as the Collaborators page lists it.
type AscAccessRequestRow struct {
	*repo_model.AscAccessRequest
	User *user_model.User
	// Role is the role a grant gives: write or admin.
	Role string
}

// loadAscAccessRequests puts the requests for access to the repository into the page data. A request of a deleted
// account is left out (it goes with the account).
func loadAscAccessRequests(ctx *context.Context) bool {
	reqs, err := repo_model.ListAscAccessRequests(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		ctx.ServerError("ListAscAccessRequests", err)
		return false
	}
	ids := make([]int64, 0, len(reqs))
	for _, r := range reqs {
		ids = append(ids, r.UserID)
	}
	users, err := user_model.GetUsersByIDs(ctx, ids)
	if err != nil {
		ctx.ServerError("GetUsersByIDs", err)
		return false
	}
	byID := make(map[int64]*user_model.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	rows := make([]AscAccessRequestRow, 0, len(reqs))
	for _, r := range reqs {
		if u := byID[r.UserID]; u != nil {
			rows = append(rows, AscAccessRequestRow{AscAccessRequest: r, User: u, Role: ascRequestRole(r.Mode)})
		}
	}
	ctx.Data["AscAccessRequests"] = rows
	return true
}

func ascRequestRole(mode perm.AccessMode) string {
	if mode >= perm.AccessModeAdmin {
		return ascRoleAdmin
	}
	return ascRoleWrite
}

// AscGrantAccessRequest gives the person of a request (form id) the access asked for, as a collaborator, and removes
// the request. A person who cannot be a collaborator (no longer active, an owner of the organization, blocked) is
// told so and the request stays, so the admin can dismiss it.
func AscGrantAccessRequest(ctx *context.Context) {
	repo := ctx.Repo.Repository
	back := ctx.Repo.RepoLink + "/settings/collaboration"
	r, err := repo_model.GetAscAccessRequestByID(ctx, repo.ID, ctx.FormInt64("id"))
	if err != nil {
		ctx.ServerError("GetAscAccessRequestByID", err)
		return
	}
	if r == nil {
		ctx.Flash.Error(ctx.Tr("asc_rp.access_request_gone"))
		ctx.Redirect(back)
		return
	}
	u, err := user_model.GetUserByID(ctx, r.UserID)
	if err != nil {
		ctx.ServerError("GetUserByID", err)
		return
	}
	if !u.IsActive || u.IsOrganization() {
		ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_inactive_user"))
		ctx.Redirect(back)
		return
	}
	if repo.Owner.IsOrganization() {
		isOwner, err := organization.IsOrganizationOwner(ctx, repo.OwnerID, u.ID)
		if err != nil {
			ctx.ServerError("IsOrganizationOwner", err)
			return
		}
		if isOwner {
			ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_owner"))
			ctx.Redirect(back)
			return
		}
	}
	collab, err := repo_model.IsCollaborator(ctx, repo.ID, u.ID)
	if err != nil {
		ctx.ServerError("IsCollaborator", err)
		return
	}
	if !collab {
		if err := repo_module.AddCollaborator(ctx, repo, u); err != nil {
			if errors.Is(err, user_model.ErrBlockedByUser) {
				ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_blocked_them"))
				ctx.Redirect(back)
				return
			}
			ctx.ServerError("AddCollaborator", err)
			return
		}
	}
	role := ascRequestRole(r.Mode)
	if collab && role == ascRoleWrite {
		err = repo_module.ChangeCollaborationAccessMode(ctx, repo, u.ID, perm.AccessModeWrite)
	} else {
		err = ascApplyRole(ctx, u.ID, role)
	}
	if err != nil {
		ctx.ServerError("ascApplyRole", err)
		return
	}
	if err := repo_model.DeleteAscAccessRequest(ctx, repo.ID, u.ID); err != nil {
		ctx.ServerError("DeleteAscAccessRequest", err)
		return
	}
	if setting.Service.EnableNotifyMail && !collab {
		mailer.SendCollaboratorMail(u, ctx.Doer, repo)
	}
	ctx.Flash.Success(ctx.Tr("asc_rp.access_request_granted", u.Name, ascRoleName(ctx, role)))
	ctx.Redirect(back)
}

// AscDismissAccessRequest removes a request (form id) without giving access.
func AscDismissAccessRequest(ctx *context.Context) {
	repo := ctx.Repo.Repository
	back := ctx.Repo.RepoLink + "/settings/collaboration"
	r, err := repo_model.GetAscAccessRequestByID(ctx, repo.ID, ctx.FormInt64("id"))
	if err != nil {
		ctx.ServerError("GetAscAccessRequestByID", err)
		return
	}
	if r != nil {
		if err := repo_model.DeleteAscAccessRequest(ctx, repo.ID, r.UserID); err != nil {
			ctx.ServerError("DeleteAscAccessRequest", err)
			return
		}
	}
	ctx.Flash.Success(ctx.Tr("asc_rp.access_request_dismissed"))
	ctx.Redirect(back)
}
