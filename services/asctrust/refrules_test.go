// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const refRulesSample = `version = 1

[trunk]
require_approvals = 1

# release lines
[refs."release/*"]
land = ["maintainers", "queue", "nobody"]
signed = true

[refs."refs/tags/v*"]
land = ["release-key"]
delete = true
`

func TestRefRulesOfText(t *testing.T) {
	rules := RefRulesOfText(refRulesSample)
	require.Len(t, rules, 2)
	assert.Equal(t, RefRule{Pattern: "release/*", Land: []string{LandQueue, LandMaintainers}, Signed: true}, rules[0], "land in page order, unknown words dropped")
	assert.False(t, rules[0].IsTags())
	assert.Equal(t, "release/*", rules[0].Name())
	assert.True(t, rules[1].IsTags())
	assert.Equal(t, "v*", rules[1].Name())
	assert.True(t, rules[1].Lets(LandReleaseKey))
	assert.False(t, rules[1].Lets(LandQueue))
	assert.True(t, rules[1].Delete)
	assert.Empty(t, RefRulesOfText("version = 1\n[trunk]\nrequire_approvals = 1\n"))
}

func TestApplyRefRuleAdds(t *testing.T) {
	next, err := ApplyRefRule(refRulesSample, "", RefRule{Pattern: "hotfix/**", Land: []string{LandAgents, LandQueue}, Force: true})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(next, strings.TrimSuffix(refRulesSample, "\n")), "every other byte stays")
	rules := RefRulesOfText(next)
	require.Len(t, rules, 3)
	assert.Equal(t, RefRule{Pattern: "hotfix/**", Land: []string{LandQueue, LandAgents}, Force: true}, rules[2])
	assert.NotContains(t, next[strings.Index(next, `[refs."hotfix/**"]`):], "signed", "a switch that is off is not written")
}

func TestApplyRefRuleEdits(t *testing.T) {
	next, err := ApplyRefRule(refRulesSample, "release/*", RefRule{Pattern: "release/*", Land: []string{LandQueue}})
	require.NoError(t, err)
	rules := RefRulesOfText(next)
	require.Len(t, rules, 2)
	assert.Equal(t, RefRule{Pattern: "release/*", Land: []string{LandQueue}}, rules[0])
	assert.NotContains(t, next, "signed = true", "a switch turned off is taken out")
	assert.Contains(t, next, "# release lines")
	assert.Contains(t, next, "[trunk]\nrequire_approvals = 1")
}

func TestApplyRefRuleMoves(t *testing.T) {
	next, err := ApplyRefRule(refRulesSample, "release/*", RefRule{Pattern: "stable/*", Land: []string{LandMaintainers}, Signed: true})
	require.NoError(t, err)
	assert.NotContains(t, next, `"release/*"`)
	rules := RefRulesOfText(next)
	require.Len(t, rules, 2)
	assert.Equal(t, "refs/tags/v*", rules[0].Pattern)
	assert.Equal(t, RefRule{Pattern: "stable/*", Land: []string{LandMaintainers}, Signed: true}, rules[1])
}

func TestApplyRefRuleRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		was  string
		rule RefRule
		want error
	}{
		"exists":      {"", RefRule{Pattern: "release/*"}, ErrRefRuleExists},
		"moved onto":  {"release/*", RefRule{Pattern: "refs/tags/v*"}, ErrRefRuleExists},
		"missing":     {"gone/*", RefRule{Pattern: "gone/*"}, ErrNoRefRule},
		"bad word":    {"", RefRule{Pattern: "x/*", Land: []string{"everyone"}}, ErrBadInput},
		"bad pattern": {"", RefRule{Pattern: "refs/heads/main"}, ErrBadInput},
		"empty":       {"", RefRule{Pattern: ""}, ErrBadInput},
		"empty tag":   {"", RefRule{Pattern: "refs/tags/"}, ErrBadInput},
	} {
		_, err := ApplyRefRule(refRulesSample, c.was, c.rule)
		require.ErrorIs(t, err, c.want, name)
	}
}

func TestApplyRefRuleLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("version = 1\n")
	for i := range MaxRefRules {
		fmt.Fprintf(&b, "[refs.\"r%d/*\"]\nland = [\"queue\"]\n", i)
	}
	text := b.String()
	_, err := ApplyRefRule(text, "", RefRule{Pattern: "one-more/*"})
	require.ErrorIs(t, err, ErrBadInput)
	_, err = ApplyRefRule(text, "r0/*", RefRule{Pattern: "r0/*", Signed: true})
	require.NoError(t, err, "an edit is not a new rule")
}

func TestValidateRefPattern(t *testing.T) {
	for _, ok := range []string{"release/*", "release/**", "refs/tags/v*", "hotfix-1.2", "a/b/c"} {
		require.NoError(t, ValidateRefPattern(ok), ok)
	}
	for _, bad := range []string{
		"", "refs/tags/", "refs/heads/main", "refs/tags/refs/x", "/lead", "trail/", "a//b", "x.lock", "a..b", "a@{b",
		"a b", "a~b", "a^b", "a:b", "a?b", "a[b", `a\b`, `a"b`, "a\tb", "a\x7fb", strings.Repeat("a", MaxGlobLen+1),
	} {
		require.ErrorIs(t, ValidateRefPattern(bad), ErrBadInput, bad)
	}
}

func TestRemoveRefRule(t *testing.T) {
	next, err := RemoveRefRule(refRulesSample, "refs/tags/v*")
	require.NoError(t, err)
	rules := RefRulesOfText(next)
	require.Len(t, rules, 1)
	assert.Equal(t, "release/*", rules[0].Pattern)
	assert.NotContains(t, next, "release-key")
	_, err = RemoveRefRule(refRulesSample, "nope/*")
	require.ErrorIs(t, err, ErrNoRefRule)
}
