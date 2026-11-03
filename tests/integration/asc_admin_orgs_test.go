// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
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
