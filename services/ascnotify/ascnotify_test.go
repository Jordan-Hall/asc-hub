// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascnotify_test

import (
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
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

func TestPushChoiceAndDigestHour(t *testing.T) {
	p := ascnotify.Prefs{PushOff: []string{"checks_failed"}}
	require.NoError(t, p.Validate())
	assert.False(t, p.PushOn("checks_failed"))
	assert.True(t, p.PushOn("merge_queue"))
	require.Error(t, ascnotify.Prefs{PushOff: []string{"nope"}}.Validate())

	at := time.Date(2026, 10, 28, 7, 30, 0, 0, time.UTC) // 08:30 in London (BST ended on 25 October: 07:30 UTC is 07:30)
	assert.False(t, ascnotify.Prefs{}.DigestDue(time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)), "no summary unless asked for")
	d := ascnotify.Prefs{Digest: true}
	assert.True(t, d.DigestDue(time.Date(2026, 10, 28, 8, 5, 0, 0, time.UTC)))
	assert.False(t, d.DigestDue(at))
	d.TZ = "Asia/Tokyo" // 08:00 in Tokyo is 23:00 UTC the day before
	assert.True(t, d.DigestDue(time.Date(2026, 10, 27, 23, 10, 0, 0, time.UTC)))
	assert.False(t, d.DigestDue(time.Date(2026, 10, 28, 8, 10, 0, 0, time.UTC)))
}

func TestTimeZoneNameIsTheZoneOfTheProfile(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	assert.Empty(t, ascnotify.TimeZoneName(ctx, 2), "none chosen")
	require.NoError(t, ascnotify.Save(ctx, 2, ascnotify.Prefs{TZ: "Asia/Tokyo"}))
	assert.Equal(t, "Asia/Tokyo", ascnotify.TimeZoneName(ctx, 2))
	assert.Empty(t, ascnotify.TimeZoneName(ctx, 4), "another person's choice is theirs")
	// a name stored by an older hub that this one cannot load is not handed to the page
	require.NoError(t, user_model.SetUserSetting(ctx, 4, "asc.notify", `{"tz":"Not/AZone"}`))
	assert.Empty(t, ascnotify.TimeZoneName(ctx, 4))
}
