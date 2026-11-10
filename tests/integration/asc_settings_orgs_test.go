// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The UserSettingsOrgs board notes: Leave asks to confirm in a dialog; the last owner cannot leave and is told
// why, with a link to People; a person with no organizations sees the empty state; a pending invitation shows
// with Decline and Accept.
func TestAscSettingsOrgsBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	sess := loginUser(t, "user2")
	get := func(s *TestSession) *HTMLDoc {
		return NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/user/settings/organization"), http.StatusOK).Body)
	}
	row := func(doc *HTMLDoc, org string) *goquery.Selection {
		return doc.Find(".wk-rows.orgs .wk-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
			href, _ := s.Find(".ds-li-title a").Attr("href")
			return href == "/"+org
		})
	}

	// user2 is the only member of the owners team of org3
	doc := get(sess)
	r := row(doc, "org3")
	require.Equal(t, 1, r.Length())
	assert.Equal(t, 1, r.Find(`[data-testid="org-leave-blocked"][disabled]`).Length())
	assert.Equal(t, 0, r.Find(`[data-testid="org-leave"]`).Length())
	note := r.Find(`[data-testid="org-last-owner"]`)
	require.Equal(t, 1, note.Length())
	href, _ := note.Find("a").Attr("href")
	assert.Equal(t, "/org/org3/members", href)
	desc, _ := r.Find(`[data-testid="org-leave-blocked"]`).Attr("aria-describedby")
	id, _ := note.Attr("id")
	assert.Equal(t, id, desc)
	settings, _ := r.Find(`a[href="/org/org3/settings"]`).Attr("href")
	assert.Equal(t, "/org/org3/settings", settings, "an owner gets a Settings link")
	assert.Equal(t, 1, doc.Find("#leave-organization.modal").Length())

	// the leave action refuses the last owner as well
	sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/org/org3/members/action/leave", map[string]string{}), NoExpectedStatus)
	isOwner, err := organization.IsOrganizationOwner(db.DefaultContext, 3, 2)
	require.NoError(t, err)
	assert.True(t, isOwner, "the last owner stays")

	// with a second owner, Leave opens the confirm dialog
	require.NoError(t, db.Insert(db.DefaultContext, &organization.TeamUser{OrgID: 3, TeamID: 1, UID: 4}))
	_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE `team` SET num_members = 2 WHERE id = 1")
	require.NoError(t, err)
	r = row(get(sess), "org3")
	leave := r.Find(`[data-testid="org-leave"]`)
	require.Equal(t, 1, leave.Length())
	modal, _ := leave.Attr("data-modal-id")
	assert.Equal(t, "leave-organization", modal)
	url, _ := leave.Attr("data-url")
	assert.Equal(t, "/org/org3/members/action/leave", url)
	assert.Equal(t, 0, r.Find(`[data-testid="org-last-owner"]`).Length())

	// a person in no organization sees the empty state, then a pending invitation
	unittest.AssertCount(t, &organization.OrgUser{UID: 8}, 0)
	s8 := loginUser(t, "user8")
	doc = get(s8)
	assert.Equal(t, 1, doc.Find(`[data-testid="orgs-empty"]`).Length())
	assert.Equal(t, 0, doc.Find(`[data-testid="org-invites"]`).Length())
	require.NoError(t, db.Insert(db.DefaultContext, &organization.TeamInvite{Token: "asc-orgs-board", InviterID: 2, OrgID: 3, TeamID: 2, Email: "user8@example.com", InvitedID: optional.Some[int64](8)}))
	doc = get(s8)
	inv := doc.Find(`[data-testid="org-invites"] .wk-row`)
	require.Equal(t, 1, inv.Length())
	org3 := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	assert.Contains(t, inv.Find(".ds-li-title").Text(), org3.DisplayName())
	accept, _ := inv.Find("a.ds-btn-primary").Attr("href")
	assert.Equal(t, "/org/invite/asc-orgs-board", accept)
	assert.Equal(t, 1, inv.Find(`form[action="/user/settings/organization/decline"] input[name="token"][value="asc-orgs-board"]`).Length())
}
