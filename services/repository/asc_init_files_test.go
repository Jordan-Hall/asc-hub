// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"io"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/repostore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePolicyChecks(t *testing.T) {
	got, err := ParsePolicyChecks("build, test\nci/lint,build\r\n  ")
	require.NoError(t, err)
	assert.Equal(t, []string{"build", "test", "ci/lint"}, got, "repeats are dropped")

	got, err = ParsePolicyChecks("")
	require.NoError(t, err)
	assert.Empty(t, got)

	for _, bad := range []string{`a"b`, `x\y`, "-lead", "tab\tname", strings.Repeat("a", 65), "a,b,c,d,e,f,g,h,i,j,k", "naïve", "a=b"} {
		_, err := ParsePolicyChecks(bad)
		assert.ErrorIs(t, err, ErrBadPolicyChecks, bad)
	}
}

func TestPolicyFileText(t *testing.T) {
	text, err := PolicyFileText("maya", 2, []string{"build", "ci/test"})
	require.NoError(t, err)
	assert.Equal(t, "version = 1\n\n[roles]\nmaintainer = [\"user:maya\"]\n\n[trunk]\nrequire_approvals = 2\nrequire_checks = [\"build\", \"ci/test\"]\n", text)

	text, err = PolicyFileText("maya", 0, nil)
	require.NoError(t, err)
	assert.Equal(t, "version = 1\n\n[roles]\nmaintainer = [\"user:maya\"]\n", text, "nothing required: no trunk table")

	for _, bad := range []struct {
		who       string
		approvals int
		checks    []string
	}{{`ma"ya`, 1, nil}, {"", 1, nil}, {"maya", 6, nil}, {"maya", -1, nil}, {"maya", 1, []string{`x"y`}}} {
		_, err := PolicyFileText(bad.who, bad.approvals, bad.checks)
		assert.Error(t, err, bad)
	}
}

func TestAscExtraFiles(t *testing.T) {
	read := func(op repostore.FileOp) string {
		b, err := io.ReadAll(op.Content)
		require.NoError(t, err)
		return string(b)
	}
	repo := &repo_model.Repository{Name: "ledger"} // the git backend
	ops, err := ascExtraFiles(repo, CreateRepoOptions{AgentsFile: true, PolicyFile: true, PolicyMaintainer: "maya", PolicyApprovals: 2})
	require.NoError(t, err)
	require.Len(t, ops, 1, "asc does not keep this repository: no policy file")
	assert.Equal(t, "AGENTS.md", ops[0].Path)
	assert.Contains(t, read(ops[0]), "# Agents in ledger")

	repo.StorageBackend = "asc"
	ops, err = ascExtraFiles(repo, CreateRepoOptions{PolicyFile: true, PolicyMaintainer: "maya", PolicyApprovals: 2, PolicyChecks: []string{"build"}})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, ".asc/policy.toml", ops[0].Path)
	assert.Contains(t, read(ops[0]), "require_approvals = 2")

	_, err = ascExtraFiles(repo, CreateRepoOptions{PolicyFile: true, PolicyMaintainer: `bad"name`})
	assert.Error(t, err)

	// extras alone make a first commit without the README
	assert.True(t, CreateRepoOptions{AgentsFile: true}.wantsFirstCommit())
	assert.False(t, CreateRepoOptions{}.wantsFirstCommit())
}
