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
