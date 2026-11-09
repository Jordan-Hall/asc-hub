// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/mailer"
	org_service "forgejo.org/services/org"
	repo_service "forgejo.org/services/repository"
)

// Collaboration render a repository's collaboration page
func Collaboration(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.collaboration")
	ctx.Data["PageIsSettingsCollaboration"] = true
	if !loadAccess(ctx) {
		return
	}
	ctx.HTML(http.StatusOK, tplCollaboration)
}

// loadAccess puts the page data of the access page in place: the people, the teams and the deploy keys of the
// repository. Both /settings/collaboration and /settings/keys render that page. It reports false after it has
// answered with an error.
func loadAccess(ctx *context.Context) bool {
	users, err := repo_model.GetCollaborators(ctx, ctx.Repo.Repository.ID, db.ListOptions{})
	if err != nil {
		ctx.ServerError("GetCollaborators", err)
		return false
	}
	ctx.Data["Collaborators"] = users
	ctx.Data["AgentScope"] = ascAgentScope(ctx, users)
	if !loadAscAccessRequests(ctx) {
		return false
	}

	teams, err := organization.GetRepoTeams(ctx, ctx.Repo.Repository)
	if err != nil {
		ctx.ServerError("GetRepoTeams", err)
		return false
	}
	ctx.Data["Teams"] = teams
	ctx.Data["Repo"] = ctx.Repo.Repository
	ctx.Data["OrgID"] = ctx.Repo.Repository.OwnerID
	ctx.Data["OrgName"] = ctx.Repo.Repository.OwnerName
	ctx.Data["Org"] = ctx.Repo.Repository.Owner
	ctx.Data["Units"] = unit_model.Units
	if !prepareAscAccessRows(ctx, users, teams) {
		return false
	}
	return loadDeployKeys(ctx)
}

