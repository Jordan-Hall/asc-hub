// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

// These cases are the ones of crates/asc-core/src/policy.rs. The port must agree with the Rust code on every one.

func TestGlobMatchBySegmentIgnoringCase(t *testing.T) {
	yes := [][2]string{
		{"src/**", "src/a/b.rs"},
		{"src/**", "src"}, // ** matches no segment at all
		{"src/**", "SRC/A.RS"},
		{"**", "any/thing/at/all"},
		{"**/test.rs", "test.rs"},
		{"**/test.rs", "a/b/test.rs"},
		{"src/*/mod.rs", "src/x/mod.rs"},
		{"*.md", "README.md"},
		{"a?c", "abc"},
		{"docs/**/img/*.png", "docs/a/b/img/x.png"},
	}
	for _, c := range yes {
		assert.True(t, GlobMatch(c[0], c[1]), "%s should match %s", c[0], c[1])
	}
	no := [][2]string{
		{"src/*/mod.rs", "src/x/y/mod.rs"}, // * stays in one segment
		{"src/*/mod.rs", "src/mod.rs"},
		{"*.md", "docs/README.md"},
		{"a?c", "ac"},
		{"infra/**", "srcinfra/x"},
		{"src", "src/a"},
	}
	for _, c := range no {
		assert.False(t, GlobMatch(c[0], c[1]), "%s should not match %s", c[0], c[1])
	}
}

func TestGlobMatchCostIsBounded(t *testing.T) {
	// Many ** segments against a long path is the shape that makes a naive matcher exponential.
	glob := "**/**/**/**/**/**/**/**/**/**/**/**/x"
	path := "a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/y"
	assert.False(t, GlobMatch(glob, path))
}

func testRules() *Rules {
	return &Rules{
		Roles: map[string][]string{
			"maintainer": {"user:alice", "user:bob"},
			"docs":       {"user:dora", "agent:docs-*"},
		},
		Paths: []PathRule{
			{Glob: "infra/**", Write: []string{"role:maintainer"}},
			{Glob: "docs/**", Write: []string{"role:maintainer", "role:docs"}},
			{Glob: "**/*.lock", Write: []string{"role:maintainer", "agent:*"}},
		},
		AgentScope:  []string{"src/**", "tests/**"},
		AgentDenied: []string{".github/**"},
		MaxChanges:  8,
		MaxPaths:    100,
	}
}

func TestSubjectsMatchByKindAndNameAndRolesExpand(t *testing.T) {
	r := testRules()
	assert.True(t, r.IsMaintainer(Who{Kind: "user", Name: "alice"}))
	assert.True(t, r.IsMaintainer(Who{Kind: "user", Name: "bob"}))
	assert.False(t, r.IsMaintainer(Who{Kind: "user", Name: "carol"}))
	assert.False(t, r.IsMaintainer(Who{Kind: "agent", Name: "alice"}), "the kind has to match")
	assert.True(t, r.IsMember(Who{Kind: "agent", Name: "docs-writer"}, "docs"), "a name glob")
	assert.False(t, r.IsMember(Who{Kind: "agent", Name: "other"}, "docs"))
	assert.False(t, r.IsMaintainer(Who{}), "an unknown kind matches only *")
	assert.True(t, r.subjectMatches(Who{}, "*"))
}

func TestWritesFollowEveryMatchingRule(t *testing.T) {
	r := testRules()
	alice := Who{Kind: "user", Name: "alice"}
	carol := Who{Kind: "user", Name: "carol"}
	dora := Who{Kind: "user", Name: "dora"}

	ok, _ := r.CheckWrite(alice, "infra/main.tf")
	assert.True(t, ok)
	ok, why := r.CheckWrite(carol, "infra/main.tf")
	assert.False(t, ok)
	assert.Contains(t, why, "infra/**", "the reason names the rule")
	ok, _ = r.CheckWrite(dora, "docs/guide.md")
	assert.True(t, ok, "a role member through role:docs")
	ok, _ = r.CheckWrite(dora, "infra/x")
	assert.False(t, ok)
	ok, _ = r.CheckWrite(carol, "src/anything.rs")
	assert.True(t, ok, "a path no rule names is open to anyone who can write the ref")
	// Two rules match: both have to admit.
	ok, _ = r.CheckWrite(carol, "infra/Cargo.lock")
	assert.False(t, ok)
	ok, _ = r.CheckWrite(alice, "infra/Cargo.lock")
	assert.True(t, ok)
}

