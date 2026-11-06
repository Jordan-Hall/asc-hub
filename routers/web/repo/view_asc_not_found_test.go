// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscPathChanges(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, _, err := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...).RunStdString(&git.RunOpts{Dir: dir})
		require.NoError(t, err, args)
		return strings.TrimSpace(out)
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "t")
	run("config", "user.email", "t@example.org")
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	write("retry.rs", "1")
	run("add", "retry.rs")
	run("commit", "-qm", "add")
	added := run("rev-parse", "HEAD")
	for _, body := range []string{"2", "3", "4"} {
		write("retry.rs", body)
		run("commit", "-qam", "edit "+body)
	}
	run("rm", "-q", "retry.rs")
	run("commit", "-qm", "remove")
	removed := run("rev-parse", "HEAD")

	gitRepo, err := git.OpenRepository(t.Context(), dir)
	require.NoError(t, err)
	defer gitRepo.Close()
	// five commits over three pages: the oldest is read from the last page
	defer test.MockVariableValue(&setting.Git.CommitsRangeSize, 2)()

	newest, oldest, err := ascPathChanges(gitRepo, "main", "retry.rs")
	require.NoError(t, err)
	assert.Equal(t, removed, newest)
	assert.Equal(t, added, oldest)

	newest, oldest, err = ascPathChanges(gitRepo, "main", "never.rs")
	require.NoError(t, err)
	assert.Empty(t, newest+oldest)
}

func TestPrepareAscNotFound(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	page := func(t *testing.T, branch, treePath, backend string) *context.Context {
		t.Helper()
		ctx, _ := contexttest.MockContext(t, "/user2/repo1/src/branch/"+branch+"/"+treePath)
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		t.Cleanup(func() { ctx.Repo.GitRepo.Close() })
		ctx.Repo.Repository.StorageBackend = backend
		commit, err := ctx.Repo.GitRepo.GetBranchCommit(branch)
		require.NoError(t, err)
		ctx.Repo.Commit, ctx.Repo.CommitID = commit, commit.ID.String()
		ctx.Repo.IsViewBranch, ctx.Repo.RefName, ctx.Repo.BranchName, ctx.Repo.TreePath = true, branch, branch, treePath
		prepareAscNotFound(ctx)
		return ctx
	}

	t.Run("a file on the default branch says the change that added it", func(t *testing.T) {
		// the fixture's sub-home-md-img-check has no README.md; master's first commit added it
		nf := page(t, "sub-home-md-img-check", "README.md", "asc").Data["AscNotFound"].(*AscNotFound)
		assert.Equal(t, "README.md", nf.Name)
		assert.Equal(t, "sub-home-md-img-check@4649299398", nf.Version)
		assert.True(t, nf.OnMain)
		assert.Equal(t, "65f1bf27bc", nf.Change)
		assert.Equal(t, "/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d", nf.ChangeLink)
		assert.Equal(t, "/user2/repo1/src/branch/master/README.md", nf.MainLink)
		assert.Equal(t, "/user2/repo1/commits/branch/master/README.md", nf.HistoryLink)
	})
	t.Run("a path the default branch never had", func(t *testing.T) {
		nf := page(t, "master", "File-WoW", "asc").Data["AscNotFound"].(*AscNotFound)
		assert.False(t, nf.OnMain)
		assert.Empty(t, nf.Change)
		assert.Empty(t, nf.HistoryLink)
		assert.Equal(t, "/user2/repo1/src/branch/master", nf.MainLink)
	})
	t.Run("the page shows the state", func(t *testing.T) {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/src/branch/sub-home-md-img-check/README.md")
		ctx.Render = templates.HTMLRenderer()
		ctx.Req.Header.Set("Accept", "text/html")
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		ctx.Repo.Repository.StorageBackend = "asc"
		commit, err := ctx.Repo.GitRepo.GetBranchCommit("sub-home-md-img-check")
		require.NoError(t, err)
		ctx.Repo.Commit, ctx.Repo.CommitID = commit, commit.ID.String()
		ctx.Repo.IsViewBranch, ctx.Repo.RefName, ctx.Repo.BranchName, ctx.Repo.TreePath = true, "sub-home-md-img-check", "sub-home-md-img-check", "README.md"
		HandleGitError(ctx, "test", git.ErrNotExist{RelPath: "README.md"})
		require.Equal(t, http.StatusNotFound, resp.Code)
		body := resp.Body.String()
		assert.Contains(t, body, `data-testid="asc-not-found"`)
		assert.Contains(t, body, `asc_rp.nf_title`)
		assert.Contains(t, body, `href="/user2/repo1/src/branch/master/README.md"`)
		assert.Contains(t, body, `href="/user2/repo1/commits/branch/master/README.md"`)
	})
	t.Run("a git repository keeps the plain not found page", func(t *testing.T) {
		assert.Nil(t, page(t, "master", "File-WoW", "git").Data["AscNotFound"])
	})
}
