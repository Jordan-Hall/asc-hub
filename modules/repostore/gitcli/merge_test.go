// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/gitcli"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergeFixture is a repository with main and topic branches that diverge from one root:
// main has one commit that changes a.txt, topic has two commits that change b.txt.
type mergeFixture struct {
	repo                        repostore.RepoRef
	root, main2, topic1, topic2 git.ObjectID
}

func newMergeFixture(t *testing.T) *mergeFixture {
	t.Helper()
	repo := newRepo(t, "m", "sha1")
	f := &mergeFixture{repo: repo}
	f.root = commit(t, repo, nil, "main", "root", put("a.txt", "a\n"), put("b.txt", "b\n"))
	f.main2 = commit(t, repo, f.root, "main", "main2", put("a.txt", "a main\n"))
	f.topic1 = commit(t, repo, f.root, "topic", "topic1", put("b.txt", "b topic\n"))
	f.topic2 = commit(t, repo, f.topic1, "topic", "topic2", put("c.txt", "c topic\n"))
	return f
}

func (f *mergeFixture) request(style repostore.MergeStyle) repostore.MergeRequest {
	return repostore.MergeRequest{
		Base: f.repo, BaseRef: "refs/heads/main",
		Head: f.repo, HeadRev: "refs/heads/topic",
		Style:   style,
		Meta:    repostore.CommitMeta{Author: alice, Committer: alice, Message: "Merge topic"},
		Publish: toBranch(f.repo, "main"),
	}
}

func parents(t *testing.T, repo repostore.RepoRef, rev string) []string {
	t.Helper()
	fields := strings.Fields(gitOut(t, repo.Path, "rev-list", "--parents", "-n", "1", rev))
	return fields[1:]
}

