// Copyright 2026 The Forgejo Authors.
// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"context"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
)

func DeterminePackageAccessMode(ctx context.Context, owner, doer *user_model.User) (perm.AccessMode, error) {
	if setting.Service.RequireSignInView && (doer == nil || doer.IsGhost()) {
		return perm.AccessModeNone, nil
	}

	if doer != nil && !doer.IsGhost() && !doer.IsAccessAllowed(ctx) {
		return perm.AccessModeNone, nil
	}

	// TODO: ActionUser permission check
	accessMode := perm.AccessModeNone
	if owner.IsOrganization() {
		org := organization.OrgFromUser(owner)

		if doer != nil && !doer.IsGhost() {
			// 1. If user is logged in, check all team packages permissions
			var err error
			accessMode, err = org.GetOrgUserMaxAuthorizeLevel(ctx, doer.ID)
			if err != nil {
				return accessMode, err
			}
			// If access mode is less than write check every team for more permissions
			// The minimum possible access mode is read for org members
			if accessMode < perm.AccessModeWrite {
				teams, err := organization.GetUserOrgTeams(ctx, org.ID, doer.ID)
				if err != nil {
					return accessMode, err
				}
				for _, t := range teams {
					perm := t.UnitAccessMode(ctx, unit.TypePackages)
					if accessMode < perm {
						accessMode = perm
					}
				}
			}
		}
		if accessMode == perm.AccessModeNone && organization.HasOrgOrUserVisible(ctx, owner, doer) {
			// 2. If user is unauthorized or no org member, check if org is visible
			accessMode = perm.AccessModeRead
		}
	} else {
		if doer != nil && !doer.IsGhost() {
			// 1. Check if user is package owner
			if doer.ID == owner.ID {
				accessMode = perm.AccessModeOwner
			} else if owner.Visibility == api.VisibleTypePublic || owner.Visibility == api.VisibleTypeLimited { // 2. Check if package owner is public or limited
				accessMode = perm.AccessModeRead
			}
		} else if owner.Visibility == api.VisibleTypePublic { // 3. Check if package owner is public
			accessMode = perm.AccessModeRead
		}
	}

	return accessMode, nil
}

// DeterminePackageAccessModeForTask is [DeterminePackageAccessMode] for the job token of an Actions task. The token
// has the access of the Actions user, which it keeps whatever its workflow says, plus one grant: write access to the
// packages of the owner of its own repository, when the workflow's `permissions:` has `packages: write` and the
// repository's Actions setting allows it. A fork pull request never has the grant.
func DeterminePackageAccessModeForTask(ctx context.Context, owner, doer *user_model.User, taskID int64) (perm.AccessMode, error) {
	accessMode, err := DeterminePackageAccessMode(ctx, owner, doer)
	if err != nil {
		return accessMode, err
	}

	task, err := actions_model.GetTaskByID(ctx, taskID)
	if err != nil {
		return perm.AccessModeNone, err
	}
	if task.IsForkPullRequest || task.OwnerID != owner.ID {
		return accessMode, nil
	}
	tokenPerms, err := task.TokenPermissions()
	if err != nil {
		return perm.AccessModeNone, err
	}
	if tokenPerms == nil || tokenPerms.Level(actions_model.TokenScopePackages) != actions_model.TokenPermissionWrite {
		return accessMode, nil
	}

	// The setting is read again so that turning it off ends the grant of a job that is running.
	repo, err := repo_model.GetRepositoryByID(ctx, task.RepoID)
	if err != nil {
		return perm.AccessModeNone, err
	}
	if !actions_model.PackagesWriteAllowed(ctx, repo) {
		return accessMode, nil
	}
	return max(accessMode, perm.AccessModeWrite), nil
}
