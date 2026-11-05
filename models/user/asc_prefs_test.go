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
