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

// AscProfileTeams lists the teams of person in the organizations the viewer may see (those in orgs, which the caller
// has already narrowed to the viewer), so the card never names an organization the profile does not show. The owners
// team is listed too. viewer is nil for a visitor who is not signed in.
func AscProfileTeams(ctx context.Context, person *user_model.User, orgs []*organization.Organization, viewer *user_model.User) ([]AscProfileTeam, error) {
	if len(orgs) == 0 {
		return nil, nil
	}
	teams, err := organization.GetUserTeams(ctx, person.ID)
	if err != nil {
		return nil, err
	}
	byOrg := map[int64]*organization.Organization{}
	for _, o := range orgs {
		if o.Visibility.IsPublic() || (viewer != nil && (o.Visibility.IsLimited() || viewer.IsAdmin || person.ID == viewer.ID)) {
			byOrg[o.ID] = o
		}
	}
	var out []AscProfileTeam
	for _, t := range teams {
		o := byOrg[t.OrgID]
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