func TestMergeStyles(t *testing.T) {
	w := gitcli.New()

	t.Run("merge", func(t *testing.T) {
		f := newMergeFixture(t)
		res, err := w.Merge(t.Context(), f.request(repostore.MergeStyleMerge))
		require.NoError(t, err)
		assert.Equal(t, res.Commit.String(), id(t, f.repo, "refs/heads/main").String())
		assert.Equal(t, f.main2.String(), res.BaseBefore.String())
		assert.Equal(t, []string{f.main2.String(), f.topic2.String()}, parents(t, f.repo, "main"))
		assert.Equal(t, "Merge topic", gitOut(t, f.repo.Path, "log", "-1", "--format=%B", "main"))
		assert.Equal(t, "Alice", gitOut(t, f.repo.Path, "log", "-1", "--format=%cn", "main"))
		assert.Equal(t, "a main", fileAt(t, f.repo, "main", "a.txt"))
		assert.Equal(t, "b topic", fileAt(t, f.repo, "main", "b.txt"))
		assert.Equal(t, f.topic2.String(), id(t, f.repo, "refs/heads/topic").String(), "the head does not move")
	})

	t.Run("squash", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleSquash)
		req.Meta.Message = "Squashed"
		var authors []string
		req.SquashAuthor = func(_ context.Context, a []*repostore.Signature) (*repostore.Signature, error) {
			for _, s := range a {
				authors = append(authors, s.Email)
			}
			return bob, nil
		}
		res, err := w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, []string{f.main2.String()}, parents(t, f.repo, "main"))
		assert.Equal(t, res.Commit.String(), id(t, f.repo, "refs/heads/main").String())
		assert.Equal(t, "Squashed", gitOut(t, f.repo.Path, "log", "-1", "--format=%B", "main"))
		assert.Equal(t, "Bob", gitOut(t, f.repo.Path, "log", "-1", "--format=%an", "main"))
		assert.Equal(t, []string{"alice@example.com"}, authors, "one author per distinct email")
		assert.Equal(t, "c topic", fileAt(t, f.repo, "main", "c.txt"))
	})

	t.Run("squash keeps the request author without a chooser", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleSquash)
		req.Meta.Author = bob
		_, err := w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "Bob", gitOut(t, f.repo.Path, "log", "-1", "--format=%an", "main"))
	})

	t.Run("rebase", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleRebase)
		req.AmendMessage = func(_ context.Context, last string) (string, error) {
			assert.Equal(t, "topic2", strings.TrimSpace(last))
			return "topic2 amended", nil
		}
		_, err := w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, []string{"topic2 amended", "topic1", "main2", "root"}, strings.Split(gitOut(t, f.repo.Path, "log", "--format=%s", "main"), "\n"))
		assert.Equal(t, "a main", fileAt(t, f.repo, "main", "a.txt"))
		assert.Equal(t, "c topic", fileAt(t, f.repo, "main", "c.txt"))
	})

	t.Run("rebase without a template keeps the commits", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleRebase)
		req.AmendMessage = func(context.Context, string) (string, error) { return "", nil }
		_, err := w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "topic2", gitOut(t, f.repo.Path, "log", "-1", "--format=%s", "main"))
	})

	t.Run("rebase-merge", func(t *testing.T) {
		f := newMergeFixture(t)
		_, err := w.Merge(t.Context(), f.request(repostore.MergeStyleRebaseMerge))
		require.NoError(t, err)
		assert.Equal(t, "Merge topic", gitOut(t, f.repo.Path, "log", "-1", "--format=%s", "main"))
		ps := parents(t, f.repo, "main")
		require.Len(t, ps, 2)
		assert.Equal(t, f.main2.String(), ps[0])
		assert.NotEqual(t, f.topic2.String(), ps[1], "the head was rebased")
		assert.Equal(t, f.main2.String(), gitOut(t, f.repo.Path, "merge-base", ps[0], ps[1]))
	})

	t.Run("fast-forward only", func(t *testing.T) {
		f := newMergeFixture(t)
		_, err := w.Merge(t.Context(), f.request(repostore.MergeStyleFastForwardOnly))
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.True(t, conflict.Diverging)
		assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String())

		// topic3 builds on main, so it fast-forwards.
		topic3 := commit(t, f.repo, f.main2, "ff", "ff", put("ff.txt", "ff\n"))
		req := f.request(repostore.MergeStyleFastForwardOnly)
		req.HeadRev = "refs/heads/ff"
		_, err = w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, topic3.String(), id(t, f.repo, "refs/heads/main").String())
	})

	t.Run("rebase update publishes the rebased head", func(t *testing.T) {
		f := newMergeFixture(t)
		var in repostore.PublishInput
		req := f.request(repostore.MergeStyleRebaseUpdate)
		req.Publish = &repostore.RefUpdate{Repo: f.repo, Ref: "refs/heads/topic", Force: true}
		req.BeforePublish = func(_ context.Context, got repostore.PublishInput) error {
			in = got
			return nil
		}
		res, err := w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String(), "the base does not move")
		assert.Equal(t, res.Commit.String(), id(t, f.repo, "refs/heads/topic").String())
		assert.Equal(t, []string{"topic2", "topic1", "main2", "root"}, strings.Split(gitOut(t, f.repo.Path, "log", "--format=%s", "topic"), "\n"))
		assert.Equal(t, f.main2.String(), in.Base.String())
		assert.Equal(t, f.root.String(), in.MergeBase.String())
		assert.Equal(t, f.topic2.String(), in.Head.String())
	})

	t.Run("a non-forced publish of a rebase is refused", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleRebaseUpdate)
		req.Publish = toBranch(f.repo, "topic")
		_, err := w.Merge(t.Context(), req)
		var outOfDate *git.ErrPushOutOfDate
		require.ErrorAs(t, err, &outOfDate)
		assert.Equal(t, f.topic2.String(), id(t, f.repo, "refs/heads/topic").String())
	})
}

