// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"errors"
	"net/http"
	"slices"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/base"
	"forgejo.org/modules/json"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

const (
	tplAuthorizedIntegrationList        base.TplName = "user/settings/authorized_integrations"
	tplAuthorizedIntegrationViewGeneric base.TplName = "user/settings/authorized_integrations/generic/view"
)

func ListAuthorizedIntegrations(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.authorized_integrations")
	ctx.Data["PageIsSettingsAuthorizedIntegrations"] = true

	ais, err := db.Find[auth_model.AuthorizedIntegration](ctx,
		auth_model.ListAuthorizedIntegrationOptions{UserID: optional.Some(ctx.Doer.ID)})
	if err != nil {
		ctx.ServerError("ListAuthorizedIntegrations", err)
		return
	}
	ctx.Data["AuthorizedIntegrations"] = ais

	ctx.HTML(http.StatusOK, tplAuthorizedIntegrationList)
}

type AuthorizedIntegrationForm struct {
	Name         string
	Description  string
	Audience     string
	Resource     string // all, public-only, repo-specific
	ScopeAll     bool
	Scope        []string
	SelectedRepo []string // slice of ownername/reponame

	// Future: ClaimRules is only required when aiUI == "generic"
	ClaimRules string
	// Future: Issuer is likely to be replaced with more-specific fields on non-generic UIs
	Issuer string
}

func ViewAuthorizedIntegration(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.authorized_integrations")
	ctx.Data["PageIsSettingsAuthorizedIntegrations"] = true

	aiUIString := ctx.Params("ui")
	aiUI, err := auth_model.ParseAuthorizedIntegrationUI(aiUIString)
	if err != nil {
		ctx.NotFound("ParseAuthorizedIntegrationUI", err)
		return
	}

	aiID := ctx.ParamsInt64("id")
	ai, err := auth_model.GetAuthorizedIntegrationByUI(ctx, ctx.Doer.ID, aiUI, aiID)
	if errors.Is(err, util.ErrNotExist) {
		ctx.NotFound("GetAuthorizedIntegrationByUI", err)
		return
	} else if err != nil {
		ctx.ServerError("GetAuthorizedIntegrationByUI", err)
		return
	}

	form := &AuthorizedIntegrationForm{
		Name:        ai.Name,
		Description: ai.Description,
		Audience:    ai.Audience,
		Issuer:      ai.Issuer,
	}

	if ai.ResourceAllRepos {
		publicOnly, err := ai.Scope.PublicOnly()
		if err != nil {
			ctx.ServerError("PublicOnly", err)
			return
		}
		if publicOnly {
			form.Resource = "public-only"
		} else {
			form.Resource = "all"
		}
	} else {
		form.Resource = "repo-specific"
	}

	form.Scope = ai.Scope.StringSlice()
	form.ScopeAll, err = ai.Scope.HasScope(auth_model.AccessTokenScopeAll)
	if err != nil {
		ctx.ServerError("HasScope", err)
		return
	}

	// Future: ClaimRules is only required when aiUI == "generic"
	claimRulesJSON, err := json.MarshalIndent(ai.ClaimRules, "", "  ")
	if err != nil {
		ctx.ServerError("MarshalIndent", err)
		return
	}
	form.ClaimRules = string(claimRulesJSON)

	// FIXME: form.SelectedRepo

	ctx.Data["Form"] = form

	categories := []string{
		"activitypub",
		"issue",
		"misc",
		"notification",
		"organization",
		"package",
		"repository",
		"user",
	}
	if ctx.Doer.IsAdmin {
		categories = append(categories, "admin")
	}
	slices.Sort(categories)
	ctx.Data["Categories"] = categories

	ctx.HTML(http.StatusOK, tplAuthorizedIntegrationViewGeneric)
}
