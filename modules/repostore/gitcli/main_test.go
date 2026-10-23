// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/gitcli"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRun(m *testing.M) error {
	gitHomePath, err := os.MkdirTemp(os.TempDir(), "git-home")
	if err != nil {
		return fmt.Errorf("unable to create temp dir: %w", err)
	}
	defer util.RemoveAll(gitHomePath)
	setting.Git.HomePath = gitHomePath

	appData, err := os.MkdirTemp(os.TempDir(), "repostore-appdata")
	if err != nil {
		return fmt.Errorf("unable to create temp dir: %w", err)
	}
	defer util.RemoveAll(appData)
	setting.AppDataPath = appData
	setting.Repository.Local.LocalCopyPath = "tmp/local-repo"

	if err = git.InitFull(context.Background()); err != nil {
		return fmt.Errorf("failed to call Init: %w", err)
	}

	if exitCode := m.Run(); exitCode != 0 {
		return fmt.Errorf("run test failed, ExitCode=%d", exitCode)
	}
	return nil
}

func TestMain(m *testing.M) {
	if err := testRun(m); err != nil {
		fmt.Fprintf(os.Stderr, "Test failed: %v", err)
		os.Exit(1)
	}
}

var (
	alice = &repostore.Signature{Name: "Alice", Email: "alice@example.com", When: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	bob   = &repostore.Signature{Name: "Bob", Email: "bob@example.com", When: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)}
)

func meta(msg string) repostore.CommitMeta {
	return repostore.CommitMeta{Author: alice, Committer: alice, Message: msg}
}

// newRepo creates a bare repository with the Writer and returns its reference.
func newRepo(t *testing.T, name, format string) repostore.RepoRef {
	t.Helper()
	ref := repostore.RepoRef{ID: 1, Owner: "o", Name: name, ObjectFormat: format, Path: filepath.Join(t.TempDir(), name+".git")}
	require.NoError(t, gitcli.New().Init(t.Context(), repostore.InitRequest{Repo: ref, DefaultBranch: "main"}))
	// git.InitFull points core.hooksPath at generated hooks that run the Forgejo binary, which here is the test
	// binary. Use the repository's own hooks directory, so a test can add a hook of its own.
	gitOut(t, ref.Path, "config", "core.hooksPath", filepath.Join(ref.Path, "hooks"))
	return ref
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(t, cmd.Run(), "git %v: %s", args, stderr.String())
	return strings.TrimSpace(stdout.String())
}

func put(path, content string) repostore.FileOp {
	return repostore.FileOp{Kind: repostore.OpPut, Path: path, Content: strings.NewReader(content)}
}

func del(path string) repostore.FileOp {
	return repostore.FileOp{Kind: repostore.OpDelete, Path: path}
}

func toBranch(repo repostore.RepoRef, branch string) *repostore.RefUpdate {
	return &repostore.RefUpdate{Repo: repo, Ref: "refs/heads/" + branch}
}

// commit writes a commit on a branch (creating it when parent is nil) and returns its id.
func commit(t *testing.T, repo repostore.RepoRef, parent git.ObjectID, branch string, msg string, ops ...repostore.FileOp) git.ObjectID {
	t.Helper()
	res, err := gitcli.New().CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: repo, Parent: parent, Ops: ops, Meta: meta(msg), Update: toBranch(repo, branch),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Commit)
	return res.Commit
}

func id(t *testing.T, repo repostore.RepoRef, rev string) git.ObjectID {
	t.Helper()
	out := gitOut(t, repo.Path, "rev-parse", "--verify", rev)
	oid, err := git.NewIDFromString(out)
	require.NoError(t, err)
	return oid
}

func fileAt(t *testing.T, repo repostore.RepoRef, rev, path string) string {
	t.Helper()
	return gitOut(t, repo.Path, "show", rev+":"+path)
}

func requireNoFile(t *testing.T, repo repostore.RepoRef, rev, path string) {
	t.Helper()
	assert.Empty(t, gitOut(t, repo.Path, "ls-tree", "--name-only", rev, "--", path))
}

func hasObject(t *testing.T, repo repostore.RepoRef, oid git.ObjectID) bool {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "cat-file", "-e", oid.String())
	cmd.Dir = repo.Path
	return cmd.Run() == nil
}
