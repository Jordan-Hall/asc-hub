// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"

	"forgejo.org/models/db"

	"xorm.io/builder"
)

// The roles of the people directory (board ExploreUsers). The hub has no role of its own beyond site admin, so a role
// is read from what an account can do:
//   - owner: a site admin (runs the hub);
//   - maintainer: can write to a repository it does not own (a collaborator or a team member with write);
//   - agent: a bot account (services/ascagentui EnsureAgentUser);
//   - member: anyone else.
const (
	AscRoleOwner      = "owner"
	AscRoleMaintainer = "maintainer"
	AscRoleMember     = "member"
	AscRoleAgent      = "agent"
)

// AscRoles is the order the roles are shown in.
var AscRoles = []string{AscRoleOwner, AscRoleMaintainer, AscRoleMember, AscRoleAgent}

// accessModeWrite is perm.AccessModeWrite; the user package cannot import models/perm/access.
const accessModeWrite = 2

func ascWritersCond() builder.Cond {
	return builder.In("`user`.id", builder.Select("user_id").From("access").Where(builder.Gte{"mode": accessModeWrite}))
}

// AscRolesOf gives the role of each of the users.
func AscRolesOf(ctx context.Context, users []*User) (map[int64]string, error) {
	roles := make(map[int64]string, len(users))
	var ids []int64
	for _, u := range users {
		switch {
		case u.IsOrganization():
		case u.IsBot():
			roles[u.ID] = AscRoleAgent
		case u.IsAdmin:
			roles[u.ID] = AscRoleOwner
		default:
			roles[u.ID] = AscRoleMember
			ids = append(ids, u.ID)
		}
	}
	if len(ids) == 0 {
		return roles, nil
	}
	var writers []int64
	if err := db.GetEngine(ctx).Table("access").Distinct("user_id").
		In("user_id", ids).And(builder.Gte{"mode": accessModeWrite}).
		Find(&writers); err != nil {
		return nil, err
	}
	for _, id := range writers {
		roles[id] = AscRoleMaintainer
	}
	return roles, nil
}

// AscRoleCounts counts the active accounts of the hub the actor may see, by role.
func AscRoleCounts(ctx context.Context, actor *User) (map[string]int64, error) {
	base := builder.Eq{"is_active": true}.And(BuildCanSeeUserCondition(actor))
	people := builder.In("type", UserTypeIndividual, UserTypeRemoteUser)
	conds := map[string]builder.Cond{
		AscRoleOwner:      people.And(builder.Eq{"is_admin": true}),
		AscRoleMaintainer: people.And(builder.Eq{"is_admin": false}, ascWritersCond()),
		AscRoleMember:     people.And(builder.Eq{"is_admin": false}, builder.Not{ascWritersCond()}),
		AscRoleAgent:      builder.Eq{"type": UserTypeBot},
	}
	counts := make(map[string]int64, len(conds))
	for role, cond := range conds {
		n, err := db.GetEngine(ctx).Table("user").Where(base.And(cond)).Count()
		if err != nil {
			return nil, err
		}
		counts[role] = n
	}
	return counts, nil
}
