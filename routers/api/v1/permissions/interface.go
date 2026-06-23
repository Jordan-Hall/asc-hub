// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"context"

	auth_model "forgejo.org/models/auth"
	org_model "forgejo.org/models/organization"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/auth"
	"forgejo.org/services/authz"
)

type Context interface {
	GetContext() context.Context

	GetRepository() *repo_model.Repository

	GetDoer() *user_model.User

	GetUser() *user_model.User

	GetOrg() *org_model.Organization

	GetTeam() *org_model.Team

	GetPackageOwner() *user_model.User
	GetPackageAccessMode() perm.AccessMode

	GetPermission() *access_model.Permission
	SetPermission(*access_model.Permission)

	GetIsSigned() bool

	GetPublicOnly() bool
	SetPublicOnly(bool)

	GetReducer() authz.AuthorizationReducer
	SetReducer(authz.AuthorizationReducer)

	GetAuthentication() auth.AuthenticationResult

	RequiredScopeCategories() []auth_model.AccessTokenScopeCategory
	SetRequiredScopeCategories([]auth_model.AccessTokenScopeCategory)

	Error(status int, title string, obj any)
	InternalServerError(err error)
	NotFound(objs ...any)

	GetError() error
	WrittenStatus() int
}
