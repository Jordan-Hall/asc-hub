// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascnotify_test

import (
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/services/ascnotify"

	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

func TestQuietHoursCrossMidnightInTheTimeZoneOfThePerson(t *testing.T) {
	p := ascnotify.Prefs{Quiet: ascnotify.QuietHours{On: true, From: "20:00", Until: "07:30"}, TZ: "Europe/Lisbon"}
	at := func(h, m int) time.Time { return time.Date(2026, 1, 15, h, m, 0, 0, time.UTC) } // Lisbon is UTC+0 in winter
	assert.True(t, p.InQuietHours(at(22, 0)))
	assert.True(t, p.InQuietHours(at(3, 0)))
	assert.True(t, p.InQuietHours(at(7, 29)))
	assert.False(t, p.InQuietHours(at(7, 30)))
	assert.False(t, p.InQuietHours(at(12, 0)))

	// the same instant is a different hour in Tokyo (UTC+9): 12:00 UTC is 21:00 there, inside the window
	p.TZ = "Asia/Tokyo"
	assert.True(t, p.InQuietHours(at(12, 0)))
	// quiet hours off, or an empty window, never hold anything back
	p.Quiet.On = false
	assert.False(t, p.InQuietHours(at(22, 0)))
	assert.False(t, ascnotify.Prefs{Quiet: ascnotify.QuietHours{On: true, From: "08:00", Until: "08:00"}}.InQuietHours(at(8, 0)))
}

func TestValidate(t *testing.T) {
	require.NoError(t, ascnotify.Prefs{}.Validate())
	require.NoError(t, ascnotify.Prefs{EmailOff: []string{"mentioned"}, TZ: "Europe/London", Quiet: ascnotify.QuietHours{On: true, From: "20:00", Until: "07:30"}}.Validate())
	require.NoError(t, ascnotify.Prefs{EmailOff: []string{"agent_question"}}.Validate())
	require.Error(t, ascnotify.Prefs{EmailOff: []string{"nope"}}.Validate(), "an event the page does not have cannot be switched")
	require.Error(t, ascnotify.Prefs{TZ: "Mars/Olympus"}.Validate())
	require.Error(t, ascnotify.Prefs{Quiet: ascnotify.QuietHours{On: true, From: "25:00", Until: "07:30"}}.Validate())
}

func TestEmailAllowedFollowsTheStoredChoice(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	noon := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	night := time.Date(2026, 1, 15, 23, 0, 0, 0, time.UTC)

	// the default: every email event on
	assert.True(t, ascnotify.EmailAllowed(ctx, 2, ascnotify.EventMentioned, noon))

	require.NoError(t, ascnotify.Save(ctx, 2, ascnotify.Prefs{EmailOff: []string{ascnotify.EventMentioned}, Quiet: ascnotify.QuietHours{On: true, From: "20:00", Until: "07:30"}}))
	assert.False(t, ascnotify.EmailAllowed(ctx, 2, ascnotify.EventMentioned, noon), "switched off")
	assert.True(t, ascnotify.EmailAllowed(ctx, 2, ascnotify.EventReviewRequested, noon), "another event is untouched")
	assert.False(t, ascnotify.EmailAllowed(ctx, 2, ascnotify.EventReviewRequested, night), "quiet hours hold every email")
	assert.False(t, ascnotify.EmailAllowed(ctx, 2, "", night), "also a mail that is not an event of the page")
	assert.True(t, ascnotify.EmailAllowed(ctx, 4, ascnotify.EventMentioned, night), "another person is not affected")

	require.Error(t, ascnotify.Save(ctx, 2, ascnotify.Prefs{TZ: "nope"}))
}
