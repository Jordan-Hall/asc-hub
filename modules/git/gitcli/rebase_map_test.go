// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const treeV1 = `package git

func (t *Tree) ListEntries() {
	a := 1
}

func Other() {
	b := 1
}
`

// runRebaseMap runs build/rebase-map.sh in dir and returns its output and exit status.
func runRebaseMap(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	script, err := filepath.Abs("../../../build/rebase-map.sh")
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(out))
	return string(out), exit.ExitCode()
}

func TestRebaseMap(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}

	dir := t.TempDir()
	env := []string{
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_AUTHOR_DATE=2026-01-01T00:00:00+00:00",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_COMMITTER_DATE=2026-01-01T00:00:00+00:00",
	}
	gitRun(t, dir, nil, "init", "-q", "-b", "master")
	commit := func(msg string) string {
		gitRun(t, dir, nil, "add", "-A")
		gitRun(t, dir, env, "commit", "-q", "-m", msg)
		return gitRun(t, dir, nil, "rev-parse", "HEAD")
	}

	writeFile(t, dir, "modules/git/tree.go", []byte(treeV1))
	writeFile(t, dir, "modules/git/command.go", []byte("package git\n\nfunc Run() {\n\tx := 1\n}\n"))
	writeFile(t, dir, "modules/git/grep.go", []byte("package git\n\nfunc GrepSearch() {\n\tg := 1\n}\n"))
	writeFile(t, dir, "modules/git/gitcli/objects.go", []byte("package gitcli\n"))
	writeFile(t, dir, "modules/git/tree_test.go", []byte("package git\n"))
	old := commit("old")

	// Upstream touches moved code in tree.go, unrelated code in tree.go and command.go, a test and gitcli.
	writeFile(t, dir, "modules/git/tree.go", []byte(`package git

func (t *Tree) ListEntries() {
	a := 2
}

func Other() {
	b := 2
}
`))
	writeFile(t, dir, "modules/git/command.go", []byte("package git\n\nfunc Run() {\n\tx := 2\n}\n"))
	writeFile(t, dir, "modules/git/tree_test.go", []byte("package git\n\n// changed\n"))
	writeFile(t, dir, "modules/git/gitcli/objects.go", []byte("package gitcli\n\n// changed\n"))
	changed := commit("changed")

	out, code := runRebaseMap(t, dir, old, changed)
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "M  modules/git/tree.go -> modules/git/gitcli/objects.go  [moved]")
	assert.Contains(t, out, "    port: @@")
	assert.Contains(t, out, "ListEntries")
	assert.NotContains(t, out, "Other()", "a hunk in code that stayed is not reported as a port")
	assert.Contains(t, out, "M  modules/git/command.go  [stays in modules/git]")
	assert.NotContains(t, out, "tree_test.go")
	assert.Equal(t, 1, strings.Count(out, "gitcli/objects.go"), "files below gitcli are not upstream files")
	assert.NotContains(t, out, "grep.go", "unchanged files are not listed")

	// The range defaults to HEAD.
	again, code := runRebaseMap(t, dir, old)
	assert.Equal(t, 1, code, again)
	assert.Equal(t, out, again)

	// Only unmoved code changed: nothing to port.
	writeFile(t, dir, "modules/git/tree.go", []byte(`package git

func (t *Tree) ListEntries() {
	a := 2
}

func Other() {
	b := 3
}
`))
	other := commit("other")
	out, code = runRebaseMap(t, dir, changed, other)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "no hunk touches moved code")
	assert.NotContains(t, out, "port:")

	// A deleted file loses the moved code's upstream home.
	require.NoError(t, os.Remove(filepath.Join(dir, "modules/git/grep.go")))
	deleted := commit("delete grep.go")
	out, code = runRebaseMap(t, dir, other, deleted)
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "D  modules/git/grep.go -> modules/git/gitcli/bulk.go  [moved]")
	assert.Contains(t, out, "port: file deleted upstream")

	// No change in modules/git.
	out, code = runRebaseMap(t, dir, deleted, deleted)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "no changes to modules/git")

	// Revisions are data: an option-shaped or unknown revision is refused and nothing is written.
	marker := filepath.Join(t.TempDir(), "written")
	for _, bad := range []string{"--output=" + marker, "no-such-rev", ""} {
		out, code = runRebaseMap(t, dir, bad)
		assert.Equal(t, 2, code, "%q: %s", bad, out)
	}
	_, err := os.Stat(marker)
	assert.True(t, errors.Is(err, os.ErrNotExist), "an option-shaped revision wrote a file")

	// Usage errors and help.
	_, code = runRebaseMap(t, dir)
	assert.Equal(t, 2, code)
	out, code = runRebaseMap(t, dir, "-h")
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "Usage: build/rebase-map.sh")
}