func TestAgentsKeepToTheirScopeAndOutOfDeniedPaths(t *testing.T) {
	r := testRules()
	agent := Who{Kind: "agent", Name: "bot"}
	ok, _ := r.CheckWrite(agent, "src/a.rs")
	assert.True(t, ok)
	ok, _ = r.CheckWrite(agent, "tests/a.rs")
	assert.True(t, ok)
	ok, why := r.CheckWrite(agent, "scripts/a.sh")
	assert.False(t, ok)
	assert.Contains(t, why, "outside the agent scope")
	ok, why = r.CheckWrite(agent, ".github/workflows/ci.yml")
	assert.False(t, ok)
	assert.Contains(t, why, "may not write")
	// A rule that names agents extends the scope.
	ok, _ = r.CheckWrite(agent, "vendor/x.lock")
	assert.True(t, ok)
	// An agent in no scope list at all is not restricted by one.
	open := &Rules{Roles: map[string][]string{}}
	ok, _ = open.CheckWrite(agent, "anything")
	assert.True(t, ok)
}

func TestThePolicyFileNeedsAMaintainerWhateverTheRulesSay(t *testing.T) {
	r := &Rules{Roles: map[string][]string{"maintainer": {"user:alice"}}}
	carol := Who{Kind: "user", Name: "carol"}
	for _, p := range []string{".asc/policy.toml", ".ASC/Policy.TOML", ".asc", ".asc/policy.toml/x"} {
		ok, why := r.CheckWrite(carol, p)
		assert.False(t, ok, p)
		assert.Contains(t, why, "only by a maintainer")
		ok, _ = r.CheckWrite(Who{Kind: "user", Name: "alice"}, p)
		assert.True(t, ok, p)
	}
	ok, _ := r.CheckWrite(carol, ".asc/other.toml")
	assert.True(t, ok)
}

func TestRunsAreBudgetedForAgentsOnly(t *testing.T) {
	r := testRules()
	agent := Who{Kind: "agent", Name: "bot"}
	user := Who{Kind: "user", Name: "carol"}
	ok, _ := r.CheckRun(agent, 8, 100)
	assert.True(t, ok)
	ok, why := r.CheckRun(agent, 9, 1)
	assert.False(t, ok)
	assert.Contains(t, why, "at most 8 changes")
	ok, why = r.CheckRun(agent, 1, 101)
	assert.False(t, ok)
	assert.Contains(t, why, "at most 100 paths")
	ok, _ = r.CheckRun(user, 1000, 100000)
	assert.True(t, ok, "a person has no budget")
	ok, _ = (&Rules{}).CheckRun(agent, 1000, 1000)
	assert.True(t, ok, "no budget is no limit")
}

func TestRulesOfCopiesTheAnswer(t *testing.T) {
	p := &ascv1.GetPolicyResponse{
		Roles:  []*ascv1.PolicyRole{{Name: "maintainer", Members: []string{"user:a"}}},
		Paths:  []*ascv1.PolicyPathRule{{Glob: "x/**", Write: []string{"role:maintainer"}}},
		Agents: &ascv1.PolicyAgents{DefaultScope: []string{"src/**"}, DeniedPaths: []string{"x/**"}, MaxChangesPerRun: 3, MaxPathsPerRun: 4},
	}
	r := RulesOf(p)
	assert.Equal(t, []string{"user:a"}, r.Roles["maintainer"])
	assert.Equal(t, []PathRule{{Glob: "x/**", Write: []string{"role:maintainer"}}}, r.Paths)
	assert.Equal(t, []string{"src/**"}, r.AgentScope)
	assert.EqualValues(t, 3, r.MaxChanges)
	assert.EqualValues(t, 4, r.MaxPaths)
	assert.NotNil(t, RulesOf(nil).Roles)
}
