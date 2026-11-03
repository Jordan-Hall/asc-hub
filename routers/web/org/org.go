// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2018 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package org

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"forgejo.org/models"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

const (
	// tplCreateOrg template path for create organization
	tplCreateOrg base.TplName = "org/create"
)

// Create render the page for create organization
func Create(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("new_org.title")
	if !ctx.Doer.CanCreateOrganization() {
		ctx.ServerError("Not allowed", errors.New(ctx.Locale.TrString("org.form.create_org_not_allowed")))
		return
	}

	ctx.Data["visibility"] = setting.Service.DefaultOrgVisibilityMode
	ctx.Data["repo_admin_change_team_access"] = true
	ctx.Data["create_everyone"] = true
	createData(ctx)

	ctx.HTML(http.StatusOK, tplCreateOrg)
}

// createData is what the form shows besides the fields: whether asc serve can take the agents switch.
func createData(ctx *context.Context) {
	ctx.Data["AscOrgAgentsOffered"] = setting.Asc.Enabled && asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings)
}

// everyoneTeamName is the first team the form offers to make.
const everyoneTeamName = "Everyone"

// createEveryoneTeam makes the team Everyone of a new organization: it reads every repository, and the creator is in it.
func createEveryoneTeam(ctx *context.Context, org *organization.Organization) error {
	t := &organization.Team{OrgID: org.ID, Name: everyoneTeamName, AccessMode: perm.AccessModeRead, IncludesAllRepositories: true}
	for _, tp := range unit.AllRepoUnitTypes {
		t.Units = append(t.Units, &organization.TeamUnit{OrgID: org.ID, Type: tp, AccessMode: perm.AccessModeRead})
	}
	if err := models.NewTeam(ctx, t); err != nil {
		return err
	}
	return models.AddTeamMember(ctx, t, ctx.Doer.ID)
}

// allowAgents turns agents on for the new organization as its creator. A failure is a sentence for the person.
func allowAgents(ctx *context.Context, org *organization.Organization) string {
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc new org agents: no connection to asc serve: %v", err)
		return ctx.Locale.TrString("asc_trust.err.unavailable")
	}
	callCtx, release, err := asctrust.UserCallOrg(ctx, ctx.Doer, org.AsUser())
	if err != nil {
		return trustweb.FailureOf("new org agents credential", err).Localized(ctx)
	}
	defer release()
	if err := asctrust.AllowOrgAgents(callCtx, conn, org.Name, asctrust.NewOpID()); err != nil {
		return trustweb.FailureOf("new org agents", err).Localized(ctx)
	}
	return ""
}

// orgNameCheck is the answer to a handle typed in the new organization form: whether it can be taken, and why not.
type orgNameCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// checkOrgName runs the checks CreateOrganization makes on a name, for an answer while it is typed. An empty name has
// no message.
func checkOrgName(ctx *context.Context, name string) (orgNameCheck, error) {
	if name == "" {
		return orgNameCheck{}, nil
	}
	if utf8.RuneCountInString(name) > 40 {
		return orgNameCheck{Message: ctx.Locale.TrString("asc_work.neworg_handle_long", 40)}, nil
	}
	if err := user_model.IsUsableUsername(name); err != nil {
		switch {
		case db.IsErrNameReserved(err):
			return orgNameCheck{Message: ctx.Locale.TrString("org.form.name_reserved", name)}, nil
		case db.IsErrNamePatternNotAllowed(err):
			return orgNameCheck{Message: ctx.Locale.TrString("org.form.name_pattern_not_allowed", err.(db.ErrNamePatternNotAllowed).Pattern)}, nil
		case setting.Service.AllowDotsInUsernames:
			return orgNameCheck{Message: ctx.Locale.TrString("asc_work.neworg_handle_chars")}, nil
		default:
			return orgNameCheck{Message: ctx.Locale.TrString("asc_work.neworg_handle_chars_no_dots")}, nil
		}
	}
	taken, err := user_model.IsUserExist(ctx, 0, name)
	if err != nil {
		return orgNameCheck{}, err
	}
	if taken {
		return orgNameCheck{Message: ctx.Locale.TrString("form.org_name_been_taken")}, nil
	}
	return orgNameCheck{OK: true, Message: ctx.Locale.TrString("asc_work.neworg_handle_free", name)}, nil
}

