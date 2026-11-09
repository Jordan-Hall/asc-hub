// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"time"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"

	"xorm.io/builder"
)

// AscAgentActivityDays is how far back the Agent activity chip of the repository lists looks.
const AscAgentActivityDays = 7

// AscQuickFilter is what the quick chips of the repository lists ask for. They add to the other filters of a search.
type AscQuickFilter struct {
	// OpenStacks keeps the repositories with an open pull request: each open stack has one, so these are the
	// repositories with a stack waiting to land.
	OpenStacks bool
	// AgentsSince, when set, keeps the repositories where an agent acted since then. An agent acts on the hub as its
	// bot account (a user of type bot, services/ascagentui EnsureAgentUser), so this is any action of a bot account.
	AgentsSince timeutil.TimeStamp
	// StarredBy, when set, keeps the repositories that user starred.
	StarredBy int64
}

// AgentActivitySince is the start of the window of the Agent activity chip at now.
func AgentActivitySince(now time.Time) timeutil.TimeStamp {
	return timeutil.TimeStamp(now.AddDate(0, 0, -AscAgentActivityDays).Unix())
}

func (q AscQuickFilter) cond() builder.Cond {
	cond := builder.NewCond()
	if q.OpenStacks {
		cond = cond.And(builder.In("`repository`.id", builder.Select("repo_id").From("issue").Where(builder.Eq{"is_pull": true, "is_closed": false})))
	}
	if q.AgentsSince > 0 {
		cond = cond.And(builder.In("`repository`.id", builder.Select("repo_id").From("action").Where(
			builder.Gte{"created_unix": q.AgentsSince}.
				And(builder.In("act_user_id", builder.Select("id").From("`user`").Where(builder.Eq{"type": user_model.UserTypeBot}))),
		)))
	}
	if q.StarredBy > 0 {
		cond = cond.And(builder.In("`repository`.id", builder.Select("repo_id").From("star").Where(builder.Eq{"uid": q.StarredBy})))
	}
	return cond
}
