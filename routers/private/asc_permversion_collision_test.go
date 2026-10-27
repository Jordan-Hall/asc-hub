// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No text of a ref or a scope can pass for another field: the fingerprint hashes an encoding in which every field is
// named and every string is quoted. Each case below must have a version of its own.
func TestAscPermVersionHasNoCollisions(t *testing.T) {
	base := permFacts{Repo: permRepo{ID: 1, Name: "repo1"}, Units: []string{"1=1"}, Ref: "refs/heads/a", User: &permUser{ID: 1, Active: true}, Owner: &permOwner{Type: 1}}
	v := func(f permFacts) uint64 {
		t.Helper()
		n, err := permVersionOf(f)
		require.NoError(t, err)
		return n
	}
	with := func(change func(f *permFacts)) permFacts {
		f := base
		change(&f)
		return f
	}
	cases := []struct {
		name   string
		change func(f *permFacts)
	}{
		{"a ref that spells a rule", func(f *permFacts) { f.Ref = "refs/heads/a|rule=1@2|signed=true" }},
		{"the rule it spells", func(f *permFacts) { f.Rule = &permRule{ID: 1, Updated: 2, Signed: true} }},
		{"a ref that spells a field", func(f *permFacts) {
			f.Ref = `refs/heads/a","rule":{"ID":1,"Updated":2,"Signed":true},"x":"`
		}},
		{"a ref with a quote", func(f *permFacts) { f.Ref = `refs/heads/a"` }},
		{"a ref with a newline", func(f *permFacts) { f.Ref = "refs/heads/a\nrule" }},
		{"an empty ref", func(f *permFacts) { f.Ref = "" }},
		{"a scope that spells the grants", func(f *permFacts) { f.Cred.Scope = "read:repository|grants=write:user" }},
		{"the grants it spells", func(f *permFacts) { f.Cred.Scope = "read:repository"; f.Cred.Grants = "write:user" }},
		{"scope and grants swapped", func(f *permFacts) { f.Cred.Scope = "write:user"; f.Cred.Grants = "read:repository" }},
		{"a scope with a separator", func(f *permFacts) { f.Cred.Scope = "a,b|c" }},
		{"a scope without it", func(f *permFacts) { f.Cred.Scope = "a,b" }},
		{"a user that is absent", func(f *permFacts) { f.User = nil }},
		{"a user with zero values", func(f *permFacts) { f.User = &permUser{} }},
		{"a rule with zero values", func(f *permFacts) { f.Rule = &permRule{} }},
		{"a task with zero values", func(f *permFacts) { f.Task = &permTask{} }},
		{"a key with zero values", func(f *permFacts) { f.Key = &permKey{} }},
		{"an owner that is absent", func(f *permFacts) { f.Owner = nil }},
		{"an owner with zero values", func(f *permFacts) { f.Owner = &permOwner{} }},
		{"units that are empty", func(f *permFacts) { f.Units = []string{} }},
		{"units that are one text", func(f *permFacts) { f.Units = []string{"1=1,2=1"} }},
		{"units that are two", func(f *permFacts) { f.Units = []string{"1=1", "2=1"} }},
		{"a name that spells a path", func(f *permFacts) { f.Repo.OwnerName = "o"; f.Repo.Name = "x/y" }},
		{"the same split elsewhere", func(f *permFacts) { f.Repo.OwnerName = "o/x"; f.Repo.Name = "y" }},
	}
	seen := map[uint64]string{v(base): "the base"}
	for _, c := range cases {
		got := v(with(c.change))
		if other, dup := seen[got]; dup {
			t.Errorf("%q has the version of %q", c.name, other)
		}
		seen[got] = c.name
	}
	assert.Equal(t, v(base), v(base), "the same facts have the same version")
}
