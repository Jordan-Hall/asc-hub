// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postQuiet(t *testing.T, away string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /user/settings/notifications")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Form = url.Values{"section": {"quiet"}, "on": {"0"}, "from": {"20:00"}, "until": {"07:30"}, "tz": {""}, "away": {away}}
	ctx.Req.PostForm = ctx.Req.Form
	NotificationsPost(ctx)
	require.Equal(t, http.StatusSeeOther, resp.Code)
}

func TestAwayUntilIsSavedWithTheQuietHoursAndShownWhileItLasts(t *testing.T) {
	unittest.PrepareTestEnv(t)
	day := time.Now().UTC().AddDate(0, 0, 3).Format(time.DateOnly)
	postQuiet(t, day)
	assert.Equal(t, day, ascnotify.Load(t.Context(), 2).Away)

	ctx, resp := contexttest.MockContext(t, "/user/settings/notifications", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
	contexttest.LoadUser(t, ctx, 2)
	Notifications(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), `name="away" value="`+day+`"`)

	postQuiet(t, "Thursday")
	assert.Equal(t, day, ascnotify.Load(t.Context(), 2).Away, "a value that is not a date is refused and changes nothing")

	postQuiet(t, "")
	assert.Empty(t, ascnotify.Load(t.Context(), 2).Away, "an empty date clears it")

	past := time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly)
	postQuiet(t, past)
	ctx, resp = contexttest.MockContext(t, "/user/settings/notifications", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
	contexttest.LoadUser(t, ctx, 2)
	Notifications(ctx)
	assert.Contains(t, resp.Body.String(), `name="away" value=""`, "a day that is over is not shown")
}
