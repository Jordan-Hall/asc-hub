// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const samplePolicy = `version = 1  # the schema

[roles]
maintainer = ["user:alice", "user:bob"]

[trunk]
require_approvals = 1
require_checks = [
  "unit",   # the fast ones
  "lint",
]
allow_force = false

[paths."infra/**"]
write = ["role:maintainer"]

[signing]
require_signed = false # off for now

[agents.budgets]
max_changes_per_run = 8
max_tokens = 200_000
`

func TestTomlReading(t *testing.T) {
	v, ok := TomlBool(samplePolicy, "signing", "require_signed")
	assert.True(t, ok)
	assert.False(t, v, "a trailing comment is not part of the value")

	v, ok = TomlBool(samplePolicy, "trunk", "allow_force")
	assert.True(t, ok)
	assert.False(t, v)

	_, ok = TomlBool(samplePolicy, "secrets", "block_on_push")
	assert.False(t, ok, "a key of a section that is not there")
	_, ok = TomlBool(samplePolicy, "trunk", "require_approvals")
	assert.False(t, ok, "a number is not a boolean")

	n, ok := TomlInt(samplePolicy, "trunk", "require_approvals")
	assert.True(t, ok)
	assert.EqualValues(t, 1, n)
	n, ok = TomlInt(samplePolicy, "agents.budgets", "max_tokens")
	assert.True(t, ok)
	assert.EqualValues(t, 200000, n, "underscores are digit separators")
	n, ok = TomlInt(samplePolicy, "", "version")
	assert.True(t, ok)
	assert.EqualValues(t, 1, n, "top level keys are in the section \"\"")

	list, ok := TomlStrings(samplePolicy, "trunk", "require_checks")
	assert.True(t, ok)
	assert.Equal(t, []string{"unit", "lint"}, list, "a multi-line array with comments")
	list, ok = TomlStrings(samplePolicy, "roles", "maintainer")
	assert.True(t, ok)
	assert.Equal(t, []string{"user:alice", "user:bob"}, list)
	list, ok = TomlStrings(`[a]
k = ['x', "y\"z", 'w']
`, "a", "k")
	assert.True(t, ok)
	assert.Equal(t, []string{"x", `y"z`, "w"}, list, "literal and escaped basic strings")

	_, ok = TomlStrings(samplePolicy, "trunk", "allow_force")
	assert.False(t, ok, "a boolean is not a list")
}

func TestTomlSectionNamesAreNormalised(t *testing.T) {
	text := "[ signing ]\nrequire_signed = true\n"
	v, ok := TomlBool(text, "signing", "require_signed")
	assert.True(t, ok)
	assert.True(t, v)
	// A table with a quoted key is a section of its own and is not mistaken for a key of another one.
	v, ok = TomlBool(samplePolicy, "paths.\"infra/**\"", "write")
	assert.False(t, ok)
	assert.False(t, v)
}

func TestTomlSetReplacesAKeyAndKeepsTheRest(t *testing.T) {
	out := TomlSetBool(samplePolicy, "signing", "require_signed", true)
	v, ok := TomlBool(out, "signing", "require_signed")
	assert.True(t, ok)
	assert.True(t, v)
	assert.Contains(t, out, "# the schema", "comments elsewhere stay")
	assert.Contains(t, out, "[paths.\"infra/**\"]")
	assert.Equal(t, strings.Count(samplePolicy, "\n"), strings.Count(out, "\n"), "one line replaced by one line")
	assert.NotContains(t, out, "off for now", "the replaced line goes with its comment")
}

func TestTomlSetReplacesAMultiLineArrayWhole(t *testing.T) {
	out := TomlSetStrings(samplePolicy, "trunk", "require_checks", []string{"build"})
	list, ok := TomlStrings(out, "trunk", "require_checks")
	assert.True(t, ok)
	assert.Equal(t, []string{"build"}, list)
	assert.NotContains(t, out, "the fast ones")
	assert.NotContains(t, out, `"lint"`)
	assert.Contains(t, out, "allow_force = false", "the next key is untouched")
	v, ok := TomlBool(out, "trunk", "allow_force")
	assert.True(t, ok)
	assert.False(t, v)
}

func TestTomlSetAddsAKeyToAnExistingSection(t *testing.T) {
	out := TomlSetBool(samplePolicy, "trunk", "require_signed_x", true)
	v, ok := TomlBool(out, "trunk", "require_signed_x")
	assert.True(t, ok)
	assert.True(t, v)
	// It goes inside [trunk], before the blank line and the next table.
	trunk := strings.Index(out, "[trunk]")
	paths := strings.Index(out, "[paths.")
	added := strings.Index(out, "require_signed_x")
	assert.Greater(t, added, trunk)
	assert.Less(t, added, paths)
}

