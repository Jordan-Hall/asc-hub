// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/gitcli"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistered(t *testing.T) {
	w, err := repostore.Default()
	require.NoError(t, err)
	assert.IsType(t, &gitcli.Writer{}, w)
}

func TestCreateCommitRootAndChild(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run(format, func(t *testing.T) {
			if format == "sha256" && !git.SupportHashSha256 {
				t.Skip("git without sha256 support")
			}
			repo := newRepo(t, "r", format)

			root := commit(t, repo, nil, "main", "first", put("README.md", "hello\n"), put("dir/a.txt", "a\n"))
			assert.Equal(t, root.String(), id(t, repo, "refs/heads/main").String())
			assert.Equal(t, "hello", fileAt(t, repo, "main", "README.md"))
			assert.Equal(t, "Alice", gitOut(t, repo.Path, "log", "-1", "--format=%an", "main"))
			assert.Equal(t, "first", gitOut(t, repo.Path, "log", "-1", "--format=%B", "main"))
			assert.Equal(t, root.String(), gitOut(t, repo.Path, "rev-list", "--parents", "-n", "1", "main"), "a root commit has no parent")

			child := commit(t, repo, root, "main", "second",
				put("dir/a.txt", "changed\n"), del("README.md"), put("run.sh", "#!/bin/sh\n"))
			assert.Equal(t, "changed", fileAt(t, repo, "main", "dir/a.txt"))
			requireNoFile(t, repo, "main", "README.md")
			assert.Equal(t, root.String(), gitOut(t, repo.Path, "rev-parse", child.String()+"^"))

			// A mode is kept.
			res, err := gitcli.New().CreateCommit(t.Context(), repostore.CommitRequest{
				Repo: repo, Parent: child, Meta: meta("exec"),
				Ops:    []repostore.FileOp{{Kind: repostore.OpPut, Path: "run.sh", Mode: git.EntryModeExec, Content: strings.NewReader("#!/bin/sh\n")}},
				Update: toBranch(repo, "exec"),
			})
			require.NoError(t, err)
			assert.Contains(t, gitOut(t, repo.Path, "ls-tree", res.Commit.String(), "run.sh"), "100755")
		})
	}
}

func TestCreateCommitUnpublished(t *testing.T) {
	repo := newRepo(t, "r", "sha1")
	res, err := gitcli.New().CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: repo,
		Ops:  []repostore.FileOp{put("a", "a")},
		Meta: meta("msg"),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Commit)
	assert.False(t, hasObject(t, repo, res.Commit), "a commit without Update never reaches the repository")
	assert.Empty(t, gitOut(t, repo.Path, "for-each-ref"))
}

