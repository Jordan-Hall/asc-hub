// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The UserSettingsPrivacy board notes: a switch saves at once and offers Undo; Block asks in a dialog and a name that
// does not exist says so; Unblock asks nothing and offers Undo for ten seconds; with nobody blocked the list says so.
func TestAscSettingsPrivacyBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	sess := loginUser(t, "user2")
	follow := func(resp *httptest.ResponseRecorder) *HTMLDoc {
		t.Helper()
		loc := resp.Header().Get("Location")
		require.NotEmpty(t, loc)
		return NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", loc), http.StatusOK).Body)
	}

	// nobody blocked: the empty state, the block form opens the confirm dialog
	doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/privacy"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`#sp-blocked ~ * .ds-empty, section[aria-labelledby="sp-blocked"] .ds-empty`).Length(), "the empty blocked list")
	modal, _ := doc.Find(`form[action="/user/settings/privacy/block"]`).Last().Attr("data-confirm-modal")
	assert.Equal(t, "#block-user-modal", modal)
	assert.Equal(t, 1, doc.Find(`[data-testid="block-user-modal"]`).Length())
	assert.Zero(t, doc.Find(`[data-testid="undo-switch"], [data-testid="undo-unblock"]`).Length())

	// a switch saves at once and offers to put it back
	resp := sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/privacy", map[string]string{"setting": "hide_email", "value": "1"}), http.StatusSeeOther)
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).KeepEmailPrivate)
	doc = follow(resp)
	undo := doc.Find(`[data-testid="undo-switch"]`)
	require.Equal(t, 1, undo.Length())
	assert.Equal(t, "10000", undo.AttrOr("data-undo-ms", ""))
	assert.Contains(t, undo.Text(), "is on.")
	assert.Equal(t, "hide_email", undo.Find(`input[name="setting"]`).AttrOr("value", ""))
	vals := undo.Find(`input[name="value"]`).Map(func(_ int, s *goquery.Selection) string { return s.AttrOr("value", "") })
	assert.Equal(t, []string{"0"}, vals, "Undo turns the switch off again")
	sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/privacy", map[string]string{"setting": "hide_email", "value": "0"}), http.StatusSeeOther)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}).KeepEmailPrivate)

	// a name that does not exist says so
	resp = sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/privacy/block", map[string]string{"username": "nobody-here-zz"}), http.StatusSeeOther)
	doc = follow(resp)
	assert.Contains(t, strings.TrimSpace(doc.Find(".flash-error").Text()), "nobody-here-zz")

	// block, then Unblock asks nothing and offers Undo
	sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/privacy/block", map[string]string{"username": "user4"}), http.StatusSeeOther)
	require.True(t, user_model.IsBlocked(t.Context(), 2, 4))
	doc = NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/user/settings/privacy"), http.StatusOK).Body)
	unblock := doc.Find(`form[action="/user/settings/blocked_users/unblock"]`)
	require.Equal(t, 1, unblock.Length())
	_, confirms := unblock.Attr("data-confirm-modal")
	assert.False(t, confirms, "Unblock asks no questions")
	assert.Zero(t, unblock.Find(".delete-button").Length())
	resp = sess.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/blocked_users/unblock", map[string]string{"user_id": strconv.Itoa(4)}), http.StatusSeeOther)
	assert.False(t, user_model.IsBlocked(t.Context(), 2, 4))
	doc = follow(resp)
	again := doc.Find(`[data-testid="undo-unblock"]`)
	require.Equal(t, 1, again.Length())
	assert.Equal(t, "user4", again.Find(`input[name="username"]`).AttrOr("value", ""))
}
