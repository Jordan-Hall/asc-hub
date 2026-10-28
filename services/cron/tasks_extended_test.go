// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cron

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GCLFSConfig(t *testing.T) {
	cfg, err := setting.NewConfigProviderFromData(`
[cron.gc_lfs]
ENABLED = true
RUN_AT_START = true
SCHEDULE = "@every 2h"
OLDER_THAN = "1h"
LAST_UPDATED_MORE_THAN_AGO = "7h"
NUMBER_TO_CHECK_PER_REPO = 10
PROPORTION_TO_CHECK_PER_REPO = 0.1
`)
	require.NoError(t, err)
	defer test.MockVariableValue(&setting.CfgProvider, cfg)()

	config := &GCLFSConfig{
		BaseConfig: BaseConfig{
			Enabled:    false,
			RunAtStart: false,
			Schedule:   "@every 24h",
		},
		OlderThan:                24 * time.Hour * 7,
		LastUpdatedMoreThanAgo:   24 * time.Hour * 3,
		NumberToCheckPerRepo:     100,
		ProportionToCheckPerRepo: 0.6,
	}

	_, err = setting.GetCronSettings("gc_lfs", config)
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.True(t, config.RunAtStart)
	assert.Equal(t, "@every 2h", config.Schedule)
	assert.Equal(t, 1*time.Hour, config.OlderThan)
	assert.Equal(t, 7*time.Hour, config.LastUpdatedMoreThanAgo)
	assert.Equal(t, int64(10), config.NumberToCheckPerRepo)
	assert.InDelta(t, 0.1, config.ProportionToCheckPerRepo, 0.001)
}

// The task cannot be registered without the translation of its name (RegisterTask refuses and the hub stops), so the
// key is checked in the locale file; the deletion itself is tested with the model.
func Test_CleanupAscDeviceCodesHasATranslation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "options", "locale", "locale_en-US.ini"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\ndashboard.cleanup_asc_device_codes = ")
}
