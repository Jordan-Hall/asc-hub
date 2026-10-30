// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const protectionSample = `version = 1

[roles]
maintainer = ["user:qa"]

# who may land
[trunk]
require_approvals = 1
require_checks = ["build"]

[merge]
strategy = "rebase"
`

func TestProtectionRoundTrip(t *testing.T) {
	p := ProtectionOfText(protectionSample, "main")
	assert.Equal(t, int64(1), p.RequiredApprovals)
	assert.Equal(t, []string{"build"}, p.StatusChecks)
	assert.False(t, p.RequireSigned)
	assert.False(t, p.QueueOnly)

	want := BranchRule{
		Branch: "main", RequiredApprovals: 2, StatusChecks: []string{"build", "lint"}, RequireSigned: true,
		AllowForcePush: false, QueueOnly: true, ProtectedPaths: []string{".asc/**", "infra/**"},
	}
	out, err := ApplyProtection(protectionSample, want)
	require.NoError(t, err)
	assert.Contains(t, out, "# who may land", "comments stay")
	assert.Contains(t, out, "strategy = \"rebase\"", "other keys stay")
	got := ProtectionOfText(out, "main")
	assert.Equal(t, want, got)
}

func TestApplyProtectionRemovesOnlyItsOwnPathRules(t *testing.T) {
	text := protectionSample + "\n[paths.\"src/**\"]\nwrite = [\"role:maintainer\", \"agent:*\"]\n\n[paths.\"docs/**\"]\nwrite = [\"role:maintainer\"]\n"
	p := ProtectionOfText(text, "main")
	assert.Equal(t, []string{"docs/**"}, p.ProtectedPaths, "a rule with other writers is not a protected pattern")
	p.ProtectedPaths = nil
	out, err := ApplyProtection(text, p)
	require.NoError(t, err)
	assert.Contains(t, out, `[paths."src/**"]`)
	assert.NotContains(t, out, `[paths."docs/**"]`)
	// adding a pattern on top of a rule with other writers is refused, never merged
	_, err = ApplyProtection(text, BranchRule{Branch: "main", ProtectedPaths: []string{"src/**"}})
	require.ErrorIs(t, err, ErrUnsupportedProtection)
}

func TestApplyProtectionRefusesBadInput(t *testing.T) {
	_, err := ApplyProtection(protectionSample, BranchRule{RequiredApprovals: -1})
	require.ErrorIs(t, err, ErrBadInput)
	_, err = ApplyProtection(protectionSample, BranchRule{ProtectedPaths: []string{"../etc"}})
	require.ErrorIs(t, err, ErrBadInput)
}

func TestProtectionFromRequest(t *testing.T) {
	ok := ProtectionRequest{RuleName: "main", EnablePush: false, EnableStatusCheck: true, StatusCheckContexts: []string{"build"}, RequiredApprovals: 2, RequireSignedCommits: true, ProtectedFilePatterns: ".asc/**; infra/**"}
	p, err := ProtectionFromRequest(ok, "main")
	require.NoError(t, err)
	assert.True(t, p.QueueOnly, "push disabled is landing through the queue")
	assert.Equal(t, []string{".asc/**", "infra/**"}, p.ProtectedPaths)
	assert.Equal(t, []string{"build"}, p.StatusChecks)

	for name, mutate := range map[string]func(*ProtectionRequest){
		"other branch":    func(r *ProtectionRequest) { r.RuleName = "release/*" },
		"push whitelist":  func(r *ProtectionRequest) { r.PushWhitelist = true },
		"merge whitelist": func(r *ProtectionRequest) { r.MergeWhitelist = true },
		"outdated":        func(r *ProtectionRequest) { r.BlockOnOutdatedBranch = true },
		"unprotected":     func(r *ProtectionRequest) { r.UnprotectedFilePatterns = "docs/**" },
		"no contexts":     func(r *ProtectionRequest) { r.StatusCheckContexts = nil },
	} {
		r := ok
		mutate(&r)
		_, err := ProtectionFromRequest(r, "main")
		require.ErrorIs(t, err, ErrUnsupportedProtection, name)
	}
}
