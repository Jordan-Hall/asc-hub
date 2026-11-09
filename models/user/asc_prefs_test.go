// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscDensity(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	assert.Equal(t, user_model.AscDensityComfy, user_model.AscDensity(ctx, 2), "comfy until chosen")
	require.NoError(t, user_model.SetAscDensity(ctx, 2, user_model.AscDensityCompact))
	assert.Equal(t, user_model.AscDensityCompact, user_model.AscDensity(ctx, 2))
	assert.Equal(t, user_model.AscDensityComfy, user_model.AscDensity(ctx, 4), "one person's choice only")

	require.ErrorIs(t, user_model.SetAscDensity(ctx, 2, "huge"), user_model.ErrAscDensity)
	assert.Equal(t, user_model.AscDensityCompact, user_model.AscDensity(ctx, 2), "a refused value changes nothing")

	require.NoError(t, user_model.SetUserSetting(ctx, 2, user_model.SettingsKeyAscDensity, "huge"))
	assert.Equal(t, user_model.AscDensityComfy, user_model.AscDensity(ctx, 2), "a stored value it does not know reads as comfy")
}

func TestAscTimeFormat(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	clock, order := user_model.AscTimeFormat(ctx, 2)
	assert.Equal(t, user_model.AscClock24, clock, "24 hours until chosen")
	assert.Equal(t, user_model.AscDateDMY, order, "day first until chosen")
	require.NoError(t, user_model.SetAscTimeFormat(ctx, 2, user_model.AscClock12, user_model.AscDateMDY))
	clock, order = user_model.AscTimeFormat(ctx, 2)
	assert.Equal(t, user_model.AscClock12, clock)
	assert.Equal(t, user_model.AscDateMDY, order)
	clock, _ = user_model.AscTimeFormat(ctx, 4)
	assert.Equal(t, user_model.AscClock24, clock, "one person's choice only")

	for _, bad := range [][2]string{{"13", "dmy"}, {"24", "ydm"}, {"", ""}} {
		require.ErrorIs(t, user_model.SetAscTimeFormat(ctx, 2, bad[0], bad[1]), user_model.ErrAscTimeFormat)
	}
	clock, order = user_model.AscTimeFormat(ctx, 2)
	assert.Equal(t, [2]string{"12", "mdy"}, [2]string{clock, order}, "a refused value changes nothing")

	require.NoError(t, user_model.SetUserSetting(ctx, 2, user_model.SettingsKeyAscClock, "36"))
	clock, _ = user_model.AscTimeFormat(ctx, 2)
	assert.Equal(t, user_model.AscClock24, clock, "a stored value it does not know reads as the default")
}
