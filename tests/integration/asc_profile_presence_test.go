// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/ascpresence"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The profile head says whether the person is online and the time where they are, as they allow.
func TestTheProfileShowsAPresencePill(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	ctx := t.Context()
	// user4 is seen now and lives in Tokyo; user5 looks at the profile
	require.NoError(t, user_model.SetUserSetting(ctx, 4, "asc.last_seen", strconv.FormatInt(time.Now().Unix(), 10)))
	require.NoError(t, ascnotify.Save(ctx, 4, ascnotify.Prefs{TZ: "Asia/Tokyo"}))
	owner := loginUser(t, "user4")
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/privacy", map[string]string{"setting": "show_local_time", "value": "1"}), http.StatusSeeOther)
	require.True(t, ascpresence.ShowLocalTime(ctx, 4))
	viewer := loginUser(t, "user5")
	pill := func(t *testing.T, s *TestSession) (string, string, int) {
		t.Helper()
		req := NewRequest(t, "GET", "/user4")
		var doc *HTMLDoc
		if s == nil {
			doc = NewHTMLParser(t, MakeRequest(t, req, http.StatusOK).Body)
		} else {
			doc = NewHTMLParser(t, s.MakeRequest(t, req, http.StatusOK).Body)
		}
		p := doc.Find(`.wk-pf-presence`)
		return p.AttrOr("data-asc-presence", ""), p.Text(), p.Length()
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)

	t.Run("Online with the local time", func(t *testing.T) {
		state, text, n := pill(t, viewer)
		require.Equal(t, 1, n)
		assert.Equal(t, "online", state)
		assert.Regexp(t, `^Online, local time \d\d:\d\d$`, text)
		got, err := time.ParseInLocation("15:04", text[len(text)-5:], tokyo)
		require.NoError(t, err)
		local := time.Now().In(tokyo)
		minutes := func(t time.Time) int { return t.Hour()*60 + t.Minute() }
		assert.LessOrEqual(t, (minutes(local)-minutes(got)+1440)%1440, 1, "the time is the time in Tokyo")
		assert.Equal(t, 1, NewHTMLParser(t, viewer.MakeRequest(t, NewRequest(t, "GET", "/user4"), http.StatusOK).Body).Find(`.wk-pf-online`).Length())
	})

	t.Run("A visitor sees it too", func(t *testing.T) {
		_, _, n := pill(t, nil)
		assert.Equal(t, 1, n)
	})

	t.Run("Offline, the local time alone", func(t *testing.T) {
		require.NoError(t, user_model.SetUserSetting(ctx, 4, "asc.last_seen", strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)))
		state, text, n := pill(t, viewer)
		require.Equal(t, 1, n)
		assert.Equal(t, "away", state)
		assert.Regexp(t, `^Local time \d\d:\d\d$`, text)
	})

	t.Run("Without the local time switch, an offline person has no pill", func(t *testing.T) {
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/privacy", map[string]string{"setting": "show_local_time", "value": "0"}), http.StatusSeeOther)
		_, _, n := pill(t, viewer)
		assert.Equal(t, 0, n)
		doc := NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/user/settings/privacy"), http.StatusOK).Body)
		sw := doc.Find(`form:has(input[name="setting"][value="show_local_time"]) input[type="checkbox"]`)
		require.Equal(t, 1, sw.Length())
		_, checked := sw.Attr("checked")
		assert.False(t, checked)
		require.NoError(t, ascpresence.SetShowLocalTime(ctx, 4, true))
	})

	t.Run("Nothing shows when the person hides presence or activity", func(t *testing.T) {
		require.NoError(t, ascpresence.SetShowOnline(ctx, 4, false))
		_, _, n := pill(t, viewer)
		assert.Equal(t, 0, n)
		require.NoError(t, ascpresence.SetShowOnline(ctx, 4, true))

		_, err := db.GetEngine(ctx).Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 4", true)
		require.NoError(t, err)
		_, _, n = pill(t, viewer)
		assert.Equal(t, 0, n)
	})
}
