// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"net/http"

	actions_model "forgejo.org/models/actions"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	shared_user "forgejo.org/routers/web/shared/user"
	"forgejo.org/services/context"
)

// The settings page of the `packages: write` grant of the job token. Its routes are admin-only (the repository
// settings need repository admin, the organization settings need an owner) and its POST is protected by the CSRF
// middleware of the web router.

const (
	tplRepoPermissions = "repo/settings/actions"
	tplOrgPermissions  = "org/settings/actions"
)

func setPermissionsPage(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("actions.permissions.title")
	ctx.Data["PageType"] = "permissions"
	ctx.Data["PageIsSharedSettingsPermissions"] = true
}

// RepoPermissions shows the repository's setting and what its owner allows.
func RepoPermissions(ctx *context.Context) {
	setPermissionsPage(ctx)
	repo := ctx.Repo.Repository
	ctx.Data["PackagesWriteOn"] = actions_model.RepoAllowsPackagesWrite(ctx, repo)
	ctx.Data["PackagesWriteOrgOff"] = !actions_model.OrgAllowsPackagesWrite(ctx, repo.OwnerID)
	ctx.HTML(http.StatusOK, tplRepoPermissions)
}

// RepoPermissionsPost turns the grant for the repository on or off. A repository can narrow what its organization
// allows, never widen it, so turning it on is refused while the organization says no.
func RepoPermissionsPost(ctx *context.Context) {
	repo := ctx.Repo.Repository
	allow := ctx.FormBool("allow_packages_write")
	link := ctx.Repo.RepoLink + "/settings/actions/permissions"
	if allow && !actions_model.OrgAllowsPackagesWrite(ctx, repo.OwnerID) {
		ctx.Flash.Error(ctx.Tr("actions.permissions.packages_write.org_off"))
		ctx.Redirect(link)
		return
	}
	cfgUnit, err := repo.GetUnit(ctx, unit.TypeActions)
	if err != nil {
		ctx.ServerError("GetUnit", err)
		return
	}
	cfgUnit.ActionsConfig().AllowPackagesWrite = allow
	if err := repo_model.UpdateRepoUnit(ctx, cfgUnit); err != nil {
		ctx.ServerError("UpdateRepoUnit", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("actions.permissions.packages_write.saved"))
	ctx.Redirect(link)
}

// OrgPermissions shows what the organization allows its repositories.
func OrgPermissions(ctx *context.Context) {
	setPermissionsPage(ctx)
	if err := shared_user.LoadHeaderCount(ctx); err != nil {
		ctx.ServerError("LoadHeaderCount", err)
		return
	}
	ctx.Data["PackagesWriteForOrg"] = true
	ctx.Data["PackagesWriteOn"] = actions_model.OrgAllowsPackagesWrite(ctx, ctx.Org.Organization.ID)
	ctx.HTML(http.StatusOK, tplOrgPermissions)
}

// OrgPermissionsPost sets what the repositories of the organization can have. Turning it off ends the grant of the
// jobs that are running, because the grant is read again on every package request.
func OrgPermissionsPost(ctx *context.Context) {
	err := actions_model.SetOrgAllowsPackagesWrite(ctx, ctx.Org.Organization.AsUser(), ctx.FormBool("allow_packages_write"))
	if err != nil {
		ctx.ServerError("SetOrgAllowsPackagesWrite", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("actions.permissions.packages_write.saved"))
	ctx.Redirect(ctx.Org.OrgLink + "/settings/actions/permissions")
}
