// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strconv"
	"strings"
)

// Sparkline turns a series into the points of an SVG polyline that fits a w by h box, one point to a value, oldest first. The
// scale runs from zero to the largest value, so a quiet series stays low. It returns "" for fewer than two values or a series
// of zeros only: there is nothing to draw. The text holds digits, dots, commas and spaces only, so it is safe in an attribute.
func Sparkline(values []uint32, w, h int) string {
	if len(values) < 2 || w <= 0 || h <= 0 {
		return ""
	}
	var peak uint32
	for _, v := range values {
		peak = max(peak, v)
	}
	if peak == 0 {
		return ""
	}
	var b strings.Builder
	step := float64(w) / float64(len(values)-1)
	for i, v := range values {
		if i > 0 {
			b.WriteByte(' ')
		}
		x := float64(i) * step
		y := float64(h) - float64(v)/float64(peak)*float64(h-2) - 1
		b.WriteString(strconv.FormatFloat(x, 'f', 1, 64))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(y, 'f', 1, 64))
	}
	return b.String()
}
