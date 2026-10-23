// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
)

// OrgPackagesWriteSettingKey is the key of the owner setting that is the ceiling of
// [repo_model.ActionsConfig.AllowPackagesWrite] for the repositories of an organization.
const OrgPackagesWriteSettingKey = "actions.allow_packages_write"

const orgPackagesWriteOn = "true"

// OrgAllowsPackagesWrite tells whether the owner of a repository lets a job token of the repository have the
// `packages: write` grant. The owner of a user's repository never limits it. An organization limits it: the default
// is no, and only the value "true" is yes. An error is no.
func OrgAllowsPackagesWrite(ctx context.Context, ownerID int64) bool {
	owner, err := user_model.GetUserByID(ctx, ownerID)
	if err != nil {
		return false
	}
	if !owner.IsOrganization() {
		return true
	}
	value, err := user_model.GetUserSetting(ctx, owner.ID, OrgPackagesWriteSettingKey)
	return err == nil && value == orgPackagesWriteOn
}

// SetOrgAllowsPackagesWrite sets the organization's ceiling for the `packages: write` grant of its repositories.
func SetOrgAllowsPackagesWrite(ctx context.Context, org *user_model.User, allow bool) error {
	if allow {
		return user_model.SetUserSetting(ctx, org.ID, OrgPackagesWriteSettingKey, orgPackagesWriteOn)
	}
	return user_model.DeleteUserSetting(ctx, org.ID, OrgPackagesWriteSettingKey)
}

// RepoAllowsPackagesWrite tells whether the Actions setting of the repository is on. The repository can only narrow
// what [OrgAllowsPackagesWrite] gives.
func RepoAllowsPackagesWrite(ctx context.Context, repo *repo_model.Repository) bool {
	cfgUnit, err := repo.GetUnit(ctx, unit.TypeActions)
	return err == nil && cfgUnit.ActionsConfig().AllowPackagesWrite
}

// PackagesWriteAllowed is the one decision whether a job token of the repository can have the `packages: write`
// grant: the owner and the repository both say yes. It is read again whenever the grant is used.
func PackagesWriteAllowed(ctx context.Context, repo *repo_model.Repository) bool {
	return RepoAllowsPackagesWrite(ctx, repo) && OrgAllowsPackagesWrite(ctx, repo.OwnerID)
}
