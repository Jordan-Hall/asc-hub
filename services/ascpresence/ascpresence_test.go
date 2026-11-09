// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascpresence

import (
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

func TestOnlineFollowsRequestsAndTheChoice(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	clock := time.Unix(1_800_000_000, 0)
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	mu.Lock()
	stored = map[int64]time.Time{}
	mu.Unlock()

	assert.False(t, Online(ctx, u.ID), "never seen")
	Touch(ctx, u)
	assert.True(t, Online(ctx, u.ID))

	// a request a minute later is not stored again (coarse granularity), the person stays online for ten minutes
	clock = clock.Add(time.Minute)
	Touch(ctx, u)
	v, _ := user_model.GetUserSetting(ctx, u.ID, lastSeenKey, "")
	assert.Equal(t, "1800000000", v)
	clock = clock.Add(9 * time.Minute)
	assert.True(t, Online(ctx, u.ID))
	clock = clock.Add(2 * time.Minute)
	assert.False(t, Online(ctx, u.ID), "no request for more than ten minutes")

	// the choice: switched off, the dot never shows, however recent the request
	clock = clock.Add(10 * time.Minute)
	Touch(ctx, u)
	assert.True(t, Online(ctx, u.ID))
	require.NoError(t, SetShowOnline(ctx, u.ID, false))
	assert.False(t, ShowOnline(ctx, u.ID))
	assert.False(t, Online(ctx, u.ID))
	require.NoError(t, SetShowOnline(ctx, u.ID, true))
	assert.True(t, Online(ctx, u.ID))

	// no person, no write
	Touch(ctx, nil)
}

func TestThePillSaysOnlineAndTheLocalTime(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	clock := time.Date(2026, 11, 9, 13, 20, 0, 0, time.UTC)
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = time.Now })
	mu.Lock()
	stored = map[int64]time.Time{}
	mu.Unlock()

	_, ok := Of(ctx, u.ID, "")
	assert.False(t, ok, "offline and no zone: nothing to say")

	_, ok = Of(ctx, u.ID, "Europe/Berlin")
	assert.False(t, ok, "the local time shows only when the person switched it on")
	assert.False(t, ShowLocalTime(ctx, u.ID))
	require.NoError(t, SetShowLocalTime(ctx, u.ID, true))
	assert.True(t, ShowLocalTime(ctx, u.ID))

	p, ok := Of(ctx, u.ID, "Europe/Berlin")
	assert.True(t, ok)
	assert.Equal(t, Presence{LocalTime: "14:20"}, p, "offline, the local time alone")

	Touch(ctx, u)
	p, ok = Of(ctx, u.ID, "Asia/Kolkata")
	assert.True(t, ok)
	assert.Equal(t, Presence{Online: true, LocalTime: "18:50"}, p)

	p, ok = Of(ctx, u.ID, "Not/AZone")
	assert.True(t, ok)
	assert.Equal(t, Presence{Online: true}, p, "a zone the hub cannot load is left out")

	require.NoError(t, SetShowOnline(ctx, u.ID, false))
	_, ok = Of(ctx, u.ID, "Europe/Berlin")
	assert.False(t, ok, "switched off, neither the dot nor the local time shows")
	require.NoError(t, SetShowOnline(ctx, u.ID, true))

	require.NoError(t, SetShowLocalTime(ctx, u.ID, false))
	p, ok = Of(ctx, u.ID, "Europe/Berlin")
	assert.True(t, ok)
	assert.Equal(t, Presence{Online: true}, p, "online, the local time switched off")
}
