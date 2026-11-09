// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"fmt"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/structs"

	"xorm.io/builder"
)

// AscOrgSize is a Size choice of the organization directory, by how many members an organization has.
type AscOrgSize string

// The Size choices. The bounds are inclusive; a large organization has no upper bound.
const (
	AscOrgSizeAny    AscOrgSize = ""
	AscOrgSizeSmall  AscOrgSize = "small"  // 1 to 10 members
	AscOrgSizeMedium AscOrgSize = "medium" // 11 to 100 members
	AscOrgSizeLarge  AscOrgSize = "large"  // more than 100 members
)

// AscOrgSizes is the Size choices in the order the filter lists them.
var AscOrgSizes = []AscOrgSize{AscOrgSizeSmall, AscOrgSizeMedium, AscOrgSizeLarge}

// ParseAscOrgSize reads a Size choice; anything else is Any.
func ParseAscOrgSize(s string) AscOrgSize {
	for _, size := range AscOrgSizes {
		if string(size) == s {
			return size
		}
	}
	return AscOrgSizeAny
}

func (s AscOrgSize) cond() builder.Cond {
	switch s {
	case AscOrgSizeSmall:
		return builder.Between{Col: "`user`.num_members", LessVal: 1, MoreVal: 10}
	case AscOrgSizeMedium:
		return builder.Between{Col: "`user`.num_members", LessVal: 11, MoreVal: 100}
	case AscOrgSizeLarge:
		return builder.Gt{"`user`.num_members": 100}
	}
	return builder.NewCond()
}

// AscOrgFilter is what the filters of the organization directory ask for. They add to the other options of a search.
type AscOrgFilter struct {
	// Verified keeps the organizations that proved a domain.
	Verified bool
	// WithAgents keeps the organizations with an agent (a bot account) among their members. A membership the viewer
	// may not see (a private one, in an organization the viewer is not a member of) does not count, unless the
	// viewer is an admin.
	WithAgents bool
	// ViewerMember keeps the organizations the viewer is a member of.
	ViewerMember bool
	// Size keeps the organizations of that size.
	Size AscOrgSize
	// Viewer is the signed-in viewer, nil for a visitor.
	Viewer *User
}

func (f AscOrgFilter) cond(cond builder.Cond) builder.Cond {
	if f.Verified {
		cond = cond.And(builder.In("`user`.id", builder.Select("org_id").From("asc_org_domain").Where(builder.Gt{"verified_unix": 0})))
	}
	if f.WithAgents {
		agents := builder.In("uid", builder.Select("id").From("`user`").Where(builder.Eq{"type": UserTypeBot}))
		if f.Viewer == nil || !f.Viewer.IsAdmin {
			seen := builder.Cond(builder.Eq{"is_public": true})
			if f.Viewer != nil {
				seen = seen.Or(builder.In("org_id", builder.Select("o.org_id").From("org_user", "o").Where(builder.Eq{"o.uid": f.Viewer.ID})))
			}
			agents = agents.And(seen)
		}
		cond = cond.And(builder.In("`user`.id", builder.Select("org_id").From("org_user").Where(agents)))
	}
	if f.ViewerMember {
		if f.Viewer == nil {
			return builder.Expr("1 = 0")
		}
		cond = cond.And(builder.In("`user`.id", builder.Select("org_id").From("org_user").Where(builder.Eq{"uid": f.Viewer.ID})))
	}
	return cond.And(f.Size.cond())
}

// AscMostActiveOrgOrder is the Most active order of the organization directory at now: most actions in the public
// repositories of the organization in the last AscActiveDays days first. A private or limited organization counts no
// action, and the actions of people who keep their activity private are left out, so the order tells nothing a
// visitor could not read in the public feeds. Each action counts once (the copy
// in the feed of the actor). A tie goes to the name.
func AscMostActiveOrgOrder(now time.Time) db.SearchOrderBy {
	since := now.AddDate(0, 0, -AscActiveDays).Unix()
	return db.SearchOrderBy(fmt.Sprintf("CASE WHEN `user`.visibility = %d THEN (SELECT COUNT(*) FROM `action` "+
		"WHERE `action`.user_id = `action`.act_user_id AND `action`.is_private = false AND `action`.created_unix >= %d "+
		"AND `action`.repo_id IN (SELECT `repository`.id FROM `repository` WHERE `repository`.owner_id = `user`.id "+
		"AND `repository`.is_private = false) AND `action`.act_user_id NOT IN (SELECT `actor`.id FROM `user` `actor` "+
		"WHERE `actor`.keep_activity_private = true)) ELSE 0 END DESC, `user`.name ASC",
		structs.VisibleTypePublic, since))
}
