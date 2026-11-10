// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package cron

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A new install checks no outside host for releases: the checker is off and names no source.
func TestUpdateCheckerDefaults(t *testing.T) {
	c := updateCheckerDefaults()
	assert.False(t, c.IsEnabled())
	assert.False(t, c.DoRunAtStart())
	assert.Empty(t, c.HTTPEndpoint)
	assert.Empty(t, c.DomainEndpoint)
	assert.Equal(t, "@every 168h", c.Schedule)
}