// CollaborationPost response for actions for a collaboration of a repository
func CollaborationPost(ctx *context.Context) {
	name := strings.ToLower(ctx.FormString("collaborator"))
	if len(name) == 0 || ctx.Repo.Owner.LowerName == name {
		ctx.Redirect(setting.AppSubURL + ctx.Req.URL.EscapedPath())
		return
	}
	// the role picked before adding (board RepoSettingsAccess); a form without one adds with write access
	role := ctx.FormString("role")
	if role == "" {
		role = ascRoleWrite
	}
	if !slices.Contains(ascAccessRoles, role) {
		ctx.Flash.Error(ctx.Tr("asc_rp.access_role_unknown"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}
	// a name picked as a team from the search is a team; a typed name is a person or an agent first, then a
	// team of the owning organization
	if ctx.FormString("kind") == ascAccessTeam {
		ascAddTeam(ctx, name)
		return
	}

	u, err := user_model.GetUserByName(ctx, name)
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			ctx.ServerError("GetUserByName", err)
			return
		}
		if ctx.Repo.Owner.IsOrganization() {
			if _, terr := organization.OrgFromUser(ctx.Repo.Owner).GetTeam(ctx, name); terr == nil {
				ascAddTeam(ctx, name)
				return
			}
		}
		ctx.Flash.Error(ctx.Tr("form.user_not_exist"))
		ctx.Redirect(setting.AppSubURL + ctx.Req.URL.EscapedPath())
		return
	}

	if !u.IsActive {
		ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_inactive_user"))
		ctx.Redirect(setting.AppSubURL + ctx.Req.URL.EscapedPath())
		return
	}

	// Organization is not allowed to be added as a collaborator.
	if u.IsOrganization() {
		ctx.Flash.Error(ctx.Tr("repo.settings.org_not_allowed_to_be_collaborator"))
		ctx.Redirect(setting.AppSubURL + ctx.Req.URL.EscapedPath())
		return
	}

	if got, err := repo_model.IsCollaborator(ctx, ctx.Repo.Repository.ID, u.ID); err == nil && got {
		// say what access the person already has, so the invite does not look lost
		collab := &repo_model.Collaboration{}
		if has, merr := db.GetEngine(ctx).Where("repo_id = ? AND user_id = ?", ctx.Repo.Repository.ID, u.ID).Get(collab); merr == nil && has {
			ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_already", u.GetDisplayName(), ascRoleName(ctx, ascCollaboratorRole(collab))))
		} else {
			ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_duplicate"))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	// find the owner team of the organization the repo belongs too and
	// check if the user we're trying to add is an owner.
	if ctx.Repo.Repository.Owner.IsOrganization() {
		if isOwner, err := organization.IsOrganizationOwner(ctx, ctx.Repo.Repository.Owner.ID, u.ID); err != nil {
			ctx.ServerError("IsOrganizationOwner", err)
			return
		} else if isOwner {
			ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_owner"))
			ctx.Redirect(setting.AppSubURL + ctx.Req.URL.EscapedPath())
			return
		}
	}

	if err = repo_module.AddCollaborator(ctx, ctx.Repo.Repository, u); err != nil {
		if !errors.Is(err, user_model.ErrBlockedByUser) {
			ctx.ServerError("AddCollaborator", err)
			return
		}

		// To give an good error message, be precise on who has blocked who.
		if blockedOurs := user_model.IsBlocked(ctx, ctx.Repo.Repository.OwnerID, u.ID); blockedOurs {
			ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_blocked_our"))
		} else {
			ctx.Flash.Error(ctx.Tr("repo.settings.add_collaborator_blocked_them"))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	if err := ascApplyRole(ctx, u.ID, role); err != nil {
		ctx.ServerError("ascApplyRole", err)
		return
	}

	if setting.Service.EnableNotifyMail {
		mailer.SendCollaboratorMail(u, ctx.Doer, ctx.Repo.Repository)
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.add_collaborator_success"))
	ctx.Redirect(setting.AppSubURL + ctx.Req.URL.EscapedPath())
}

// ChangeCollaborationAccessMode response for changing access of a collaboration
func ChangeCollaborationAccessMode(ctx *context.Context) {
	if ctx.FormString("mode") == ascMaintainMode {
		if err := repo_module.SetAscMaintain(ctx, ctx.Repo.Repository, ctx.FormInt64("uid")); err != nil {
			log.Error("SetAscMaintain: %v", err)
		}
		return
	}
	if err := repo_module.ChangeCollaborationAccessMode(
		ctx,
		ctx.Repo.Repository,
		ctx.FormInt64("uid"),
		perm.AccessMode(ctx.FormInt("mode"))); err != nil {
		log.Error("ChangeCollaborationAccessMode: %v", err)
	}
}

// ascApplyRole gives a collaborator one of ascAccessRoles; a new collaborator has write access already.
func ascApplyRole(ctx *context.Context, uid int64, role string) error {
	switch role {
	case ascRoleMaintain:
		return repo_module.SetAscMaintain(ctx, ctx.Repo.Repository, uid)
	case ascRoleAdmin:
		return repo_module.ChangeCollaborationAccessMode(ctx, ctx.Repo.Repository, uid, perm.AccessModeAdmin)
	case ascRoleRead:
		return repo_module.ChangeCollaborationAccessMode(ctx, ctx.Repo.Repository, uid, perm.AccessModeRead)
	default:
		return nil
	}
}

// ascRoleName is the label of a role of the access page.
func ascRoleName(ctx *context.Context, role string) string {
	if role == ascRoleMaintain {
		return ctx.Locale.TrString("asc_rp.access_maintain")
	}
	if role == "" {
		return ctx.Locale.TrString("repo.settings.collaboration.undefined")
	}
	return ctx.Locale.TrString("repo.settings.collaboration." + role)
}

// DeleteCollaboration delete a collaboration for a repository
func DeleteCollaboration(ctx *context.Context) {
	if err := repo_module.DeleteCollaboration(ctx, ctx.Repo.Repository, ctx.FormInt64("id")); err != nil {
		ctx.Flash.Error("DeleteCollaboration: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.remove_collaborator_success"))
	}

	ctx.JSONRedirect(ctx.Repo.RepoLink + "/settings/collaboration")
}

// AddTeamPost response for adding a team to a repository
func AddTeamPost(ctx *context.Context) {
	if !ctx.Repo.Owner.RepoAdminChangeTeamAccess && !ctx.Repo.IsOwner() {
		ctx.Flash.Error(ctx.Tr("repo.settings.change_team_access_not_allowed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	name := strings.ToLower(ctx.FormString("team"))
	if len(name) == 0 {
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}
	ascAddTeam(ctx, name)
}

// ascAddTeam gives a team of the owning organization access to the repository, with the access mode of the team.
func ascAddTeam(ctx *context.Context, name string) {
	if !ctx.Repo.Owner.IsOrganization() {
		ctx.Flash.Error(ctx.Tr("form.team_not_exist"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}
	if !ctx.Repo.Owner.RepoAdminChangeTeamAccess && !ctx.Repo.IsOwner() {
		ctx.Flash.Error(ctx.Tr("repo.settings.change_team_access_not_allowed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	team, err := organization.OrgFromUser(ctx.Repo.Owner).GetTeam(ctx, name)
	if err != nil {
		if organization.IsErrTeamNotExist(err) {
			ctx.Flash.Error(ctx.Tr("form.team_not_exist"))
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		} else {
			ctx.ServerError("GetTeam", err)
		}
		return
	}

	if team.OrgID != ctx.Repo.Repository.OwnerID {
		ctx.Flash.Error(ctx.Tr("repo.settings.team_not_in_organization"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	if organization.HasTeamRepo(ctx, ctx.Repo.Repository.OwnerID, team.ID, ctx.Repo.Repository.ID) {
		ctx.Flash.Error(ctx.Tr("repo.settings.add_team_duplicate"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	if err = org_service.TeamAddRepository(ctx, team, ctx.Repo.Repository); err != nil {
		ctx.ServerError("TeamAddRepository", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.add_team_success"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
}

// DeleteTeam response for deleting a team from a repository
func DeleteTeam(ctx *context.Context) {
	if !ctx.Repo.Owner.RepoAdminChangeTeamAccess && !ctx.Repo.IsOwner() {
		ctx.Flash.Error(ctx.Tr("repo.settings.change_team_access_not_allowed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/collaboration")
		return
	}

	team, err := organization.GetTeamByID(ctx, ctx.FormInt64("id"))
	if err != nil {
		ctx.ServerError("GetTeamByID", err)
		return
	}

	if err = repo_service.RemoveRepositoryFromTeam(ctx, team, ctx.Repo.Repository.ID); err != nil {
		ctx.ServerError("team.RemoveRepositorys", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.remove_team_success"))
	ctx.JSONRedirect(ctx.Repo.RepoLink + "/settings/collaboration")
}
