// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscBellPopover checks the notifications popover of the header bell (board Shell): the header opens it, it lists
// the notifications of the hub and the events asc reported, an event opens where it points and is read, and Mark all
// read empties the count.
func TestAscBellPopover(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	session := loginUser(t, "user2")
	ev := &user_model.AscNotifyLog{UserID: 2, RepoID: 1, Event: "agent_finished", Title: "Agent coder-2 finished: tidy the parser", Path: "/user2/repo1/pulls/2"}
	require.NoError(t, user_model.RecordAscEvent(db.DefaultContext, ev))

	t.Run("Header", func(t *testing.T) {
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		page.AssertElement(t, `a[data-ds-open="ds-bell"][aria-controls="ds-bell"]`, true)
		pop := page.Find("#ds-bell[data-asc-bell]")
		require.Equal(t, 1, pop.Length())
		src, _ := pop.Attr("data-asc-bell")
		assert.Equal(t, "/-/asc/bell", src)
	})

	t.Run("List", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequest(t, "GET", "/-/asc/bell"), http.StatusOK)
		assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
		page := NewHTMLParser(t, resp.Body)
		items := page.Find("[data-asc-bell-content] a.ds-bell-it")
		assert.Positive(t, items.Length())
		assert.Contains(t, items.First().Text(), "Agent coder-2 finished: tidy the parser")
		assert.Equal(t, "c2", strings.TrimSpace(items.First().Find(".ds-av-agent").Text()))
		href, _ := items.First().Attr("href")
		assert.Equal(t, "/-/asc/bell/open/"+strconv.FormatInt(ev.ID, 10), href)
		page.AssertElement(t, "[data-asc-bell-count]", true)
		page.AssertElement(t, `button[data-asc-bell-read="/-/asc/bell/read"]`, true)
		page.AssertElement(t, `a[href="/-/asc/inbox"]`, true)
	})

	t.Run("OpenIsTheirsOnly", func(t *testing.T) {
		loginUser(t, "user5").MakeRequest(t, NewRequest(t, "GET", "/-/asc/bell/open/"+strconv.FormatInt(ev.ID, 10)), http.StatusNotFound)
		resp := session.MakeRequest(t, NewRequest(t, "GET", "/-/asc/bell/open/"+strconv.FormatInt(ev.ID, 10)), http.StatusSeeOther)
		assert.Equal(t, "/user2/repo1/pulls/2", resp.Header().Get("Location"))
		row, err := user_model.GetAscNotification(db.DefaultContext, 2, ev.ID)
		require.NoError(t, err)
		assert.NotZero(t, row.ReadUnix)
	})

	t.Run("ReadAll", func(t *testing.T) {
		session.MakeRequest(t, NewRequest(t, "POST", "/-/asc/bell/read"), http.StatusNoContent)
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/asc/bell"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-bell-count]", false)
		page.AssertElement(t, "a.ds-bell-it.is-unread", false)
		n, err := user_model.CountUnreadAscNotifications(db.DefaultContext, 2)
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("SignedOut", func(t *testing.T) {
		MakeRequest(t, NewRequest(t, "GET", "/-/asc/bell"), http.StatusSeeOther)
	})
}