func TestCreateCommitPublishedIdentity(t *testing.T) {
	repo := newRepo(t, "r", "sha1")
	_, err := gitcli.New().CreateCommit(t.Context(), repostore.CommitRequest{
		Repo:   repo,
		Ops:    []repostore.FileOp{put("a", "a")},
		Meta:   repostore.CommitMeta{Author: alice, Committer: bob, Message: "msg", Signoff: true},
		Update: toBranch(repo, "main"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice <alice@example.com> 2026-01-02T03:04:05+00:00", gitOut(t, repo.Path, "log", "-1", "--format=%an <%ae> %aI", "main"))
	assert.Equal(t, "Bob <bob@example.com> 2026-01-02T03:04:06+00:00", gitOut(t, repo.Path, "log", "-1", "--format=%cn <%ce> %cI", "main"))
	assert.Equal(t, "msg\n\nSigned-off-by: Bob <bob@example.com>", gitOut(t, repo.Path, "log", "-1", "--format=%B", "main"))
}

func TestCreateCommitErrors(t *testing.T) {
	repo := newRepo(t, "r", "sha1")
	root := commit(t, repo, nil, "main", "first", put("a.txt", "a"))
	w := gitcli.New()
	base := func() repostore.CommitRequest {
		return repostore.CommitRequest{Repo: repo, Parent: root, Meta: meta("m"), Update: toBranch(repo, "main")}
	}

	t.Run("delete of a missing file", func(t *testing.T) {
		req := base()
		req.Ops = []repostore.FileOp{del("nope.txt")}
		_, err := w.CreateCommit(t.Context(), req)
		var notFound *repostore.FileNotFoundError
		require.ErrorAs(t, err, &notFound)
		assert.Equal(t, "nope.txt", notFound.Path)
	})

	t.Run("delete of a directory", func(t *testing.T) {
		parent := commit(t, repo, root, "dirs", "dir", put("d/x", "x"))
		req := base()
		req.Parent = parent
		req.Ops = []repostore.FileOp{del("d")}
		_, err := w.CreateCommit(t.Context(), req)
		var notFound *repostore.FileNotFoundError
		require.ErrorAs(t, err, &notFound)
	})

	t.Run("invalid path", func(t *testing.T) {
		req := base()
		req.Ops = []repostore.FileOp{put("a/../../etc/passwd", "x")}
		_, err := w.CreateCommit(t.Context(), req)
		var invalid *repostore.InvalidPathError
		require.ErrorAs(t, err, &invalid)

		req.Ops = []repostore.FileOp{put("x/.git/config", "x")}
		_, err = w.CreateCommit(t.Context(), req)
		require.ErrorAs(t, err, &invalid)
	})

	t.Run("a path git refuses", func(t *testing.T) {
		req := base()
		// git protects NTFS short names for .git: the request validation does not know them, git does.
		req.Ops = []repostore.FileOp{put("git~1/config", "x")}
		_, err := w.CreateCommit(t.Context(), req)
		var invalid *repostore.InvalidPathError
		require.ErrorAs(t, err, &invalid)
	})

	t.Run("relative repository path", func(t *testing.T) {
		req := base()
		req.Repo.Path = "relative.git"
		req.Ops = []repostore.FileOp{put("a", "a")}
		_, err := w.CreateCommit(t.Context(), req)
		require.ErrorIs(t, err, repostore.ErrInvalidRequest)
	})

	t.Run("newline in the repository path", func(t *testing.T) {
		req := base()
		req.Repo.Path = repo.Path + "\n/etc"
		req.Ops = []repostore.FileOp{put("a", "a")}
		_, err := w.CreateCommit(t.Context(), req)
		require.ErrorIs(t, err, repostore.ErrInvalidRequest)
	})

	t.Run("a sign function that fails fails the request", func(t *testing.T) {
		req := base()
		req.Ops = []repostore.FileOp{put("a", "b")}
		req.Meta.Sign = func(_ context.Context, _ repostore.SignInput) (*repostore.SignDecision, error) {
			return nil, errors.New("no key")
		}
		_, err := w.CreateCommit(t.Context(), req)
		require.Error(t, err)
		assert.Equal(t, root.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("nothing moved", func(t *testing.T) {
		assert.Equal(t, root.String(), id(t, repo, "refs/heads/main").String())
	})
}

func TestCreateCommitPublishRules(t *testing.T) {
	repo := newRepo(t, "r", "sha1")
	w := gitcli.New()
	root := commit(t, repo, nil, "main", "first", put("a.txt", "a"))
	tip := commit(t, repo, root, "main", "second", put("a.txt", "b"))

	t.Run("stale parent is not fast-forward", func(t *testing.T) {
		_, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: root, Meta: meta("late"), Ops: []repostore.FileOp{put("a.txt", "c")}, Update: toBranch(repo, "main"),
		})
		var outOfDate *git.ErrPushOutOfDate
		require.ErrorAs(t, err, &outOfDate)
		assert.Equal(t, tip.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("force overwrites", func(t *testing.T) {
		upd := toBranch(repo, "main")
		upd.Force = true
		res, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: root, Meta: meta("forced"), Ops: []repostore.FileOp{put("a.txt", "c")}, Update: upd,
		})
		require.NoError(t, err)
		assert.Equal(t, res.Commit.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("BeforePublish sees the commit and can stop the publish", func(t *testing.T) {
		before := id(t, repo, "refs/heads/main")
		var seen repostore.PublishInput
		stop := errors.New("stop")
		_, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: before, Meta: meta("never"), Ops: []repostore.FileOp{put("never", "x")}, Update: toBranch(repo, "main"),
			BeforePublish: func(_ context.Context, in repostore.PublishInput) error {
				seen = in
				return stop
			},
		})
		require.ErrorIs(t, err, stop)
		assert.Equal(t, before.String(), id(t, repo, "refs/heads/main").String())
		require.NotNil(t, seen.Commit)
		assert.Equal(t, before.String(), seen.Base.String())
		assert.NotEmpty(t, seen.GitDir)
		assert.False(t, hasObject(t, repo, seen.Commit), "the objects reach the repository only with the publish")
	})

	t.Run("a hook rejection is a rejection", func(t *testing.T) {
		hook := repo.Path + "/hooks/pre-receive"
		require.NoError(t, os.MkdirAll(repo.Path+"/hooks", 0o755))
		require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho no >&2\nexit 1\n"), 0o755))
		defer os.Remove(hook)
		before := id(t, repo, "refs/heads/main")
		_, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: before, Meta: meta("hooked"), Ops: []repostore.FileOp{put("h", "x")}, Update: toBranch(repo, "main"),
		})
		var rejected *git.ErrPushRejected
		require.ErrorAs(t, err, &rejected)
		assert.Equal(t, before.String(), id(t, repo, "refs/heads/main").String())
	})

	t.Run("the hook environment reaches the hook", func(t *testing.T) {
		hook := repo.Path + "/hooks/post-receive"
		out := t.TempDir() + "/env.txt"
		require.NoError(t, os.MkdirAll(repo.Path+"/hooks", 0o755))
		require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho \"$REPOSTORE_TEST\" > "+out+"\n"), 0o755))
		defer os.Remove(hook)
		_, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: id(t, repo, "refs/heads/main"), Meta: meta("env"), Ops: []repostore.FileOp{put("e", "x")}, Update: toBranch(repo, "main"),
			HookEnv: append(os.Environ(), "REPOSTORE_TEST=seen"),
		})
		require.NoError(t, err)
		got, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Equal(t, "seen", strings.TrimSpace(string(got)))
	})
}

func TestCreateCommitPreview(t *testing.T) {
	repo := newRepo(t, "r", "sha1")
	root := commit(t, repo, nil, "main", "first", put("a.txt", "one\ntwo\n"))
	var diff bytes.Buffer
	res, err := gitcli.New().CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: repo, Parent: root, Meta: meta("preview"), Ops: []repostore.FileOp{put("a.txt", "one\nthree\n"), put("b.txt", "new\n")},
		Preview: &diff,
	})
	require.NoError(t, err)
	assert.Nil(t, res.Commit)
	assert.NotNil(t, res.Tree)
	assert.Equal(t, 2, res.Stat.NumFiles)
	assert.Equal(t, 2, res.Stat.Additions)
	assert.Equal(t, 1, res.Stat.Deletions)
	assert.Contains(t, diff.String(), "\\a/a.txt")
	assert.Contains(t, diff.String(), "+three")
	assert.Equal(t, root.String(), id(t, repo, "refs/heads/main").String(), "a preview moves nothing")

	t.Run("a consumer that stops reading is not an error", func(t *testing.T) {
		_, err := gitcli.New().CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: root, Meta: meta("preview"), Ops: []repostore.FileOp{put("a.txt", "x\n")},
			Preview: failingWriter{},
		})
		require.NoError(t, err)
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
