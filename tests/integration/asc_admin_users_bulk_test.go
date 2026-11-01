// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bulkRequest(t *testing.T, action string, ids []string, extra map[string]string) *RequestWrapper {
	v := url.Values{"action": {action}, "ids": ids}
	for k, x := range extra {
		v.Set(k, x)
	}
	return NewRequestWithBody(t, "POST", "/admin/users/bulk", strings.NewReader(v.Encode())).
		SetHeader("Content-Type", "application/x-www-form-urlencoded")
}

// People: tick rows, pick Require 2FA or Suspend, see who it changes, give a reason, and it is done.
func TestAdminUsersBulkActions(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	admin := loginUser(t, "user1")
	doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/admin/users"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="admin-users-bulk"]`).Length())
	assert.Equal(t, 0, doc.Find(`input[data-bulk-row][value="1"]`).Length(), "no tick for yourself")
	assert.Positive(t, doc.Find(`input[data-bulk-row][value="2"]`).Length())

	// the confirm page lists the people, never the doer
	resp := admin.MakeRequest(t, bulkRequest(t, "require_2fa", []string{"1", "2", "4"}, nil), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	people := doc.Find(`[data-testid="admin-users-bulk-people"] input[name="ids"]`)
	require.Equal(t, 2, people.Length())
	assert.Equal(t, 0, doc.Find(`[data-testid="admin-users-bulk-people"] input[value="1"]`).Length())

	// a reason is needed
	admin.MakeRequest(t, bulkRequest(t, "require_2fa", []string{"2", "4"}, map[string]string{"confirm": "1"}), http.StatusOK)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).RequireTwoFactor)

	admin.MakeRequest(t, bulkRequest(t, "require_2fa", []string{"2", "4"}, map[string]string{"confirm": "1", "reason": "Audit finding"}), http.StatusSeeOther)
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).RequireTwoFactor)
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}).RequireTwoFactor)
	// user4 has no second factor: sent to add one
	resp = loginUser(t, "user4").MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusSeeOther)
	assert.Equal(t, "/user/settings/security", resp.Header().Get("Location"))

	// suspend
	admin.MakeRequest(t, bulkRequest(t, "suspend", []string{"5"}, map[string]string{"confirm": "1", "reason": "Spam"}), http.StatusSeeOther)
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5}).ProhibitLogin)

	// unknown actions and people who are no admin are refused
	admin.MakeRequest(t, bulkRequest(t, "delete", []string{"5"}, nil), http.StatusBadRequest)
	loginUser(t, "user2").MakeRequest(t, bulkRequest(t, "suspend", []string{"4"}, map[string]string{"confirm": "1", "reason": "x"}), http.StatusSeeOther)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4}).ProhibitLogin)
}
