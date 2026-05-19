// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/base"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/modules/web"
	auth_service "forgejo.org/services/auth"
	"forgejo.org/services/authz"
	"forgejo.org/services/context"

	"xorm.io/builder"
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
	// Top data in UI, descriptive information about the Authorized Integration:
	Name        string
	Description string
	Audience    string

	// Middle data in UI, how JWTs are validated by this Authorized Integration:
	Issuer     string // Future: Issuer is likely to be replaced with more-specific fields on non-generic UIs
	ClaimRules string // Future: ClaimRules is only required when aiUI == "generic"

	// Bottom data in the UI, what authorization is permitted by this Authorized Integration:
	Resource     string   // all, public-only, repo-specific
	SelectedRepo []string // slice of ownername/reponame for repo-specific
	ScopeAll     bool
	Scope        []string

	// Values used for repo-specific repository multi-select UI, not stored in Authorized Integration:
	RepoSearch         string
	AddSelectedRepo    string // add a repo to SelectedRepo
	RemoveSelectedRepo string // remove a repo from SelectedRepo
	Page               int    // repo search page
	SetPage            int    // repo search buttons
}

func (f *AuthorizedIntegrationForm) isEmpty() bool {
	return f.Name == "" && f.Description == "" && f.Audience == "" && f.Issuer == "" &&
		f.ClaimRules == "" && f.Resource == "" && f.SelectedRepo == nil && f.Scope == nil
}

func getAuthorizedIntegration(ctx *context.Context) *auth_model.AuthorizedIntegration {
	aiUIString := ctx.Params("ui")
	aiUI, err := auth_model.ParseAuthorizedIntegrationUI(aiUIString)
	if err != nil {
		ctx.NotFound("ParseAuthorizedIntegrationUI", err)
		return nil
	}

	aiID := ctx.ParamsInt64("id")
	ai, err := auth_model.GetAuthorizedIntegrationByUI(ctx, ctx.Doer.ID, aiUI, aiID)
	if errors.Is(err, util.ErrNotExist) {
		ctx.NotFound("GetAuthorizedIntegrationByUI", err)
		return nil
	} else if err != nil {
		ctx.ServerError("GetAuthorizedIntegrationByUI", err)
		return nil
	}

	return ai
}

func EditAuthorizedIntegration(ctx *context.Context) {
	ai := getAuthorizedIntegration(ctx)
	if ctx.Written() {
		return
	}

	form := web.GetForm(ctx).(*AuthorizedIntegrationForm)
	if form.isEmpty() {
		repos, err := auth_model.GetRepositoriesAccessibleWithIntegration(ctx, ai.ID)
		if err != nil {
			ctx.ServerError("GetRepositoriesAccessibleWithIntegration", err)
			return
		}

		form, err = copyAuthorizedIntegrationToForm(ctx, ai, repos)
		if err != nil {
			ctx.ServerError("copyAuthorizedIntegrationToForm", err)
			return
		}
	}
	ctx.Data["Form"] = form

	EditAuthorizedIntegrationRenderCommon(ctx)
}

func EditAuthorizedIntegrationPost(ctx *context.Context) {
	form := web.GetForm(ctx).(*AuthorizedIntegrationForm)
	ctx.Data["Form"] = form // make form available for template render on any error

	ai := getAuthorizedIntegration(ctx)
	if ctx.Written() {
		return
	}

	rr, err := copyFormToAuthorizedIntegration(ctx, form, ai)
	if err != nil {
		editAuthorizedIntegrationErrorHandler(ctx, err)
		return
	}

	if err := auth_service.UpdateAuthorizedIntegration(ctx, ai, rr); err != nil {
		editAuthorizedIntegrationErrorHandler(ctx, err)
		return
	}

	ctx.Redirect(setting.AppSubURL + "/user/settings/authorized-integrations")
}

func NewAuthorizedIntegration(ctx *context.Context) {
	form := web.GetForm(ctx).(*AuthorizedIntegrationForm)
	if form.isEmpty() {
		form.Resource = "all"
		form.ClaimRules = string("{\n  \"rules\":[]\n}")
	}
	ctx.Data["Form"] = form
	ctx.Data["IsNew"] = true

	EditAuthorizedIntegrationRenderCommon(ctx)
}

func NewAuthorizedIntegrationPost(ctx *context.Context) {
	form := web.GetForm(ctx).(*AuthorizedIntegrationForm)
	ctx.Data["Form"] = form // make form available for template render on any error
	ctx.Data["IsNew"] = true

	ai := &auth_model.AuthorizedIntegration{
		UserID: ctx.Doer.ID,
		UI:     auth_model.AuthorizedIntegrationUIGeneric,
	}
	rr, err := copyFormToAuthorizedIntegration(ctx, form, ai)
	if err != nil {
		editAuthorizedIntegrationErrorHandler(ctx, err)
		return
	}

	if err := auth_service.InsertAuthorizedIntegration(ctx, ai, rr); err != nil {
		editAuthorizedIntegrationErrorHandler(ctx, err)
		return
	}

	ctx.Flash.Success(ctx.Tr("settings.authorized_integration.create_success", ai.Name))
	ctx.Redirect(setting.AppSubURL + fmt.Sprintf("/user/settings/authorized-integrations/generic/%d", ai.ID))
}

