// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The organizations page of the admin says where each organization keeps its objects, and Open goes to the
// organization page.
func TestAdminOrgsStorageColumn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	f := ascbyotest.Start(t)
	f.AddTarget(&ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_ORG, Name: "org3"}, "garage-eu", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)

	session := loginUser(t, "user1")
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/orgs?q=org"), http.StatusOK).Body)
	storage := map[string]string{}
	doc.Find(".ds-table tbody tr").Each(func(_ int, row *goquery.Selection) {
		name := strings.TrimSpace(row.Find(".adm-who-sub").Text())
		storage[name] = strings.TrimSpace(row.Find(`[data-testid="org-storage"]`).Text())
		href, _ := row.Find("a.ds-btn").Attr("href")
		assert.Equal(t, "/"+name, href, "Open goes to the organization page")
	})
	assert.Equal(t, "garage-eu", storage["org3"])
	assert.Equal(t, "Server default", storage["org6"])
	assert.NotContains(t, doc.Find(".ds-table thead").Text(), "Teams")
}

// Board note: the default list, a search with no match and the empty state of a fresh install.
func TestAscAdminOrgsBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user1")

	// default: every organization, a count under the table and New organization in the header
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/orgs"), http.StatusOK).Body)
	assert.Positive(t, doc.Find(".ds-table tbody tr").Length())
	assert.Contains(t, doc.Find(".adm-table-foot").Text(), "shown")
	assert.Positive(t, doc.Find(`a[href="/org/create"]`).Length())
	assert.Equal(t, 0, doc.Find(".ds-empty").Length())

	// a search with no match says so and offers Clear filters back to the full list
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/orgs?q=zz-no-such-org"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(".ds-table").Length())
	body := doc.Find(".ds-card").Text()
	assert.Contains(t, body, "No organization matches this search.")
	assert.Contains(t, body, "Clear filters")
	assert.Positive(t, doc.Find(`.ds-card a[href="/admin/orgs"]`).Length())
	val, _ := doc.Find("#adm-org-q").Attr("value")
	assert.Equal(t, "zz-no-such-org", val, "the search keeps what was typed")

	// empty: a fresh install has no organizations yet and the page offers to create the first
	_, err := db.GetEngine(t.Context()).Where("type = ?", user_model.UserTypeOrganization).Delete(new(user_model.User))
	require.NoError(t, err)
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/orgs"), http.StatusOK).Body)
	body = doc.Find(".ds-card").Text()
	assert.Contains(t, body, "No organizations yet")
	assert.NotContains(t, body, "Clear filters")
	assert.Positive(t, doc.Find(`.ds-card a[href="/org/create"]`).Length())
}
