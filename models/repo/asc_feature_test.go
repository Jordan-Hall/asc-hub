// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscFeatureSwitches(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	for _, f := range repo_model.AscFeatures {
		assert.True(t, f.Valid())
		on, err := repo_model.AscFeatureEnabled(ctx, 1, f)
		require.NoError(t, err)
		assert.True(t, on, "%s is on until it is turned off", f)
	}
	assert.False(t, repo_model.AscFeature("stacks").Valid(), "stacks cannot be switched")

	require.NoError(t, repo_model.SetAscFeature(ctx, 1, repo_model.AscFeatureDeps, false))
	require.NoError(t, repo_model.SetAscFeature(ctx, 1, repo_model.AscFeatureDeps, false), "turning it off twice is no error")
	on, err := repo_model.AscFeatureEnabled(ctx, 1, repo_model.AscFeatureDeps)
	require.NoError(t, err)
	assert.False(t, on)
	other, err := repo_model.AscFeatureEnabled(ctx, 2, repo_model.AscFeatureDeps)
	require.NoError(t, err)
	assert.True(t, other, "a switch belongs to one repository")

	off, err := repo_model.AscFeaturesOff(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, map[repo_model.AscFeature]bool{repo_model.AscFeatureDeps: true}, off)

	require.NoError(t, repo_model.SetAscFeature(ctx, 1, repo_model.AscFeatureDeps, true))
	require.NoError(t, repo_model.SetAscFeature(ctx, 1, repo_model.AscFeatureDeps, true))
	off, err = repo_model.AscFeaturesOff(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, off)
}