func editAuthorizedIntegrationErrorHandler(ctx *context.Context, err error) {
	var errMissingField *auth_service.MissingFieldError
	switch {
	case errors.As(err, &errMissingField):
		switch errMissingField.Field {
		case "Name":
			ctx.Data["Err_Name"] = true
			ctx.Flash.Error(ctx.Tr("settings.authorized_integration.name.required"), true)
		default:
			// Unrecognized field; fallback to server error handling.
			ctx.ServerError("UpdateAuthorizedIntegration", err)
			return
		}
		EditAuthorizedIntegrationRenderCommon(ctx)
		return
	case errors.Is(err, auth_service.ErrInvalidIssuer):
		ctx.Data["Err_Issuer"] = true
		ctx.Flash.Error(ctx.Tr("settings.authorized_integration.issuer.invalid", err.Error()), true)
		EditAuthorizedIntegrationRenderCommon(ctx)
		return
	case errors.Is(err, auth_service.ErrInvalidClaimRules):
		ctx.Data["Err_ClaimRules"] = true
		ctx.Flash.Error(ctx.Tr("settings.authorized_integration.claim_rules.invalid", err.Error()), true)
		EditAuthorizedIntegrationRenderCommon(ctx)
		return
	case errors.Is(err, authz.ErrSpecifiedReposNone):
		ctx.Data["Err_SelectedRepo"] = true
		ctx.Flash.Error(ctx.Tr("settings.authorized_integration.specified_repos_none"), true)
		EditAuthorizedIntegrationRenderCommon(ctx)
		return
	case errors.Is(err, authz.ErrSpecifiedReposNoPublicOnly):
		ctx.Data["Err_SelectedRepo"] = true
		ctx.Flash.Error(ctx.Tr("settings.authorized_integration.specified_repos_and_public_only"), true)
		EditAuthorizedIntegrationRenderCommon(ctx)
		return
	case errors.Is(err, authz.ErrSpecifiedReposInvalidScope):
		ctx.Data["Err_SelectedRepo"] = true
		ctx.Data["Err_Scope"] = true
		ctx.Flash.Error(ctx.Tr("settings.authorized_integration.specified_repos_and_invalid_scope"), true)
		EditAuthorizedIntegrationRenderCommon(ctx)
		return
	}

	ctx.ServerError("UpdateAuthorizedIntegration", err)
}

func copyAuthorizedIntegrationToForm(ctx *context.Context, ai *auth_model.AuthorizedIntegration, rr []*auth_model.AuthorizedIntegResourceRepo) (*AuthorizedIntegrationForm, error) {
	form := &AuthorizedIntegrationForm{
		Name:        ai.Name,
		Description: ai.Description,
		Audience:    ai.Audience,
		Issuer:      ai.Issuer, // Future: Issuer is only required when ai.UI == "generic"
	}

	if ai.ResourceAllRepos {
		publicOnly, err := ai.Scope.PublicOnly()
		if err != nil {
			return nil, err
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
	scopeAll, err := ai.Scope.HasScope(auth_model.AccessTokenScopeAll)
	if err != nil {
		return nil, err
	}
	form.ScopeAll = scopeAll

	// Future: ClaimRules is only required when aiUI == "generic"
	claimRulesJSON, err := json.MarshalIndent(ai.ClaimRules, "", "  ")
	if err != nil {
		return nil, err
	}
	form.ClaimRules = string(claimRulesJSON)

	form.SelectedRepo = []string{}
	if len(rr) != 0 {
		repoIDs := make([]int64, len(rr))
		for _, r := range rr {
			repoIDs = append(repoIDs, r.RepoID)
		}
		repos, err := db.GetByIDs(ctx, "id", repoIDs, &repo_model.Repository{})
		if err != nil {
			return nil, err
		}
		for _, r := range rr {
			repo := repos[r.RepoID]
			// Repos associated with an authorized integration should already be visible to the owner, but it's possible
			// that access has changed, such as a removed collaborator on a repo -- don't provide info on that repo if
			// so.
			permission, err := access_model.GetUserRepoPermission(ctx, repo, ctx.Doer)
			if err != nil {
				return nil, err
			}
			if permission.HasAccess() {
				form.SelectedRepo = append(form.SelectedRepo, fmt.Sprintf("%s/%s", repo.OwnerName, repo.Name))
			}
		}
	}

	return form, nil
}

func copyFormToAuthorizedIntegration(ctx *context.Context, form *AuthorizedIntegrationForm, ai *auth_model.AuthorizedIntegration) ([]*auth_model.AuthorizedIntegResourceRepo, error) {
	ai.Name = form.Name
	ai.Description = form.Description

	// ui=Generic, to be refactored later
	ai.Issuer = form.Issuer
	var claimRules *auth_model.ClaimRules

	reader := bytes.NewReader([]byte(form.ClaimRules))
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields() // prevent typo fields from being ignored to make errors easier to identify
	if err := decoder.Decode(&claimRules); err != nil {
		return nil, fmt.Errorf("%w: %w", auth_service.ErrInvalidClaimRules, err)
	}
	// json.Decoder doesn't guarantee that all of the reader is consumed, which can lead to weird situations
	// where the UI appears to work correctly if extra content is in the form field, but it won't be parsed,
	// misleading users.  Detect if anything other than io.EOF comes out of further decodings:
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: unexpected trailing content: %s", auth_service.ErrInvalidClaimRules, extra)
		}
		return nil, fmt.Errorf("%w: error after JSON value: %w", auth_service.ErrInvalidClaimRules, err)
	}
	ai.ClaimRules = claimRules

	scopeRaw := strings.Join(form.Scope, ",")

	var resourceRepos []*auth_model.AuthorizedIntegResourceRepo
	switch form.Resource {
	case "all":
		ai.ResourceAllRepos = true
	case "public-only":
		ai.ResourceAllRepos = true
		scopeRaw = fmt.Sprintf("%s,%s", scopeRaw, auth_model.AccessTokenScopePublicOnly)
	case "repo-specific":
		ai.ResourceAllRepos = false
		selectedRepos, err := getSelectedRepos(ctx, form.SelectedRepo)
		if err != nil {
			return nil, err
		}
		for _, repo := range selectedRepos {
			resourceRepos = append(resourceRepos, &auth_model.AuthorizedIntegResourceRepo{RepoID: repo.ID})
		}
	}

	scope, err := auth_model.AccessTokenScope(scopeRaw).Normalize()
	if err != nil {
		return nil, err
	}
	ai.Scope = scope

	return resourceRepos, nil
}

