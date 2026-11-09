// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryLandingOf(t *testing.T) {
	assert.Nil(t, historyLandingOf(nil))
	assert.Nil(t, historyLandingOf([]int{0, 0, 0}), "nothing landed")

	l := historyLandingOf([]int{0, 3, 15, 19})
	require.NotNil(t, l)
	assert.Equal(t, 4, l.Weeks)
	assert.Equal(t, 37, l.Total)
	assert.Equal(t, 19, l.ThisWeek)
	assert.Equal(t, 4, l.Delta)
	assert.Zero(t, l.Fewer)
	require.Len(t, l.Bars, 4)
	assert.Equal(t, "1.0", l.Bars[0].H, "an empty week is a hairline")
	assert.Equal(t, "31.0", l.Bars[3].H, "the busiest week is full height")
	assert.Equal(t, "1.0", l.Bars[3].Y)
	assert.Equal(t, "78.8", l.Bars[3].X)
	assert.Equal(t, "17.5", l.Bars[3].W)
	assert.True(t, l.Bars[3].Current)
	assert.False(t, l.Bars[2].Current)

	l = historyLandingOf([]int{5, 2})
	assert.Equal(t, -3, l.Delta)
	assert.Equal(t, 3, l.Fewer)
	assert.Equal(t, "12.4", l.Bars[1].H)
}

func TestHistoryLandingTimes(t *testing.T) {
	assert.Equal(t, []int64{1700000000, 1700000600}, historyLandingTimes("1700000000\nnot a time\n\n1700000600\n"))
	assert.Empty(t, historyLandingTimes(""))
}
