// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The UserSettingsAccount board notes: Make primary sits in a menu on a verified row; an unverified address has
// Send activation and a direct Delete; adding an address says so; Delete asks in a dialog; the settings menu is the
// only navigation.
func TestAscSettingsAccountBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	alt := &user_model.EmailAddress{UID: 2, Email: "user2-alt@example.com", LowerEmail: "user2-alt@example.com", IsActivated: true}
	require.NoError(t, db.Insert(db.DefaultContext, alt))
	sess := loginUser(t, "user2")
	row := func(doc *HTMLDoc, email string) *goquery.Selection {
		return doc.Find(".wk-rows .wk-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return s.Find(".ds-li-title .ds-break").Text() == email
		})
	}
	get := func() *HTMLDoc {
		return NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/account"), http.StatusOK).Body)
	}

	doc := get()
	primary := row(doc, "user2@example.com")
	require.Equal(t, 1, primary.Length())
	assert.Contains(t, primary.Text(), "Primary")
	assert.Zero(t, primary.Find(`[data-testid="email-menu"]`).Length(), "the primary address has no menu")
	assert.Zero(t, primary.Find(".delete-button").Length())

	verified := row(doc, alt.Email)
	menu := verified.Find(`details[data-testid="email-menu"]`)
	require.Equal(t, 1, menu.Length(), "a verified address has a menu")
	assert.Equal(t, "More actions for "+alt.Email, menu.Find("summary").AttrOr("aria-label", ""))
	assert.Equal(t, "Make primary", menu.Find(`[data-testid="email-make-primary"]`).Text())
	assert.Equal(t, strconv.FormatInt(alt.ID, 10), menu.Find(`input[name="id"]`).AttrOr("value", ""))
	assert.Equal(t, "delete-email", menu.Find(".delete-button").AttrOr("data-modal-id", ""))

	unverified := row(doc, "user2-2@example.com")
	require.Equal(t, 1, unverified.Length())
	assert.Contains(t, unverified.Text(), "Requires activation")
	assert.Equal(t, "SENDACTIVATION", unverified.Find(`input[name="_method"]`).AttrOr("value", ""))
	assert.Zero(t, unverified.Find(`[data-testid="email-menu"]`).Length(), "an unverified address cannot be primary")
	assert.Equal(t, 1, unverified.Find("button.ds-btn-danger.delete-button").Length())

	assert.Equal(t, 1, doc.Find("#delete-email").Length(), "Delete asks in a dialog")
	assert.Equal(t, 1, doc.Find("nav#settings-menu").Length())
	assert.Zero(t, doc.Find(".ds-settings-body .ds-tabs, .ds-settings-body .tabular").Length(), "no tabs repeat the settings menu")

	t.Run("Make primary from the menu", func(t *testing.T) {
		sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/account/email", map[string]string{"_method": "PRIMARY", "id": strconv.FormatInt(alt.ID, 10)}), http.StatusSeeOther)
		assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.EmailAddress{ID: alt.ID}).IsPrimary)
		doc := get()
		assert.Contains(t, row(doc, alt.Email).Text(), "Primary")
		assert.Equal(t, 1, row(doc, "user2@example.com").Find(`[data-testid="email-menu"]`).Length(), "the old primary, verified, gets the menu")
	})

	t.Run("adding an address says so", func(t *testing.T) {
		sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/account/email", map[string]string{"email": "user2-new@example.com"}), http.StatusSeeOther)
		doc := get()
		assert.Equal(t, "The new email address has been added.", strings.TrimSpace(doc.Find(".flash-success").Text()))
		assert.Equal(t, 1, row(doc, "user2-new@example.com").Length())
	})

	t.Run("other settings pages have no tabs either", func(t *testing.T) {
		for _, p := range []string{"/user/settings", "/user/settings/security", "/user/settings/appearance", "/user/settings/privacy", "/user/settings/repos", "/user/settings/keys", "/user/settings/applications", "/user/settings/organization", "/user/settings/notifications", "/user/settings/hooks"} {
			resp := sess.MakeRequest(t, NewRequest(t, "GET", p), NoExpectedStatus)
			require.Equal(t, http.StatusOK, resp.Code, p+" -> "+resp.Header().Get("Location"))
			doc := NewHTMLParser(t, resp.Body)
			assert.Equal(t, 1, doc.Find("nav#settings-menu").Length(), p)
			assert.Zero(t, doc.Find(".ds-settings-body .ds-tabs, .ds-settings-body .tabular").Length(), p)
		}
	})
}
