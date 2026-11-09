// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/contexttest"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postSwitch(t *testing.T, section, event string, on bool) int {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /user/settings/notifications")
	contexttest.LoadUser(t, ctx, 2)
	value := []string{"0"}
	if on {
		value = append(value, "1")
	}
	ctx.Req.Form = url.Values{"section": {section}, "event": {event}, "value": value}
	ctx.Req.PostForm = ctx.Req.Form
	NotificationsPost(ctx)
	return resp.Code
}

func matrixRows(t *testing.T) *goquery.Selection {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user/settings/notifications", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
	contexttest.LoadUser(t, ctx, 2)
	Notifications(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.Body.String()))
	require.NoError(t, err)
	return doc.Find("table.wk-matrix").First().Find("tbody tr")
}

// The event by channel matrix: a row per event, the inbox locked on, and email and push switched per event.
func TestNotificationMatrix(t *testing.T) {
	unittest.PrepareTestEnv(t)
	rows := matrixRows(t)
	require.Equal(t, len(ascnotify.Events), rows.Length())
	rows.Each(func(_ int, row *goquery.Selection) {
		inbox := row.Find(`td:nth-of-type(1) input[type="checkbox"]`)
		assert.True(t, inbox.Is("[checked][disabled]"), "the inbox keeps every event")
		assert.True(t, row.Find(`td:nth-of-type(2) input[name="value"][value="1"]`).Is("[checked]"), "email is on by default")
		assert.True(t, row.Find(`td:nth-of-type(3) input[name="value"][value="1"]`).Is("[checked]"), "push is on by default")
	})

	require.Equal(t, http.StatusSeeOther, postSwitch(t, "email_event", "restack_conflict", false))
	require.Equal(t, http.StatusSeeOther, postSwitch(t, "push_event", "agent_question", false))
	prefs := ascnotify.Load(t.Context(), 2)
	assert.False(t, prefs.EmailOn("restack_conflict"))
	assert.True(t, prefs.PushOn("restack_conflict"))
	assert.False(t, prefs.PushOn("agent_question"))
	assert.True(t, prefs.EmailOn("agent_question"))

	rows = matrixRows(t)
	byEvent := map[string]*goquery.Selection{}
	rows.Each(func(_ int, row *goquery.Selection) {
		byEvent[row.Find(`input[name="event"]`).First().AttrOr("value", "")] = row
	})
	assert.False(t, byEvent["restack_conflict"].Find(`td:nth-of-type(2) input[value="1"]`).Is("[checked]"))
	assert.False(t, byEvent["agent_question"].Find(`td:nth-of-type(3) input[value="1"]`).Is("[checked]"))

	require.Equal(t, http.StatusSeeOther, postSwitch(t, "email_event", "restack_conflict", true))
	assert.True(t, ascnotify.Load(t.Context(), 2).EmailOn("restack_conflict"), "switching back on")
	assert.Equal(t, http.StatusBadRequest, postSwitch(t, "email_event", "no_such_event", false))
	assert.Equal(t, http.StatusBadRequest, postSwitch(t, "push_event", "", false))
}
