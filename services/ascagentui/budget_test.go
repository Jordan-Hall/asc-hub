// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUSD(t *testing.T) {
	for in, want := range map[string]uint64{
		"":          0,
		"0":         0,
		"2.5":       2_500_000,
		"2.50":      2_500_000,
		"0.000001":  1,
		"12":        12_000_000,
		"100000":    100_000_000_000,
		" 1.25 ":    1_250_000,
		"99999.999": 99_999_999_000,
	} {
		got, ok := ParseUSD(in)
		require.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"-1", "1.2.3", "1,5", "$1", "1e3", "abc", ".5", "5.", "0.0000001", "100000.01", "1000000", "1 000", "１２"} {
		_, ok := ParseUSD(bad)
		assert.False(t, ok, bad)
	}
}

func TestParseBudget(t *testing.T) {
	limits, err := ParseBudget(BudgetInput{Changes: "8", Tokens: "200000", CostUSD: "5", Paths: "40", Minutes: "20"})
	require.NoError(t, err)
	assert.EqualValues(t, 8, limits.GetMaxChanges())
	assert.EqualValues(t, 200_000, limits.GetMaxTokens())
	assert.EqualValues(t, 5_000_000, limits.GetMaxCostMicroUsd())
	assert.EqualValues(t, 40, limits.GetMaxPaths())
	assert.EqualValues(t, 1200, limits.GetMaxRuntimeSeconds())

	// Empty means the policy default, which is 0 on the wire: the hub invents no limit.
	limits, err = ParseBudget(BudgetInput{})
	require.NoError(t, err)
	assert.Zero(t, limits.GetMaxChanges()+uint32(limits.GetMaxTokens())+uint32(limits.GetMaxCostMicroUsd())+limits.GetMaxPaths()+limits.GetMaxRuntimeSeconds())

	for field, in := range map[string]BudgetInput{
		BudgetFields.Changes: {Changes: "10001"},
		BudgetFields.Tokens:  {Tokens: "-5"},
		BudgetFields.CostUSD: {CostUSD: "two"},
		BudgetFields.Paths:   {Paths: "1.5"},
		BudgetFields.Minutes: {Minutes: "10081"},
	} {
		_, err := ParseBudget(in)
		var be *BudgetError
		require.ErrorAs(t, err, &be, field)
		assert.Equal(t, field, be.Field)
	}
	_, err = ParseBudget(BudgetInput{Tokens: "99999999999999999999"})
	assert.Error(t, err, "more digits than fit")
	_, err = ParseBudget(BudgetInput{Changes: "+5"})
	assert.Error(t, err)
}

func TestMeters(t *testing.T) {
	limits := &ascv1.AgentBudgetLimits{MaxChanges: 8, MaxTokens: 200_000, MaxRuntimeSeconds: 1200}
	usage := &ascv1.AgentBudgetUsage{Changes: 4, Tokens: 116_000, CostMicroUsd: 1_250_000, RuntimeSeconds: 360}
	byKey := map[string]Meter{}
	for _, m := range Meters(limits, usage) {
		byKey[m.Key] = m
	}
	assert.Equal(t, Meter{Key: "tokens", Label: "Tokens", Used: "116k", Limit: "200k", Percent: 58}, byKey["tokens"])
	assert.Equal(t, Meter{Key: "changes", Label: "Changes", Used: "4", Limit: "8", Percent: 50}, byKey["changes"])
	assert.Equal(t, Meter{Key: "time", Label: "Time", Used: "6 min", Limit: "20 min", Percent: 30}, byKey["time"])
	assert.True(t, byKey["cost"].Unlimited, "no cost limit set")
	assert.Equal(t, "$1.25", byKey["cost"].Used)
	assert.False(t, byKey["cost"].Idle, "it has use")
	assert.True(t, byKey["paths"].Idle, "no limit and no use")

	// Warn at 80, over at the limit and beyond; never more than 100 percent.
	m := Meters(&ascv1.AgentBudgetLimits{MaxChanges: 10}, &ascv1.AgentBudgetUsage{Changes: 8})[2]
	assert.True(t, m.Warn)
	assert.False(t, m.Over)
	m = Meters(&ascv1.AgentBudgetLimits{MaxChanges: 10}, &ascv1.AgentBudgetUsage{Changes: 25})[2]
	assert.True(t, m.Over)
	assert.Equal(t, 100, m.Percent)
	m = Meters(&ascv1.AgentBudgetLimits{MaxChanges: 10}, &ascv1.AgentBudgetUsage{Changes: 10})[2]
	assert.True(t, m.Over, "the limit itself is reached")

	// Nil in, no panic.
	assert.Len(t, Meters(nil, nil), 5)
}

func TestFormatters(t *testing.T) {
	for in, want := range map[uint64]string{0: "0", 9999: "9999", 10_000: "10k", 116_000: "116k", 1_500_000: "1.5M", 2_000_000_000: "2G"} {
		assert.Equal(t, want, FormatCount(in))
	}
	for in, want := range map[uint64]string{0: "$0.00", 1_250_000: "$1.25", 5_000_000: "$5.00", 1: "$0.000001", 123_456: "$0.123456", 100_000_000_000: "$100000.00"} {
		assert.Equal(t, want, FormatUSD(in))
	}
	for in, want := range map[uint64]string{0: "0 s", 45: "45 s", 60: "1 min", 360: "6 min", 3600: "1 h", 7500: "2 h 5 min"} {
		assert.Equal(t, want, FormatDuration(in))
	}
}
