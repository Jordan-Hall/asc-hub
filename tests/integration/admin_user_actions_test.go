// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	system_model "forgejo.org/models/system"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The page of a person in the administration (board AdminUser): its tabs, and Reset 2FA and Suspend, which ask for a reason.
func TestAdminUserTabs(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	admin := loginUser(t, "user1")
	for _, tab := range []string{"", "overview", "emails", "keys", "sessions", "activity", "nonsense"} {
		resp := admin.MakeRequest(t, NewRequest(t, "GET", "/admin/users/2?tab="+tab), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		assert.Equal(t, 5, doc.doc.Find(".adm-person-tabs .ds-tab").Length(), "the five tabs on %q", tab)
		assert.Equal(t, 1, doc.doc.Find(`.adm-person-tabs .ds-tab[aria-current="page"]`).Length(), "one tab is current on %q", tab)
	}
	// each tab shows its own part
	body := func(tab string) string {
		return admin.MakeRequest(t, NewRequest(t, "GET", "/admin/users/2?tab="+tab), http.StatusOK).Body.String()
	}
	assert.Contains(t, body("overview"), `id="adm-acct"`)
	assert.NotContains(t, body("overview"), `id="adm-mails"`)
	assert.Contains(t, body("emails"), `id="adm-mails"`)
	assert.Contains(t, body("keys"), `id="adm-ssh"`)
	assert.Contains(t, body("keys"), `id="adm-gpg"`)
	assert.Contains(t, body("sessions"), "asc serve does not keep sessions", "without asc the tab says so")
	assert.Contains(t, body("nonsense"), `id="adm-acct"`, "an unknown tab is the first")
	// not for everybody
	loginUser(t, "user4").MakeRequest(t, NewRequest(t, "GET", "/admin/users/2"), http.StatusForbidden)
}

func TestAdminUserResetAndSuspendWantAReason(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	admin := loginUser(t, "user1")
	notices := func() int64 { return system_model.CountNotices(db.DefaultContext) }
	post := func(path string, values map[string]string, status int) {
		admin.MakeRequest(t, NewRequestWithValues(t, "POST", path, values), status)
	}

	// Suspend: not without a reason, not for yourself
	before := notices()
	post("/admin/users/2/suspend", map[string]string{"reason": "  "}, http.StatusSeeOther)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).ProhibitLogin)
	post("/admin/users/1/suspend", map[string]string{"reason": "because"}, http.StatusSeeOther)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1}).ProhibitLogin, "an administrator does not suspend themselves")
	assert.Equal(t, before, notices(), "nothing was done, nothing is recorded")

	post("/admin/users/2/suspend", map[string]string{"reason": "Account used for spam"}, http.StatusSeeOther)
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).ProhibitLogin)
	assert.Equal(t, before+1, notices(), "the reason is kept as a notice")
	list, err := system_model.Notices(db.DefaultContext, 1, 10)
	require.NoError(t, err)
	var found bool
	for _, n := range list {
		found = found || strings.Contains(n.Description, "Account used for spam")
	}
	assert.True(t, found, "the notice carries the reason")

	// the page offers the way back
	body := admin.MakeRequest(t, NewRequest(t, "GET", "/admin/users/2"), http.StatusOK).Body.String()
	assert.Contains(t, body, "/admin/users/2/unsuspend")
	post("/admin/users/2/unsuspend", map[string]string{"reason": "Cleared by the owner"}, http.StatusSeeOther)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).ProhibitLogin)

	// Reset 2FA: user24 has a second factor
	has, err := auth_model.HasTwoFactorByUID(db.DefaultContext, 24)
	require.NoError(t, err)
	require.True(t, has)
	post("/admin/users/24/reset_2fa", map[string]string{"reason": ""}, http.StatusSeeOther)
	has, _ = auth_model.HasTwoFactorByUID(db.DefaultContext, 24)
	assert.True(t, has, "without a reason nothing is reset")
	post("/admin/users/24/reset_2fa", map[string]string{"reason": "Lost the phone, identity checked by video"}, http.StatusSeeOther)
	has, _ = auth_model.HasTwoFactorByUID(db.DefaultContext, 24)
	assert.False(t, has)

	// only an administrator
	loginUser(t, "user4").MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/users/2/suspend", map[string]string{"reason": "x"}), http.StatusForbidden)
}