func TestTomlSetAddsASection(t *testing.T) {
	out := TomlSetBool("version = 1\n", "secrets", "block_on_push", false)
	assert.Equal(t, "version = 1\n\n[secrets]\nblock_on_push = false\n", out)
	v, ok := TomlBool(out, "secrets", "block_on_push")
	assert.True(t, ok)
	assert.False(t, v)

	out = TomlSetBool("", "ci", "allow_quarantine", true)
	assert.Equal(t, "[ci]\nallow_quarantine = true\n", out)
}

func TestTomlSetATopLevelKeyGoesBeforeTheFirstTable(t *testing.T) {
	out := TomlSetBool("[roles]\nmaintainer = [\"user:a\"]\n", "", "flag", true)
	assert.True(t, strings.HasPrefix(out, "flag = true\n[roles]"), out)
	v, ok := TomlBool(out, "", "flag")
	assert.True(t, ok)
	assert.True(t, v)
}

func TestTomlSetIsStableAndIdempotent(t *testing.T) {
	once := TomlSetBool(samplePolicy, "secrets", "block_on_push", false)
	twice := TomlSetBool(once, "secrets", "block_on_push", false)
	assert.Equal(t, once, twice)
	flipped := TomlSetBool(twice, "secrets", "block_on_push", true)
	v, ok := TomlBool(flipped, "secrets", "block_on_push")
	assert.True(t, ok)
	assert.True(t, v)
	assert.Equal(t, 1, strings.Count(flipped, "block_on_push"), "the key is replaced, never repeated")
}

func TestTomlSetNormalisesWindowsLineBreaks(t *testing.T) {
	out := TomlSetBool("[signing]\r\nrequire_signed = false\r\n", "signing", "require_signed", true)
	assert.NotContains(t, out, "\r")
	v, _ := TomlBool(out, "signing", "require_signed")
	assert.True(t, v)
}

func TestTomlQuoteCannotBreakOutOfAString(t *testing.T) {
	evil := "a\"]\n[evil]\nx = \"1"
	out := TomlSetStrings("[secrets]\n", "secrets", "allow_paths", []string{evil})
	got, ok := TomlStrings(out, "secrets", "allow_paths")
	require.True(t, ok)
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "evil", "the value survives as one string")
	// The text has exactly one table: nothing was injected.
	d := parseToml(out)
	assert.Len(t, d.headers, 1, out)
	assert.Equal(t, "secrets", d.headers[0].name)

	assert.Equal(t, `"a\u0007b"`, tomlQuote("a\x07b"))
	assert.Equal(t, `"tab\there"`, tomlQuote("tab\there"))
	assert.Equal(t, `"back\\slash"`, tomlQuote(`back\slash`))
}

func TestValidatePathGlob(t *testing.T) {
	good := []string{"src/**", "**", "tests/fixtures/*.pem", "a/b?c/d", "docs/**/x.md", ".github/**"}
	for _, g := range good {
		assert.NoError(t, ValidatePathGlob(g), g)
	}
	bad := []string{"", "/abs", "trailing/", "a//b", "a/./b", "a/../b", `back\slash`, "a/b**c", "with\x00nul", "line\nbreak", strings.Repeat("a", 257)}
	for _, g := range bad {
		err := ValidatePathGlob(g)
		require.Error(t, err, g)
		assert.ErrorIs(t, err, ErrBadInput)
	}
}

func TestParseGlobLines(t *testing.T) {
	got, err := ParseGlobLines("src/**\r\n\r\n  tests/**  \nsrc/**\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"src/**", "tests/**"}, got, "blank lines and repeats are dropped")

	got, err = ParseGlobLines("")
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = ParseGlobLines("ok/**\n../escape")
	assert.ErrorIs(t, err, ErrBadInput)

	many := strings.Repeat("a/**\n", 1)
	for i := 0; i < MaxGlobs+1; i++ {
		many += "p" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + "/**\n"
	}
	_, err = ParseGlobLines(many)
	assert.ErrorIs(t, err, ErrBadInput, "more than 64 patterns")

	_, err = ParseGlobLines(strings.Repeat("x", MaxGlobs*(MaxGlobLen+2)+1))
	assert.ErrorIs(t, err, ErrBadInput, "a list that is too long is refused before it is split")
}
