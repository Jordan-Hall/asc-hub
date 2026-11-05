// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscBellInitials(t *testing.T) {
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	assert.Equal(t, "c2", ascBellInitials(l, "agent_finished", "Agent coder-2 finished: retry policy"))
	assert.Equal(t, "re", ascBellInitials(l, "agent_question", "Agent reviewer: asks a question"))
	assert.Equal(t, "ch", ascBellInitials(l, "checks_failed", "Checks failed on Wire into the client"))
	assert.Equal(t, "me", ascBellInitials(l, "merge_queue", "Merged your stack of 3"))
	assert.Equal(t, "ag", ascBellInitials(l, "agent_finished", "A run finished"), "no agent name: the name of the event")
}

func TestAscBellMerge(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	hub := []AscBellItem{{Text: "h1", When: at.Add(-time.Hour)}, {Text: "h2", When: at.Add(-3 * time.Hour)}}
	events := []AscBellItem{{Text: "e1", When: at}, {Text: "e2", When: at.Add(-2 * time.Hour)}}
	var texts []string
	for _, it := range ascBellMerge(hub, events) {
		texts = append(texts, it.Text)
	}
	assert.Equal(t, []string{"e1", "h1", "e2", "h2"}, texts, "newest first")

	var many []AscBellItem
	for i := range 10 {
		many = append(many, AscBellItem{Text: strconv.Itoa(i), When: at.Add(-time.Duration(i) * time.Minute)})
	}
	assert.Len(t, ascBellMerge(many, nil), ascBellMax)
}

func TestAscBellLists(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ctx := t.Context()
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 2, RepoID: 1, Event: "agent_finished", Title: "Agent coder-2 finished run 214", Detail: "4 changes are ready for your review", Path: "/user2/repo1/pulls/2"}))
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 4, RepoID: 1, Event: "merge_queue", Title: "Another person's"}))

	rctx, body := ascRenderContext(t, "/-/asc/bell", 2)
	AscBell(rctx)
	require.Equal(t, http.StatusOK, rctx.Resp.Status())
	assert.Equal(t, "private, no-store", rctx.Resp.Header().Get("Cache-Control"))
	html := string(body())
	assert.Contains(t, html, "data-asc-bell-content")
	assert.Contains(t, html, "Agent coder-2 finished run 214")
	assert.Contains(t, html, "4 changes are ready for your review")
	assert.Contains(t, html, `ds-av-agent" aria-hidden="true">c2<`)
	assert.Contains(t, html, "user2/repo1 #4", "a notification of the hub names its repository and number")
	assert.Contains(t, html, "Mark all read")
	assert.Contains(t, html, "Open inbox")
	assert.NotContains(t, html, "Another person&#39;s")
	assert.NotContains(t, html, "Another person's")

	unread, err := db.Count[activities_model.Notification](ctx, activities_model.FindNotificationOptions{UserID: 2, Status: []activities_model.NotificationStatus{activities_model.NotificationStatusUnread}})
	require.NoError(t, err)
	assert.Equal(t, unread+AscUnreadEvents(ctx, 2), ascBellUnread(ctx, 2))
	assert.Contains(t, html, `data-asc-bell-count>`+strconv.FormatInt(ascBellUnread(ctx, 2), 10)+`<`)
}

func TestAscBellHidesUnreadableRepositories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ctx := t.Context()
	// repository 2 is private to user 2; user 4 was told about it once and cannot read it
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 4, RepoID: 2, Event: "checks_failed", Title: "Secret checks"}))
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	for _, it := range ascBellEvents(ctx, l, doer) {
		assert.NotEqual(t, "Secret checks", it.Text)
	}
}

func TestAscBellReadAll(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ctx := t.Context()
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 2, RepoID: 1, Event: "merge_queue", Title: "Merged"}))
	require.Positive(t, ascBellUnread(ctx, 2))

	rctx, _ := ascRenderContext(t, "/-/asc/bell/read", 2)
	AscBellReadPost(rctx)
	assert.Equal(t, http.StatusNoContent, rctx.Resp.Status())
	assert.Zero(t, ascBellUnread(ctx, 2), "the hub and asc both read")

	rctx, body := ascRenderContext(t, "/-/asc/bell", 2)
	AscBell(rctx)
	html := string(body())
	assert.NotContains(t, html, "Mark all read", "nothing left to mark")
	assert.NotContains(t, html, "is-unread")
}

func TestAscBellOpen(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ctx := t.Context()
	require.NoError(t, user_model.RecordAscEvent(ctx, &user_model.AscNotifyLog{UserID: 2, RepoID: 1, Event: "checks_failed", Title: "Checks failed", Path: "/user2/repo1/pulls/2"}))
	rows, err := user_model.RecentAscNotifications(ctx, 2, 1)
	require.NoError(t, err)
	id := strconv.FormatInt(rows[0].ID, 10)

	rctx, _ := ascRenderContext(t, "/-/asc/bell/open/"+id, 4)
	rctx.SetParams("id", id)
	AscBellOpen(rctx)
	assert.Equal(t, http.StatusNotFound, rctx.Resp.Status(), "another person cannot open it")
	n, err := user_model.CountUnreadAscNotifications(ctx, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "and it stays unread")

	rctx, _ = ascRenderContext(t, "/-/asc/bell/open/"+id, 2)
	rctx.SetParams("id", id)
	AscBellOpen(rctx)
	assert.Equal(t, http.StatusSeeOther, rctx.Resp.Status())
	assert.Equal(t, setting.AppSubURL+"/user2/repo1/pulls/2", rctx.Resp.Header().Get("Location"))
	n, err = user_model.CountUnreadAscNotifications(ctx, 2)
	require.NoError(t, err)
	assert.Zero(t, n, "opening it reads it")
}

func TestAscBellOff(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	rctx, _ := ascRenderContext(t, "/-/asc/bell", 2)
	AscBell(rctx)
	assert.Equal(t, http.StatusNotFound, rctx.Resp.Status())
	assert.Zero(t, AscUnreadEvents(t.Context(), 2))
}

func TestAscBellHeader(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascRenderSeed(t)
	ascRenderEntries(t)
	rctx, page := ascRenderContext(t, "/", 2)
	Dashboard(rctx)
	html := string(page())
	assert.Contains(t, html, `data-ds-open="ds-bell" aria-haspopup="dialog" aria-controls="ds-bell"`)
	assert.Contains(t, html, `id="ds-bell"`)
	assert.Contains(t, html, `data-asc-bell="/-/asc/bell"`)
}
