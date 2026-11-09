// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// On a phone the Inbox tab of the bottom navigation opens the notifications sheet; it stays a link to the inbox
// without script, on the inbox itself and for a visitor.
func TestThePhoneInboxTabOpensTheNotificationsSheet(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	tab := func(t *testing.T, s *TestSession, path string) *HTMLDoc {
		t.Helper()
		req := NewRequest(t, "GET", path)
		if s == nil {
			return NewHTMLParser(t, MakeRequest(t, req, http.StatusOK).Body)
		}
		return NewHTMLParser(t, s.MakeRequest(t, req, http.StatusOK).Body)
	}
	s := loginUser(t, "user2")

	t.Run("A signed-in page", func(t *testing.T) {
		doc := tab(t, s, "/explore/repos")
		inbox := doc.Find(`nav.ds-bnav a[data-testid="bnav-inbox-sheet"]`)
		require.Equal(t, 1, inbox.Length())
		assert.Equal(t, "ds-bell", inbox.AttrOr("data-ds-open", ""))
		assert.Equal(t, "ds-bell", inbox.AttrOr("aria-controls", ""))
		assert.Equal(t, "dialog", inbox.AttrOr("aria-haspopup", ""))
		assert.Equal(t, "false", inbox.AttrOr("aria-expanded", ""))
		assert.NotEmpty(t, inbox.AttrOr("href", ""), "without script the tab is a link to the inbox")
		sheet := doc.Find(`#ds-bell[role="dialog"]`)
		require.Equal(t, 1, sheet.Length())
		assert.Equal(t, "/-/asc/bell", sheet.AttrOr("data-asc-bell", ""))
	})

	t.Run("The sheet ends with Open inbox", func(t *testing.T) {
		doc := tab(t, s, "/-/asc/bell")
		assert.Equal(t, 1, doc.Find(`.ds-bell-foot a[href="/-/asc/inbox"]`).Length())
	})

	t.Run("On the inbox the tab is the current page, not a sheet", func(t *testing.T) {
		doc := tab(t, s, "/-/asc/inbox")
		inbox := doc.Find(`nav.ds-bnav a[aria-current="page"]`)
		require.Equal(t, 1, inbox.Length())
		_, sheet := inbox.Attr("data-ds-open")
		assert.False(t, sheet)
	})

	t.Run("A visitor has no sheet", func(t *testing.T) {
		doc := tab(t, nil, "/explore/repos")
		assert.Equal(t, 0, doc.Find(`[data-testid="bnav-inbox-sheet"]`).Length())
		assert.Equal(t, 0, doc.Find(`#ds-bell`).Length())
	})
}
