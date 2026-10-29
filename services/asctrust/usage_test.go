// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func meterOfFor(used, limit uint64) Meter {
	return meterOf(&ascv1.UsageMeter{Used: used, Limit: limit}, "count")
}

func TestFormatUSD(t *testing.T) {
	for in, want := range map[uint64]string{
		0: "$0.00", 1: "$0.0000", 1500: "$0.0015", 9_999: "$0.0099", 10_000: "$0.01", 12_345_678: "$12.35",
		999_999: "$1.00", 1_234_567_890: "$1,234.57",
	} {
		assert.Equal(t, want, FormatUSD(in), "%d", in)
	}
}

func TestParseUSD(t *testing.T) {
	for in, want := range map[string]uint64{"": 0, "12": 12_000_000, "12.5": 12_500_000, "$1,200.25": 1_200_250_000, " 0.000001 ": 1} {
		got, err := ParseUSD(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"-1", "1e3", "1.1234567", "abc", ".5", "99999999999", "10000001", "1.2.3"} {
		_, err := ParseUSD(bad)
		assert.ErrorIs(t, err, ErrBadInput, bad)
	}
}

func TestParseCountAndOwner(t *testing.T) {
	n, err := ParseCount(" 1,000 ", 5000)
	require.NoError(t, err)
	assert.EqualValues(t, 1000, n)
	_, err = ParseCount("5001", 5000)
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = ParseCount("-3", 5000)
	assert.ErrorIs(t, err, ErrBadInput)

	o, err := OwnerOf("", "")
	require.NoError(t, err)
	assert.Equal(t, "", o.GetName())
	o, err = OwnerOf("org", "Acme.Corp")
	require.NoError(t, err)
	assert.Equal(t, "acme.corp", o.GetName())
	for _, c := range [][2]string{{"", "x"}, {"team", "x"}, {"user", ""}, {"user", "-x"}, {"user", "a/b"}, {"user", "a b"}} {
		_, err := OwnerOf(c[0], c[1])
		assert.ErrorIs(t, err, ErrBadInput, "%v", c)
	}
}

func TestMeterPercent(t *testing.T) {
	assert.Equal(t, 25, meterOfFor(50, 200).Pct)
	assert.Equal(t, 100, meterOfFor(300, 200).Pct, "over the limit shows a full bar")
	assert.Equal(t, 0, meterOfFor(300, 0).Pct, "no limit has no bar")
	assert.Equal(t, 100, meterOfFor(^uint64(0)-1, ^uint64(0)).Pct, "no overflow")
	assert.Equal(t, "3.0 GiB", Meter{Used: 3 << 30, kind: "bytes"}.UsedText())
}
