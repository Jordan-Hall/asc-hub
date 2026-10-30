// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The People page of an organization (board OrgPeople): roles read from the teams, the owner's Change role, the pending
// invitations and the invite dialog that takes several names.
// org3: user2 is the only owner; user28 is in team12Creators, which has the administrator permission; user4 is in team1 (write).
func TestOrgPeopleRoles(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	const members = "/org/org3/members"
	owner := loginUser(t, "user2")

	chips := func(session *TestSession) (owners, maintainers, rolesOfMembers int) {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)
		doc.Find(".wk-member .ds-li-title .ds-chip").Each(func(_ int, s *goquery.Selection) {
			switch strings.TrimSpace(s.Text()) {
			case "Owner":
				owners++
			case "Maintainer":
				maintainers++
			case "Member":
				rolesOfMembers++
			}
		})
		return
	}
	o, m, _ := chips(owner)
	assert.Equal(t, 1, o, "user2 is the only owner")
	assert.Equal(t, 1, m, "user28 is in a team with the administrator permission")

	inTeam := func(team, uid int64) bool {
		ok, err := organization.IsTeamMember(db.DefaultContext, 3, team, uid)
		require.NoError(t, err)
		return ok
	}
	role := func(uid int64, role string, status int) {
		req := NewRequest(t, "POST", fmt.Sprintf("%s/action/role?uid=%d&role=%s", members, uid, role))
		owner.MakeRequest(t, req, status)
	}

	// a Member becomes a Maintainer: they join the team that has the administrator permission and keep their own team
	role(4, "maintainer", http.StatusOK)
	assert.True(t, inTeam(12, 4))
	assert.True(t, inTeam(2, 4))
	_, m, _ = chips(owner)
	assert.Equal(t, 2, m)

	// and an Owner: the owners team, and no longer the maintainers' team
	role(4, "owner", http.StatusOK)
	assert.True(t, inTeam(1, 4))
	assert.False(t, inTeam(12, 4))
	o, _, _ = chips(owner)
	assert.Equal(t, 2, o)

	// and a Member again: out of the owners team, still in their own
	role(4, "member", http.StatusOK)
	assert.False(t, inTeam(1, 4))
	assert.True(t, inTeam(2, 4))

	// the last owner cannot be demoted
	role(2, "member", http.StatusOK)
	assert.True(t, inTeam(1, 2), "the only owner stays an owner")

	// only an owner changes roles, and only to a role that exists
	loginUser(t, "user4").MakeRequest(t, NewRequest(t, "POST", members+"/action/role?uid=15&role=owner"), http.StatusNotFound)
	role(4, "emperor", http.StatusNotFound)
}

// Where no team has the administrator permission, making someone a Maintainer makes the team; and where there is no other
// team to keep a Maintainer in, they cannot become a Member (it is said, nothing changes).
func TestOrgPeopleMaintainerTeamIsMadeWhenThereIsNone(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	// org7 has an owners team (user5) and no other team
	owner := loginUser(t, "user5")
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/org/org7/members/action/add", map[string]string{"uid": "5", "uname": "user2", "team_4": "on"}), http.StatusSeeOther)
	owner.MakeRequest(t, NewRequest(t, "POST", "/org/org7/members/action/role?uid=2&role=maintainer"), http.StatusOK)
	team, err := organization.GetTeam(db.DefaultContext, 7, "Maintainers")
	require.NoError(t, err)
	assert.Equal(t, perm.AccessModeAdmin, team.AccessMode)
	in := func(teamID, uid int64) bool {
		ok, err := organization.IsTeamMember(db.DefaultContext, 7, teamID, uid)
		require.NoError(t, err)
		return ok
	}
	assert.True(t, in(team.ID, 2))
	assert.False(t, in(4, 2), "a Maintainer is not also an owner")
	assert.True(t, in(4, 5))

	// no team to keep them in as a Member
	owner.MakeRequest(t, NewRequest(t, "POST", "/org/org7/members/action/role?uid=2&role=member"), http.StatusOK)
	assert.True(t, in(team.ID, 2), "the person is still a Maintainer, so still in the organisation")

	// somebody who is not a member of the organisation has no role to change
	owner.MakeRequest(t, NewRequest(t, "POST", "/org/org7/members/action/role?uid=8&role=owner"), http.StatusNotFound)
}

