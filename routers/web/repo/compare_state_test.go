// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBehind(t *testing.T) {
	n, ok := parseBehind("4\n")
	assert.True(t, ok)
	assert.Equal(t, 4, n)
	n, ok = parseBehind("0")
	assert.True(t, ok)
	assert.Zero(t, n)
	for _, bad := range []string{"", "x", "-1", "1 2"} {
		_, ok := parseBehind(bad)
		assert.False(t, ok, bad)
	}
}

// compareRepo makes a repository whose main has one change after the point where the branches "clean" and "clash"
// left it; "clash" edits the same line as main.
func compareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.org", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.org", "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	write := func(name, text string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644))
	}
	run("init", "-q", "-b", "main")
	write("a.txt", "one\n")
	run("add", "a.txt")
	run("commit", "-q", "-m", "base")
	run("branch", "clean")
	run("branch", "clash")
	write("a.txt", "two\n")
	run("commit", "-q", "-am", "main")
	run("checkout", "-q", "clean")
	write("b.txt", "b\n")
	run("add", "b.txt")
	run("commit", "-q", "-m", "clean")
	run("checkout", "-q", "clash")
	write("a.txt", "three\n")
	run("commit", "-q", "-am", "clash")
	return dir
}

func TestPrepareCompareState(t *testing.T) {
	dir := compareRepo(t)

	ctx, _ := contexttest.MockContext(t, "user2/repo1/compare")
	prepareCompareState(ctx, dir, "main", "clean")
	assert.Equal(t, 1, ctx.Data["CompareBehind"])
	assert.Equal(t, true, ctx.Data["CompareBehindKnown"])
	assert.Equal(t, compareMergeClean, ctx.Data["CompareMergeState"])

	ctx, _ = contexttest.MockContext(t, "user2/repo1/compare")
	prepareCompareState(ctx, dir, "main", "clash")
	assert.Equal(t, compareMergeConflicts, ctx.Data["CompareMergeState"])

	ctx, _ = contexttest.MockContext(t, "user2/repo1/compare")
	prepareCompareState(ctx, dir, "clean", "clean")
	assert.Equal(t, 0, ctx.Data["CompareBehind"], "a head level with its base is behind by none")
	assert.Equal(t, true, ctx.Data["CompareBehindKnown"])

	ctx, _ = contexttest.MockContext(t, "user2/repo1/compare")
	prepareCompareState(ctx, dir, "main", "missing")
	assert.Nil(t, ctx.Data["CompareBehindKnown"], "an unknown count is not shown as zero")
	assert.Equal(t, compareMergeUnknown, ctx.Data["CompareMergeState"])
}
