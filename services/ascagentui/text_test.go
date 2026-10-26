// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Hidden characters, written as escapes so the source holds none of them.
var (
	rlo     = string(rune(0x202E))
	isolate = string(rune(0x2066))
	zwsp    = string(rune(0x200B))
	bom     = string(rune(0xFEFF))
)

func TestValidators(t *testing.T) {
	t.Run("agent names", func(t *testing.T) {
		for _, ok := range []string{"coder", "a", "reviewer-2", "0day", strings.Repeat("a", 40)} {
			assert.True(t, ValidAgentName(ok), ok)
		}
		for _, bad := range []string{"", "-lead", "Coder", "a b", "a_b", "a/b", "a\n", strings.Repeat("a", 41), "агент"} {
			assert.False(t, ValidAgentName(bad), bad)
		}
	})
	t.Run("token and key ids", func(t *testing.T) {
		assert.True(t, ValidTokenID("at_"+strings.Repeat("A", 20)))
		assert.False(t, ValidTokenID("at_"+strings.Repeat("A", 19)))
		assert.False(t, ValidTokenID("at_"+strings.Repeat("A", 20)+"\n"))
		assert.False(t, ValidTokenID("ak_"+strings.Repeat("A", 20)))
		assert.True(t, ValidKeyID("ak_"+strings.Repeat("z", 20)))
		assert.False(t, ValidKeyID("ak_../"+strings.Repeat("z", 15)))
	})
	t.Run("models", func(t *testing.T) {
		for _, ok := range []string{"claude-sonnet-5-5", "gpt-5.1", "models/gemini-2.5-pro", "llama3:8b", "org/model@v1"} {
			assert.True(t, ValidModel(ok), ok)
		}
		for _, bad := range []string{"", "-x", " m", "m m", "m;rm", "m\n", strings.Repeat("m", 129)} {
			assert.False(t, ValidModel(bad), bad)
		}
	})
	t.Run("workflow names keep runs for the history page", func(t *testing.T) {
		assert.True(t, ValidWorkflowName("review"))
		assert.True(t, ValidWorkflowName("nightly_scan-2"))
		assert.False(t, ValidWorkflowName("runs"))
		assert.False(t, ValidWorkflowName("Review"))
		assert.False(t, ValidWorkflowName("../x"))
		assert.False(t, ValidWorkflowName(""))
	})
	t.Run("input and step names", func(t *testing.T) {
		assert.True(t, ValidInputName("target.branch"))
		assert.False(t, ValidInputName("1x"))
		assert.False(t, ValidInputName("a b"))
		assert.True(t, ValidStepID("lint_2"))
		assert.False(t, ValidStepID("Lint"))
	})
	t.Run("codes and names from asc are slugs or nothing", func(t *testing.T) {
		assert.Equal(t, "BUDGET_EXCEEDED", SafeCode("BUDGET_EXCEEDED"))
		assert.Empty(t, SafeCode("has space"))
		assert.Empty(t, SafeCode("<script>"))
		assert.Equal(t, "agent:coder", SafeName("agent:coder"))
		assert.Empty(t, SafeName("a<b"))
		assert.Empty(t, SafeName(strings.Repeat("a", 81)))
	})
}

func TestCleanText(t *testing.T) {
	// Hidden characters go: controls, bidi overrides and isolates, zero-width marks, the BOM.
	in := "a\x00b\x1b[31mc" + rlo + "d" + isolate + "e" + zwsp + "f" + bom + "g\r\nh\ti"
	assert.Equal(t, "ab[31mcdefg\n\nh\ti", CleanText(in, 100))

	// Invalid UTF-8 is dropped.
	assert.Equal(t, "ok", CleanText("o\xffk", 10))

	// Cutting happens on a character boundary and says so.
	got := CleanText("héllo wörld", 6)
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.LessOrEqual(t, len(got), 6+len("…"))
	assert.Equal(t, "abc", CleanText("abc", 3))
	assert.Empty(t, CleanText("abc", 0))

	// Markup is not touched here (the template escapes it), only made safe to carry.
	assert.Equal(t, "<b>x</b>", CleanText("<b>x</b>", 100))
}

func TestCleanLine(t *testing.T) {
	assert.Equal(t, "a b c", CleanLine("a\nb\tc", 100))
	assert.Equal(t, "x", CleanLine("  x \n", 100))
	assert.Equal(t, "ab", CleanLine("a"+rlo+"b", 100))
}

func TestCheckFreeText(t *testing.T) {
	got, ok := CheckFreeText("  Add retries\nand tests\t ", 100)
	assert.True(t, ok)
	assert.Equal(t, "Add retries\nand tests", got)

	for name, bad := range map[string]string{
		"empty":      "   ",
		"nul":        "a\x00b",
		"escape":     "a\x1bb",
		"bad utf8":   "a\xffb",
		"too long":   strings.Repeat("x", 101),
		"only space": "\n\t",
	} {
		_, ok := CheckFreeText(bad, 100)
		assert.False(t, ok, name)
	}
	_, ok = CheckFreeText(strings.Repeat("x", 100), 100)
	assert.True(t, ok)
}

func TestCheckLine(t *testing.T) {
	got, ok := CheckLine("  Fix the build ", 64, false)
	assert.True(t, ok)
	assert.Equal(t, "Fix the build", got)
	_, ok = CheckLine("", 64, false)
	assert.False(t, ok)
	_, ok = CheckLine("", 64, true)
	assert.True(t, ok)
	_, ok = CheckLine("a\nb", 64, true)
	assert.False(t, ok, "a line has no line break")
	_, ok = CheckLine("a"+rlo+"b", 64, true)
	assert.False(t, ok, "no bidi override")
	_, ok = CheckLine(strings.Repeat("a", 65), 64, true)
	assert.False(t, ok)
}

func TestParseGlobs(t *testing.T) {
	got, ok := ParseGlobs("src/**\n tests/** ,docs/*.md\r\nsrc/**\n\n")
	assert.True(t, ok)
	assert.Equal(t, []string{"src/**", "tests/**", "docs/*.md"}, got, "split on lines and commas, trimmed, repeats dropped")

	got, ok = ParseGlobs("")
	assert.True(t, ok)
	assert.Empty(t, got)

	for name, bad := range map[string]string{
		"parent":     "src/../etc",
		"parent end": "..",
		"absolute":   "/etc/passwd",
		"backslash":  `src\x`,
		"control":    "src/\x00",
		"bidi":       "src/" + rlo + "x",
		"too long":   strings.Repeat("a", MaxGlobBytes+1),
		"too many":   strings.Repeat("a\n", 0) + strings.Join(manyGlobs(MaxGlobs+1), "\n"),
		"huge":       strings.Repeat("a,", 5000),
	} {
		_, ok := ParseGlobs(bad)
		assert.False(t, ok, name)
	}
	_, ok = ParseGlobs(strings.Join(manyGlobs(MaxGlobs), "\n"))
	assert.True(t, ok, "exactly the maximum is fine")
}

func manyGlobs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "dir" + strings.Repeat("x", i+1) + "/**"
	}
	return out
}
