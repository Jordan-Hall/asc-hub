// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadPercent(t *testing.T) {
	assert.Equal(t, hostGauge{Known: true, Percent: 50}, loadPercent("2.00 1.00 0.50 1/300 4242\n", 4))
	assert.Equal(t, hostGauge{Known: true, Percent: 100}, loadPercent("9.5 1 1 1/1 1", 4), "capped")
	assert.Equal(t, hostGauge{Known: true, Percent: 0}, loadPercent("0.00 0 0 0/0 0", 2))
	for _, bad := range []string{"", "x 1 2", "-1 0 0"} {
		assert.False(t, loadPercent(bad, 4).Known, bad)
	}
	assert.False(t, loadPercent("1.0 1 1", 0).Known)
}

func TestDiskPercent(t *testing.T) {
	// 1000 blocks, 250 free, 200 available to a user: used 750 of 950 usable, as df says
	assert.Equal(t, hostGauge{Known: true, Percent: 78}, diskPercent(1000, 250, 200))
	assert.Equal(t, hostGauge{Known: true, Percent: 100}, diskPercent(1000, 0, 0))
	assert.False(t, diskPercent(0, 0, 0).Known)
	assert.False(t, diskPercent(10, 20, 5).Known, "more free than total is not a file system we understand")
}

func TestDiskGaugeOfTheWorkingDirectory(t *testing.T) {
	g := diskGauge(".")
	assert.True(t, g.Known)
	assert.LessOrEqual(t, g.Percent, 100)
	assert.False(t, diskGauge("/definitely/not/here").Known)
}
