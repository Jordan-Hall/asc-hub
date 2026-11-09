// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"fmt"
	"time"

	"forgejo.org/models/db"

	"xorm.io/builder"
)

// AscActiveDays is how far back the Most active order of the people directory counts actions.
const AscActiveDays = 90

// AscUserFilter is what the filters of the people directory ask for. They add to the other options of a search.
type AscUserFilter struct {
	// WithAgents also finds the agents (bot accounts, services/ascagentui EnsureAgentUser) when the search is for
	// people (Type UserTypeIndividual).
	WithAgents bool
	// MemberOf, when set, keeps the members of that organization: the public members, or every member when
	// AllMembers is set (the viewer is a member of the organization or an admin). A negative MemberOf finds no one
	// (an organization the viewer may not see).
	MemberOf   int64
	AllMembers bool
}

func (f AscUserFilter) cond(t UserType, cond builder.Cond) builder.Cond {
	if f.WithAgents && t == UserTypeIndividual {
		cond = builder.Or(cond, builder.Eq{"type": UserTypeBot})
	}
	if f.MemberOf != 0 {
		members := builder.Eq{"org_id": f.MemberOf}
		if !f.AllMembers {
			members = builder.Eq{"org_id": f.MemberOf, "is_public": true}
		}
		cond = builder.And(cond, builder.In("`user`.id", builder.Select("uid").From("org_user").Where(members)))
	}
	return cond
}

// AscMostActiveOrder is the Most active order of the people directory at now: most actions in the last
// AscActiveDays days first. Only the actions anyone may see count, those in public repositories by people who do not
// keep their activity private, so the order tells nothing a visitor could not read in the public feeds. Each action
// counts once (the copy in the feed of the actor). A tie goes to the name.
func AscMostActiveOrder(now time.Time) db.SearchOrderBy {
	since := now.AddDate(0, 0, -AscActiveDays).Unix()
	return db.SearchOrderBy(fmt.Sprintf("CASE WHEN `user`.keep_activity_private = true THEN 0 ELSE (SELECT COUNT(*) FROM `action` "+
		"WHERE `action`.act_user_id = `user`.id AND `action`.user_id = `user`.id AND `action`.is_private = false "+
		"AND `action`.created_unix >= %d) END DESC, `user`.name ASC", since))
}
