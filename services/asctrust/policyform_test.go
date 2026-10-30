// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const formSample = `version = 1

[roles]
maintainer = ["user:qa"]

[trunk]
require_approvals = 1
require_checks = ["build"]

[merge]
strategy = "rebase"
queue = true

[agents]
default_scope = ["src/**"]
max_runtime_seconds = 1800
`

func TestFormRoundTrip(t *testing.T) {
	f := FormOfText(formSample)
	assert.Equal(t, int64(1), f.RequireApprovals)
	assert.Equal(t, []string{"build"}, f.RequireChecks)
	assert.Equal(t, "rebase", f.MergeStrategy)
	assert.True(t, f.MergeQueue)
	assert.True(t, f.AgentsEnabled, "agents are on unless the policy says otherwise")
	assert.Equal(t, int64(1800), f.MaxRuntimeSecs)

	f.RequireApprovals = 2
	f.RequireChecks = []string{"build", "lint"}
	f.RequireSigned = true
	f.AgentDenied = []string{".asc/**"}
	f.MaxTokens = 200000
	out, err := ApplyForm(formSample, f)
	require.NoError(t, err)
	assert.Equal(t, f, FormOfText(out))
	assert.Contains(t, out, "[roles]", "other sections stay")
}

func TestApplyFormRefuses(t *testing.T) {
	for name, mutate := range map[string]func(*PolicyForm){
		"approvals":  func(f *PolicyForm) { f.RequireApprovals = 65 },
		"budget":     func(f *PolicyForm) { f.MaxTokens = -1 },
		"strategy":   func(f *PolicyForm) { f.MergeStrategy = "octopus" },
		"glob":       func(f *PolicyForm) { f.AgentScope = []string{"../x"} },
		"check name": func(f *PolicyForm) { f.RequireChecks = []string{"a\"b"} },
	} {
		f := FormOfText(formSample)
		mutate(&f)
		_, err := ApplyForm(formSample, f)
		require.ErrorIs(t, err, ErrBadInput, name)
	}
}

func TestSplitLines(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, SplitLines("a, b\nc\r\n\n"))
	assert.Empty(t, SplitLines(" \n,"))
}

func TestLineDiff(t *testing.T) {
	d := LineDiff("a\nb\nc\n", "a\nc\nd\n")
	assert.Equal(t, []DiffLine{{" ", "a"}, {"-", "b"}, {" ", "c"}, {"+", "d"}}, d)
	assert.Equal(t, []DiffLine{{" ", "x"}}, LineDiff("x", "x\n"))
}

func TestFormReviewSwitches(t *testing.T) {
	f := FormOfText(formSample)
	assert.False(t, f.AuthorMayApprove, "an author may not approve unless the policy says so")
	assert.True(t, f.KeepApprovals, "approvals are kept on a restack unless the policy says so")
	out, err := ApplyForm(formSample, f)
	require.NoError(t, err)
	assert.NotContains(t, out, "[review]", "defaults are not written into a file that lacks them")
	f.AuthorMayApprove = true
	f.KeepApprovals = false
	out, err = ApplyForm(formSample, f)
	require.NoError(t, err)
	got := FormOfText(out)
	assert.True(t, got.AuthorMayApprove)
	assert.False(t, got.KeepApprovals)
}

func TestFormSignedSynonymAndBatch(t *testing.T) {
	text := `version = 1

[trunk]
require_signed = true

[signing]
require_signed = true

[merge]
queue = true
`
	f := FormOfText(text)
	assert.True(t, f.RequireSigned)
	f.RequireSigned = false
	f.MergeBatch = 4
	f.PostMergeChecks = true
	out, err := ApplyForm(text, f)
	require.NoError(t, err)
	got := FormOfText(out)
	assert.False(t, got.RequireSigned)
	v, _ := TomlBool(out, "trunk", "require_signed")
	assert.False(t, v, "the synonym is turned off with the section, or asc would still OR it in")
	assert.Equal(t, int64(4), got.MergeBatch)
	assert.True(t, got.PostMergeChecks)
	f.MergeBatch = 99
	_, err = ApplyForm(text, f)
	require.ErrorIs(t, err, ErrBadInput)
}
