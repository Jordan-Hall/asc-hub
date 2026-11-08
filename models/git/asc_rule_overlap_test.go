// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package git

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscRuleSamples(t *testing.T) {
	for pattern, want := range map[string][]string{
		"main":           {"main"},
		"release/*":      {"release/x"},
		"release/**":     {"release/x"},
		"v?.[0-9]":       {"vx.0"},
		"[!a]x":          {"~x"},
		"{main,dev}":     {"main", "dev"},
		"r/{a,b{c,d}}/*": {"r/a/x", "r/bc/x", "r/bd/x"},
		`a\*b`:           {"a*b"},
		"open[":          {"open["},
	} {
		assert.Equal(t, want, ascRuleSamples(pattern), pattern)
	}
	assert.Len(t, ascRuleSamples("{a,b}{a,b}{a,b}{a,b}{a,b}{a,b}"), ascRuleSampleCap)
}

func TestAscRulesOverlap(t *testing.T) {
	rule := func(p string) *ProtectedBranch { return &ProtectedBranch{RuleName: p} }
	for _, tc := range []struct {
		a, b     string
		branches []string
		want     bool
	}{
		{"main", "main", nil, true},
		{"main", "MAIN", nil, true},
		{"main", "*", nil, true},
		{"release/*", "release/1.0", nil, true},
		{"release/*", "release/**", nil, true},
		{"release/*", "feature/*", nil, false},
		{"{main,dev}", "dev", nil, true},
		{"main", "dev", nil, false},
		{"r*", "*x", nil, true}, // both match rx
		{"a*", "*b", nil, false},
		{"a*", "*b", []string{"acb"}, true},
		{"a*", "*b", []string{"ac", "cb"}, false},
	} {
		assert.Equal(t, tc.want, ascRulesOverlap(rule(tc.a), rule(tc.b), tc.branches), "%s / %s", tc.a, tc.b)
	}
}

func TestAscOverlappingRule(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	rel := &ProtectedBranch{RepoID: 1, RuleName: "release/*"}
	require.NoError(t, db.Insert(ctx, rel))

	other, err := AscOverlappingRule(ctx, 1, 0, "release/2.0")
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Equal(t, "release/*", other.RuleName)

	other, err = AscOverlappingRule(ctx, 1, rel.ID, "release/2.0")
	require.NoError(t, err)
	assert.Nil(t, other, "a rule does not overlap itself")

	other, err = AscOverlappingRule(ctx, 1, 0, "feature/*")
	require.NoError(t, err)
	assert.Nil(t, other)

	// repo1 has the branch branch2 (fixtures): b* and *2 share it, though neither matches a sample of the other
	require.NoError(t, db.Insert(ctx, &ProtectedBranch{RepoID: 1, RuleName: "b*"}))
	other, err = AscOverlappingRule(ctx, 1, 0, "*2")
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Equal(t, "b*", other.RuleName)

	other, err = AscOverlappingRule(ctx, 2, 0, "release/2.0")
	require.NoError(t, err)
	assert.Nil(t, other, "rules of another repository do not count")
}

func TestAscRuleOpenCount(t *testing.T) {
	by := map[string]int64{"main": 2, "release/1": 1, "release/2": 3, "dev": 4}
	assert.EqualValues(t, 4, AscRuleOpenCount("release/*", by))
	assert.EqualValues(t, 2, AscRuleOpenCount("main", by))
	assert.EqualValues(t, 0, AscRuleOpenCount("feature/*", by))
	assert.EqualValues(t, 10, AscRuleOpenCount("**", by))
}
