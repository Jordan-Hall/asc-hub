// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package organization

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	"forgejo.org/modules/timeutil"

	"xorm.io/builder"
)

// Who may create repositories in an organization.
const (
	// RepoCreatorsTeams is the default: owners, and the members of a team that allows it.
	RepoCreatorsTeams = "teams"
	// RepoCreatorsMaintainers is owners and the members of a team with the administrator permission.
	RepoCreatorsMaintainers = "maintainers"
	// RepoCreatorsOwners is owners only.
	RepoCreatorsOwners = "owners"
)

// The default visibility of a new repository in an organization; empty is the server default.
const (
	RepoVisibilityServer   = ""
	RepoVisibilityPrivate  = "private"
	RepoVisibilityInternal = "internal"
	RepoVisibilityPublic   = "public"
)

// AscRepoDefaults are an organization's rules for its repositories: who may create one, what every member may do in
// all of them whatever their teams, and the visibility the create form starts with. An organization without a row has
// the defaults (teams decide, no base permission, the server's visibility).
type AscRepoDefaults struct {
	OrgID          int64              `xorm:"pk"`
	RepoCreators   string             `xorm:"VARCHAR(16) NOT NULL DEFAULT 'teams'"`
	BasePermission perm.AccessMode    `xorm:"NOT NULL DEFAULT 0"`
	Visibility     string             `xorm:"VARCHAR(16) NOT NULL DEFAULT ''"`
	UpdatedUnix    timeutil.TimeStamp `xorm:"updated"`
}

func init() {
	db.RegisterModel(new(AscRepoDefaults))
}

// TableName is the table of the model.
func (AscRepoDefaults) TableName() string { return "asc_repo_defaults" }

// Validate refuses a value outside the choices of each field.
func (d *AscRepoDefaults) Validate() error {
	switch d.RepoCreators {
	case RepoCreatorsTeams, RepoCreatorsMaintainers, RepoCreatorsOwners:
	default:
		return fmt.Errorf("unknown repository creators %q", d.RepoCreators)
	}
	switch d.BasePermission {
	case perm.AccessModeNone, perm.AccessModeRead, perm.AccessModeWrite:
	default:
		return fmt.Errorf("base permission must be none, read or write, not %d", d.BasePermission)
	}
	switch d.Visibility {
	case RepoVisibilityServer, RepoVisibilityPrivate, RepoVisibilityInternal, RepoVisibilityPublic:
	default:
		return fmt.Errorf("unknown repository visibility %q", d.Visibility)
	}
	return nil
}

// GetAscRepoDefaults returns the rules of the organization, the defaults when it has set none.
func GetAscRepoDefaults(ctx context.Context, orgID int64) (*AscRepoDefaults, error) {
	d := &AscRepoDefaults{OrgID: orgID}
	has, err := db.GetEngine(ctx).Get(d)
	if err != nil {
		return nil, err
	}
	if !has {
		return &AscRepoDefaults{OrgID: orgID, RepoCreators: RepoCreatorsTeams}, nil
	}
	return d, nil
}

// SetAscRepoDefaults stores the rules of the organization. A row equal to the defaults is removed, so the defaults
// stay the absence of a row.
func SetAscRepoDefaults(ctx context.Context, d *AscRepoDefaults) error {
	if err := d.Validate(); err != nil {
		return err
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		if _, err := e.Delete(&AscRepoDefaults{OrgID: d.OrgID}); err != nil {
			return err
		}
		if d.RepoCreators == RepoCreatorsTeams && d.BasePermission == perm.AccessModeNone && d.Visibility == RepoVisibilityServer {
			return nil
		}
		_, err := e.Insert(d)
		return err
	})
}

// GetAscRepoVisibilities is the default visibility of a new repository in each of the organizations that set one.
func GetAscRepoVisibilities(ctx context.Context, orgIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(orgIDs))
	if len(orgIDs) == 0 {
		return out, nil
	}
	var rows []*AscRepoDefaults
	if err := db.GetEngine(ctx).In("org_id", orgIDs).Where("visibility <> ''").Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.OrgID] = r.Visibility
	}
	return out, nil
}

// GetAscBasePermission is what uid may do in every repository of the organization as one of its members: none when
// uid is not a member or the organization grants members nothing.
func GetAscBasePermission(ctx context.Context, orgID, uid int64) (perm.AccessMode, error) {
	var mode int
	has, err := db.GetEngine(ctx).Table("asc_repo_defaults").
		Join("INNER", "org_user", "org_user.org_id = asc_repo_defaults.org_id").
		Where(builder.Eq{"asc_repo_defaults.org_id": orgID, "org_user.uid": uid}).
		Cols("asc_repo_defaults.base_permission").Get(&mode)
	if err != nil || !has {
		return perm.AccessModeNone, err
	}
	return perm.AccessMode(mode), nil
}

// repoCreatorTeamCond selects the rows of `team` whose members may create repositories under their organization's
// rule: the owners team always; a team that allows it while the organization lets teams decide; a team with the
// administrator permission while the organization lets maintainers create.
func repoCreatorTeamCond() builder.Cond {
	withRule := func(cond builder.Cond) *builder.Builder {
		return builder.Select("org_id").From("asc_repo_defaults").Where(cond)
	}
	return builder.Or(
		builder.Eq{"`team`.authorize": perm.AccessModeOwner},
		builder.And(
			builder.Eq{"`team`.can_create_org_repo": true},
			builder.NotIn("`team`.org_id", withRule(builder.Neq{"repo_creators": RepoCreatorsTeams})),
		),
		builder.And(
			builder.Gte{"`team`.authorize": perm.AccessModeAdmin},
			builder.In("`team`.org_id", withRule(builder.Eq{"repo_creators": RepoCreatorsMaintainers})),
		),
	)
}