// CreateCheck answers GET /org/create/check?name=: the form asks it as the handle is typed.
func CreateCheck(ctx *context.Context) {
	if !ctx.Doer.CanCreateOrganization() {
		ctx.JSON(http.StatusForbidden, orgNameCheck{Message: ctx.Locale.TrString("org.form.create_org_not_allowed")})
		return
	}
	res, err := checkOrgName(ctx, strings.TrimSpace(ctx.FormString("name")))
	if err != nil {
		ctx.ServerError("IsUserExist", err)
		return
	}
	ctx.JSON(http.StatusOK, res)
}

// CreatePost response for create organization
func CreatePost(ctx *context.Context) {
	form := *web.GetForm(ctx).(*forms.CreateOrgForm)
	ctx.Data["Title"] = ctx.Tr("new_org.title")

	if !ctx.Doer.CanCreateOrganization() {
		ctx.ServerError("Not allowed", errors.New(ctx.Locale.TrString("org.form.create_org_not_allowed")))
		return
	}

	createData(ctx)
	if ctx.HasError() {
		ctx.HTML(http.StatusOK, tplCreateOrg)
		return
	}

	org := &organization.Organization{
		Name:                      form.OrgName,
		FullName:                  strings.TrimSpace(form.FullName),
		IsActive:                  true,
		Type:                      user_model.UserTypeOrganization,
		Visibility:                form.Visibility,
		RepoAdminChangeTeamAccess: form.RepoAdminChangeTeamAccess,
	}

	if err := organization.CreateOrganization(ctx, org, ctx.Doer); err != nil {
		ctx.Data["Err_OrgName"] = true
		switch {
		case user_model.IsErrUserAlreadyExist(err):
			ctx.RenderWithErr(ctx.Tr("form.org_name_been_taken"), tplCreateOrg, &form)
		case db.IsErrNameReserved(err):
			ctx.RenderWithErr(ctx.Tr("org.form.name_reserved", err.(db.ErrNameReserved).Name), tplCreateOrg, &form)
		case db.IsErrNamePatternNotAllowed(err):
			ctx.RenderWithErr(ctx.Tr("org.form.name_pattern_not_allowed", err.(db.ErrNamePatternNotAllowed).Pattern), tplCreateOrg, &form)
		case organization.IsErrUserNotAllowedCreateOrg(err):
			ctx.RenderWithErr(ctx.Tr("org.form.create_org_not_allowed"), tplCreateOrg, &form)
		default:
			ctx.ServerError("CreateOrganization", err)
		}
		return
	}
	log.Trace("Organization created: %s", org.Name)

	ctx.Flash.Success(ctx.Tr("asc_work.neworg_created", org.Name))
	// the organization exists from here: what follows can fail without undoing it, and says so
	var later []string
	if form.CreateEveryone {
		if err := createEveryoneTeam(ctx, org); err != nil {
			log.Error("new org %s: team Everyone: %v", org.Name, err)
			later = append(later, ctx.Locale.TrString("asc_work.neworg_everyone_failed"))
		}
	}
	if form.AllowAgents && setting.Asc.Enabled {
		if problem := allowAgents(ctx, org); problem != "" {
			later = append(later, ctx.Locale.TrString("asc_work.neworg_agents_failed", problem))
		}
	}
	if len(later) > 0 {
		ctx.Flash.Warning(strings.Join(later, " "))
	}
	ctx.Redirect(org.AsUser().DashboardLink())
}