func TestMergeFailures(t *testing.T) {
	w := gitcli.New()

	t.Run("conflict", func(t *testing.T) {
		f := newMergeFixture(t)
		commit(t, f.repo, f.root, "clash", "clash", put("a.txt", "a clash\n"))
		for _, style := range []repostore.MergeStyle{repostore.MergeStyleMerge, repostore.MergeStyleRebaseMerge, repostore.MergeStyleRebase} {
			req := f.request(style)
			req.HeadRev = "refs/heads/clash"
			_, err := w.Merge(t.Context(), req)
			var conflict *repostore.ConflictError
			require.ErrorAs(t, err, &conflict, style)
			assert.Equal(t, style, conflict.Style)
			assert.Equal(t, style == repostore.MergeStyleRebaseMerge || style == repostore.MergeStyleRebase, conflict.Rebase, style)
			assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String(), style)
		}
	})

	t.Run("unrelated histories", func(t *testing.T) {
		f := newMergeFixture(t)
		commit(t, f.repo, nil, "other", "other root", put("z.txt", "z\n"))
		req := f.request(repostore.MergeStyleMerge)
		req.HeadRev = "refs/heads/other"
		_, err := w.Merge(t.Context(), req)
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.True(t, conflict.Unrelated)
	})

	t.Run("unexpected head", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleMerge)
		req.ExpectHead = f.topic1
		_, err := w.Merge(t.Context(), req)
		var mismatch *repostore.HeadMismatchError
		require.ErrorAs(t, err, &mismatch)
		assert.Equal(t, f.topic1.String(), mismatch.Given)
		assert.Equal(t, f.topic2.String(), mismatch.Current)
		assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String())

		req.ExpectHead = f.topic2
		_, err = w.Merge(t.Context(), req)
		require.NoError(t, err)
	})

	t.Run("missing head", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request(repostore.MergeStyleMerge)
		req.HeadRev = "refs/heads/gone"
		_, err := w.Merge(t.Context(), req)
		var missing *repostore.RefNotFoundError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "refs/heads/gone", missing.Ref)
	})

	t.Run("before publish can stop the merge", func(t *testing.T) {
		f := newMergeFixture(t)
		stop := errors.New("lfs")
		var in repostore.PublishInput
		req := f.request(repostore.MergeStyleMerge)
		req.BeforePublish = func(_ context.Context, got repostore.PublishInput) error {
			in = got
			return stop
		}
		_, err := w.Merge(t.Context(), req)
		require.ErrorIs(t, err, stop)
		assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String())
		assert.Equal(t, f.main2.String(), in.Base.String())
		assert.Equal(t, f.topic2.String(), in.Head.String())
		assert.Equal(t, f.root.String(), in.MergeBase.String())
	})

	t.Run("invalid requests do nothing", func(t *testing.T) {
		f := newMergeFixture(t)
		for name, mutate := range map[string]func(*repostore.MergeRequest){
			"no style":        func(r *repostore.MergeRequest) { r.Style = "" },
			"head option":     func(r *repostore.MergeRequest) { r.HeadRev = "--upload-pack=touch /tmp/pwned" },
			"relative base":   func(r *repostore.MergeRequest) { r.Base.Path = "base.git" },
			"relative head":   func(r *repostore.MergeRequest) { r.Head.Path = "../head.git" },
			"newline in head": func(r *repostore.MergeRequest) { r.Head.Path = f.repo.Path + "\n/etc" },
		} {
			req := f.request(repostore.MergeStyleMerge)
			mutate(&req)
			_, err := w.Merge(t.Context(), req)
			require.Error(t, err, name)
			assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String(), name)
		}
	})
}

func TestMergeCrossRepository(t *testing.T) {
	w := gitcli.New()
	f := newMergeFixture(t)

	fork := repostore.RepoRef{ID: 2, Owner: "o", Name: "fork", ObjectFormat: "sha1", Path: strings.TrimSuffix(f.repo.Path, "m.git") + "fork.git"}
	require.NoError(t, w.Fork(t.Context(), repostore.ForkRequest{Repo: fork, From: f.repo}))
	fork.Path = strings.TrimSuffix(f.repo.Path, "m.git") + "fork.git"
	gitOut(t, fork.Path, "config", "core.hooksPath", fork.Path+"/hooks")
	onlyInFork := commit(t, fork, f.topic2, "feature", "only in fork", put("fork.txt", "fork\n"))
	require.False(t, hasObject(t, f.repo, onlyInFork))

	req := f.request(repostore.MergeStyleMerge)
	req.Head = fork
	req.HeadRev = "refs/heads/feature"
	res, err := w.Merge(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "fork", fileAt(t, f.repo, "main", "fork.txt"))
	assert.True(t, hasObject(t, f.repo, onlyInFork), "the objects of the head repository reached the base")
	assert.Equal(t, res.Commit.String(), id(t, f.repo, "refs/heads/main").String())
}

