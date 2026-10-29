// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"

	"forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
)

// AscProfileTeam is a team of the person whose profile is shown, with the organization it belongs to.
type AscProfileTeam struct {
	Name string
	Org  string
	Link string
}

// ascProfileTeamsMax caps the teams of the profile card.
const ascProfileTeamsMax = 12

// AscProfileTeams lists the teams of person that viewer may know about. The teams of an organization are for its own
// members: the person themself, a site administrator and a member of that organization see them; a visitor, and a
// person who is not a member of that organization, see none, also where the organization is public and also where the
// person made their membership public (the organization card of the profile shows that, the teams are more). orgs are
// the organizations the profile already lists for the viewer; the owners team is listed like any other.
func AscProfileTeams(ctx context.Context, person *user_model.User, orgs []*organization.Organization, viewer *user_model.User) ([]AscProfileTeam, error) {
	if viewer == nil || len(orgs) == 0 {
		return nil, nil
	}
	allowed := map[int64]*organization.Organization{}
	for _, o := range orgs {
		if viewer.IsAdmin || viewer.ID == person.ID {
			allowed[o.ID] = o
			continue
		}
		member, err := organization.IsOrganizationMember(ctx, o.ID, viewer.ID)
		if err != nil {
			return nil, err
		}
		if member {
			allowed[o.ID] = o
		}
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	teams, err := organization.GetUserTeams(ctx, person.ID)
	if err != nil {
		return nil, err
	}
	var out []AscProfileTeam
	for _, t := range teams {
		o := allowed[t.OrgID]
		if o == nil {
			continue
		}
		out = append(out, AscProfileTeam{Name: t.Name, Org: o.DisplayName(), Link: o.HomeLink() + "/teams/" + t.LowerName})
		if len(out) == ascProfileTeamsMax {
			break
		}
	}
	return out, nil
}
