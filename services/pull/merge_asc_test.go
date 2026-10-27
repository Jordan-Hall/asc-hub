// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bridgeWriter is an asc Writer whose ref update reaches the git directory only after a delay, as asc's
// bridge_promote does.
type bridgeWriter struct {
	repostore.Writer
	dir    string
	commit string
	delay  time.Duration
}

func (w bridgeWriter) UpdateRefs(_ context.Context, req repostore.UpdateRefsRequest) ([]repostore.RefResult, error) {
	id, err := git.NewIDFromString(w.commit)
	if err != nil {
		return nil, err
	}
	name := req.Updates[0].Name
	go func() {
		time.Sleep(w.delay)
		_ = exec.Command("git", "-C", w.dir, "update-ref", name, w.commit).Run()
	}()
	return []repostore.RefResult{{Name: name, New: id}}, nil
}

func TestSetPullHeadThroughAscWaitsForTheBridge(t *testing.T) {
	root := t.TempDir()
	oldRoot := setting.RepoRootPath
	setting.RepoRootPath = root
	t.Cleanup(func() { setting.RepoRootPath = oldRoot })

	repo := &repo_model.Repository{OwnerName: "owner", Name: "repo", StorageBackend: "asc"}
	require.NoError(t, os.MkdirAll(repo.RepoPath(), 0o755))
	run := func(env []string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo.RepoPath()}, args...)...)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	run(nil, "init", "-q", "--bare", "-b", "master")
	tree := run(nil, "hash-object", "-t", "tree", "-w", "/dev/null")
	commit := run([]string{"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com"}, "commit-tree", tree, "-m", "one")

	w := &bridgeWriter{dir: repo.RepoPath(), commit: commit, delay: 150 * time.Millisecond}
	old := pullHeadWriter
	pullHeadWriter = func() (repostore.Writer, error) { return w, nil }
	t.Cleanup(func() { pullHeadWriter = old })

	pr := &issues_model.PullRequest{ID: 1, Index: 1, BaseRepo: repo}
	start := time.Now()
	require.NoError(t, setPullHeadThroughAsc(t.Context(), pr, nil, commit))
	assert.GreaterOrEqual(t, time.Since(start), 120*time.Millisecond, "it returned before git showed the pull head")
	assert.Equal(t, commit, run(nil, "rev-parse", pr.GetGitRefName()))
}