func TestMergeDryRun(t *testing.T) {
	w := gitcli.New()

	for _, mergeTree := range []bool{true, false} {
		name := "merge-tree"
		if !mergeTree {
			name = "read-tree"
		}
		t.Run(name, func(t *testing.T) {
			if mergeTree && !git.SupportGitMergeTree {
				t.Skip("git without merge-tree --write-tree")
			}
			saved := git.SupportGitMergeTree
			git.SupportGitMergeTree = mergeTree
			defer func() { git.SupportGitMergeTree = saved }()

			dry := func(f *mergeFixture) repostore.MergeRequest {
				req := f.request("")
				req.DryRun, req.Publish = true, nil
				req.WantDivergence = true
				req.ProtectedGlobs = []string{"b.txt", "docs/**"}
				return req
			}

			t.Run("mergeable", func(t *testing.T) {
				f := newMergeFixture(t)
				res, err := w.Merge(t.Context(), dry(f))
				require.NoError(t, err)
				assert.Equal(t, repostore.DryRunMergeable, res.Status)
				assert.Equal(t, f.root.String(), res.MergeBase.String())
				assert.Equal(t, f.topic2.String(), res.Head.String())
				assert.Equal(t, 2, res.Ahead)
				assert.Equal(t, 1, res.Behind)
				assert.Equal(t, []string{"b.txt"}, res.ProtectedFiles)
				assert.Nil(t, res.Commit)
				assert.Equal(t, f.main2.String(), id(t, f.repo, "refs/heads/main").String(), "a dry run moves nothing")
			})

			t.Run("conflict", func(t *testing.T) {
				f := newMergeFixture(t)
				commit(t, f.repo, f.root, "clash", "clash", put("a.txt", "a clash\n"))
				req := dry(f)
				req.HeadRev = "refs/heads/clash"
				res, err := w.Merge(t.Context(), req)
				require.NoError(t, err)
				assert.Equal(t, repostore.DryRunConflict, res.Status)
				assert.Equal(t, []string{"a.txt"}, res.Conflicts)
				assert.Empty(t, res.ProtectedFiles)
			})

			t.Run("ancestor", func(t *testing.T) {
				f := newMergeFixture(t)
				req := dry(f)
				req.HeadRev = f.root.String()
				res, err := w.Merge(t.Context(), req)
				require.NoError(t, err)
				assert.Equal(t, repostore.DryRunAncestor, res.Status)
				assert.Equal(t, f.root.String(), res.Head.String())
			})

			t.Run("empty", func(t *testing.T) {
				f := newMergeFixture(t)
				// an empty commit on the root: it changes nothing, and main is at the root
				emptyOnRoot := commit(t, f.repo, f.root, "empty", "empty")
				base := f.request("")
				base.DryRun, base.Publish = true, nil
				base.BaseRef, base.HeadRev = "refs/heads/empty-base", "refs/heads/empty"
				commit(t, f.repo, f.root, "empty-base", "same root")
				res, err := w.Merge(t.Context(), base)
				require.NoError(t, err)
				assert.Equal(t, repostore.DryRunEmpty, res.Status)
				assert.Equal(t, emptyOnRoot.String(), res.Head.String())
			})

			t.Run("missing head", func(t *testing.T) {
				f := newMergeFixture(t)
				req := dry(f)
				req.HeadRev = "refs/heads/gone"
				_, err := w.Merge(t.Context(), req)
				var missing *repostore.RefNotFoundError
				require.ErrorAs(t, err, &missing)
			})

			t.Run("head in another repository", func(t *testing.T) {
				f := newMergeFixture(t)
				fork := repostore.RepoRef{ID: 2, Owner: "o", Name: "fork", ObjectFormat: "sha1", Path: strings.TrimSuffix(f.repo.Path, "m.git") + "fork.git"}
				require.NoError(t, w.Fork(t.Context(), repostore.ForkRequest{Repo: fork, From: f.repo}))
				gitOut(t, fork.Path, "config", "core.hooksPath", fork.Path+"/hooks")
				feature := commit(t, fork, f.topic2, "feature", "only in fork", put("fork.txt", "fork\n"))
				req := dry(f)
				req.Head, req.HeadRev = fork, "refs/heads/feature"
				res, err := w.Merge(t.Context(), req)
				require.NoError(t, err)
				assert.Equal(t, repostore.DryRunMergeable, res.Status)
				assert.Equal(t, feature.String(), res.Head.String())
				assert.Equal(t, 3, res.Ahead)
				assert.False(t, hasObject(t, f.repo, feature), "a dry run does not copy objects into the base")
			})
		})
	}

	t.Run("divergence only", func(t *testing.T) {
		f := newMergeFixture(t)
		req := f.request("")
		req.DryRun, req.Publish, req.DivergenceOnly = true, nil, true
		res, err := w.Merge(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, 2, res.Ahead)
		assert.Equal(t, 1, res.Behind)
		assert.Equal(t, f.root.String(), res.MergeBase.String())
		assert.Zero(t, res.Status, "no mergeability check")
	})
}