func TestOrgPeopleInvitations(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.AddMembersByInvitations, true)()
	const members = "/org/org3/members"
	owner := loginUser(t, "user2")

	// several names at once; a name nobody has and an address (the test server has a mailer) are told apart
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", members+"/action/add", map[string]string{
		"uid": "2", "invitees": "user5, nobody-by-this-name\nuser8", "role": "member",
	}), http.StatusSeeOther)
	for _, uid := range []int64{5, 8} {
		ok, err := organization.IsInvitedToOrganization(db.DefaultContext, 3, uid)
		require.NoError(t, err)
		assert.True(t, ok, "user %d is invited", uid)
	}
	doc := NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)
	assert.Contains(t, doc.Find(".flash-error").Text(), "nobody-by-this-name")
	assert.Contains(t, doc.Find(".flash-success").Text(), "Invited 2")

	// the pending list, for the owner only
	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members+"?filter=pending"), http.StatusOK).Body)
	assert.Equal(t, 2, doc.Find(`[data-testid="people-invite"]`).Length())
	assert.Equal(t, 0, doc.Find(".wk-member:not([data-testid])").Length(), "the member list is not shown under Pending")
	member := loginUser(t, "user4")
	doc = NewHTMLParser(t, member.MakeRequest(t, NewRequest(t, "GET", members+"?filter=pending"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`[data-testid="people-invite"]`).Length(), "a member does not see invitations")

	// revoke one; resend the other; a member may do neither
	var revokeURL, resendURL string
	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members+"?filter=pending"), http.StatusOK).Body)
	doc.Find(`[data-testid="people-invite"]`).Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			revokeURL, _ = s.Find(`a[data-url*="revoke_invite"]`).Attr("data-url")
		} else {
			resendURL, _ = s.Find(`a[data-url*="resend_invite"]`).Attr("data-url")
		}
	})
	require.NotEmpty(t, revokeURL)
	require.NotEmpty(t, resendURL)
	member.MakeRequest(t, NewRequest(t, "POST", revokeURL), http.StatusNotFound)
	owner.MakeRequest(t, NewRequest(t, "POST", resendURL), http.StatusOK)
	owner.MakeRequest(t, NewRequest(t, "POST", revokeURL), http.StatusOK)
	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members+"?filter=pending"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="people-invite"]`).Length())
	// a second revoke of the same invitation is told, not an error
	owner.MakeRequest(t, NewRequest(t, "POST", revokeURL), http.StatusOK)
}

// Teams and roles are for the members of an organization: a visitor who is signed out, or signed in but not a member, sees
// the people that are public and nothing of the teams (their names, or who administers them).
func TestOrgPeopleTeamsAndRolesAreForMembers(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	const members = "/org/org3/members"
	secrets := []string{"team1", "test_team", "team12Creators", "teamCreateRepo", "Maintainer", "Teams:", "No teams"}
	check := func(who string, body *bytes.Buffer) {
		doc := NewHTMLParser(t, body)
		text := doc.Find(".wk-people-page").Text()
		for _, secret := range secrets {
			assert.NotContains(t, text, secret, "%s sees %q", who, secret)
		}
		assert.Equal(t, 0, doc.Find(".wk-member .ds-li-title .ds-chip").FilterFunction(func(_ int, s *goquery.Selection) bool {
			txt := strings.TrimSpace(s.Text())
			return txt == "Owner" || txt == "Maintainer" || txt == "Member"
		}).Length(), "%s sees roles", who)
		assert.Equal(t, 0, doc.Find(".wk-menu").Length(), "%s gets no role menu", who)
		assert.Equal(t, 0, doc.Find("#add-member-modal").Length(), "%s gets no invite dialog with the teams in it", who)
	}
	check("a visitor", MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)
	// user1 is a site administrator but not a member of org3; user8 is just somebody
	check("a signed-in non-member", loginUser(t, "user8").MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)

	// the same page, for a member, shows them (the test would pass for nothing if the page were empty)
	member := NewHTMLParser(t, loginUser(t, "user4").MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)
	assert.Contains(t, member.Find(".wk-people-page").Text(), "team1")
	// and only an owner is offered the role menu
	assert.Equal(t, 0, member.Find(".wk-menu summary:contains('Change role')").Length())
	owner := NewHTMLParser(t, loginUser(t, "user2").MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)
	assert.Positive(t, owner.Find(".wk-menu summary:contains('Change role')").Length())
}

// After a role changed the page says so and offers Undo, which changes it back and offers nothing more; the owners come first.
func TestOrgPeopleUndoAndOrder(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	const members = "/org/org3/members"
	owner := loginUser(t, "user2")

	resp := owner.MakeRequest(t, NewRequest(t, "POST", members+"/action/role?uid=4&role=maintainer"), http.StatusOK)
	assert.Contains(t, resp.Body.String(), "undo=4-member-maintainer")
	doc := NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members+"?undo=4-member-maintainer"), http.StatusOK).Body)
	banner := doc.Find(`[data-testid="role-undo"]`)
	assert.Contains(t, banner.Text(), "is now Maintainer")
	undo, _ := banner.Find("a[data-url]").Attr("data-url")
	assert.Contains(t, undo, "role=member")
	resp = owner.MakeRequest(t, NewRequest(t, "POST", undo), http.StatusOK)
	assert.NotContains(t, resp.Body.String(), "undo=", "an undo offers no undo")
	ok, err := organization.IsTeamMember(db.DefaultContext, 3, 12, 4)
	require.NoError(t, err)
	assert.False(t, ok, "user4 is a Member again")

	// an address that is made up offers nothing, and a member sees no undo line at all
	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members+"?undo=999-owner-member"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`[data-testid="role-undo"]`).Length())
	doc = NewHTMLParser(t, loginUser(t, "user4").MakeRequest(t, NewRequest(t, "GET", members+"?undo=4-member-maintainer"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`[data-testid="role-undo"]`).Length())

	// owners first, then maintainers, then members
	doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", members), http.StatusOK).Body)
	var seen []string
	doc.Find(".wk-member .ds-li-title .ds-chip").Each(func(_ int, s *goquery.Selection) {
		switch txt := strings.TrimSpace(s.Text()); txt {
		case "Owner", "Maintainer", "Member":
			seen = append(seen, txt)
		}
	})
	require.NotEmpty(t, seen)
	order := map[string]int{"Owner": 0, "Maintainer": 1, "Member": 2}
	for i := 1; i < len(seen); i++ {
		assert.LessOrEqual(t, order[seen[i-1]], order[seen[i]], "the roles in order: %v", seen)
	}
	assert.Equal(t, "Owner", seen[0])
}