func EditAuthorizedIntegrationRenderCommon(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.authorized_integrations")
	ctx.Data["PageIsSettingsAuthorizedIntegrations"] = true

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

	repoMultiSelect(ctx)

	ctx.HTML(http.StatusOK, tplAuthorizedIntegrationViewGeneric)
}

func repoMultiSelect(ctx *context.Context) {
	form := ctx.Data["Form"].(*AuthorizedIntegrationForm)

	if form.AddSelectedRepo != "" {
		form.SelectedRepo = append(form.SelectedRepo, form.AddSelectedRepo)
	}
	if form.RemoveSelectedRepo != "" {
		form.SelectedRepo = slices.DeleteFunc(
			form.SelectedRepo,
			func(r string) bool { return r == form.RemoveSelectedRepo },
		)
	}

	selectedRepos, err := getSelectedRepos(ctx, form.SelectedRepo)
	if err != nil {
		ctx.Error(http.StatusBadRequest, "getSelectedRepos")
		return
	}
	ctx.Data["SelectedRepos"] = selectedRepos

	repoSearchText := form.RepoSearch

	page := 1
	// Pagination on the repo search has form submit buttons that send the `set_page` param.  It's then encoded into the
	// page in the hidden input `page` which we fall back to, if anything else causes a form get (eg. adding or removing
	// a repo).
	if form.SetPage > 0 {
		page = form.SetPage
	} else if form.Page > 0 {
		page = form.Page
	}
	pageSize := 10
	repoSearch := &repo_model.SearchRepoOptions{
		Actor:    ctx.Doer,
		Keyword:  repoSearchText,
		Private:  true,
		Archived: optional.Some(false),

		// Restrict repositories to those owned by, or collaborated with, by the user.  Repo-specific access tokens
		// could theoretically be created on any public repository as well, but there wouldn't be much point to that and
		// it would really balloon the search results to an impractical number of repos.
		OwnerID: ctx.Doer.ID,

		ListOptions: db.ListOptions{
			Page:     page,
			PageSize: pageSize,
		},
		OrderBy: db.SearchOrderByAlphabetically, // match sorting in getSelectedRepos for consistency
	}
	cond := repo_model.SearchRepositoryCondition(repoSearch)
	// Exclude all the repos that are currently in `form.SelectedRepo` from the search, by omitting them from the search
	// condition.  This prevents the UI from displaying the same repo on the left and right, and maintains the repo
	// search and page-size correctly.
	for _, selected := range selectedRepos {
		cond = cond.And(builder.Neq{"id": selected.ID})
	}
	repos, count, err := repo_model.SearchRepositoryByCondition(ctx, repoSearch, cond, false)
	if err != nil {
		log.Error("SearchRepository: %v", err)
		ctx.JSON(http.StatusInternalServerError, nil)
		return
	}
	ctx.Data["Repos"] = repos

	pager := context.NewPagination(int(count), pageSize, page, 3)
	pager.SetDefaultParams(ctx)
	ctx.Data["Page"] = pager
}

func DeleteAuthorizedIntegration(ctx *context.Context) {
	if err := auth_model.DeleteAuthorizedIntegrationByID(ctx, ctx.FormInt64("id"), ctx.Doer.ID); err != nil {
		ctx.Flash.Error("DeleteAuthorizedIntegrationByID: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("settings.authorized_integration.deleted"))
	}
	ctx.JSONRedirect(setting.AppSubURL + "/user/settings/authorized-integrations")
}
