// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSparkline(t *testing.T) {
	assert.Equal(t, "", Sparkline(nil, 80, 28))
	assert.Equal(t, "", Sparkline([]uint32{5}, 80, 28), "one value is no line")
	assert.Equal(t, "", Sparkline([]uint32{0, 0, 0}, 80, 28), "nothing happened, nothing is drawn")
	assert.Equal(t, "", Sparkline([]uint32{1, 2}, 0, 28))
	// The largest value is at the top, zero at the bottom, and the points run from the left edge to the right edge.
	assert.Equal(t, "0.0,27.0 40.0,1.0 80.0,27.0", Sparkline([]uint32{0, 4, 0}, 80, 28))
	for _, c := range Sparkline([]uint32{3, 9, 1, 7}, 80, 28) {
		assert.Contains(t, "0123456789., ", string(c))
	}
}
