// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	git_model "forgejo.org/models/git"
	api "forgejo.org/modules/structs"

	"github.com/stretchr/testify/assert"
)

func TestRelCategoryOf(t *testing.T) {
	cases := map[string]string{
		"Fix refund timeout":              relFixed,
		"fix(ledger): off by one":         relFixed,
		"Add idempotency keys":            relAdded,
		"feat: export to CSV":             relAdded,
		"Retry every call with backoff":   relChanged,
		"Bump tokio to 1.41":              relChanged,
		"":                                relChanged,
		"fixture cleanup":                 relChanged, // a word that merely starts with "fix"
		"Support for a new region":        relAdded,
		"refactor(core): split the crate": relChanged,
	}
	for in, want := range cases {
		assert.Equal(t, want, relCategoryOf(in), in)
	}
}

func TestParseRelLogAndGroups(t *testing.T) {
	out := "aaa\x00Priya\x00p@x.org\x00Fix timeout\n" +
		"bbb\x00dep-bot\x00bot@x.org\x00Bump tokio\n" +
		"broken line\n" +
		"ccc\x00Diego\x00d@x.org\x00Add replay test\n" +
		"\x00x\x00y\x00no sha\n"
	changes := parseRelLog(out)
	if !assert.Len(t, changes, 3) {
		return
	}
	groups := groupRelChanges(changes)
	var names []string
	for _, g := range groups {
		names = append(names, g.Category)
	}
	assert.Equal(t, []string{relAdded, relChanged, relFixed}, names, "groups are in the order of the notes")
	assert.Equal(t, "ccc", groups[0].Changes[0].Sha)
	assert.Empty(t, groupRelChanges(nil), "a release without changes has no groups")
}

func TestRelChecks(t *testing.T) {
	pass, total := relChecks([]*git_model.CommitStatus{{State: api.CommitStatusSuccess}, {State: api.CommitStatusSuccess}, {State: api.CommitStatusFailure}})
	assert.Equal(t, 2, pass)
	assert.Equal(t, 3, total)
	pass, total = relChecks(nil)
	assert.Zero(t, pass)
	assert.Zero(t, total)
}
