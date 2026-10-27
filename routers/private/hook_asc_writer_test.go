// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/private"
	ascwriter "forgejo.org/modules/repostore/asc"
	"forgejo.org/modules/web"
	"forgejo.org/services/ascsync"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A write that the hub's own writer makes to an asc repository (a web edit, a merge) reaches the hub's pre-receive hook
// from asc as the hub's service credential, with no person. The writer tells the hub whose change it is first, and the
// hook then runs the checks of a git push as that person: the permission to write the code, and the protected branches.

type writerPush struct {
	repo     *repo_model.Repository
	ref      string
	old, new string
}

// fastForward returns the repository 1 (user2/repo1) as an asc repository and the creation of a branch at the tip of
// master.
func fastForward(t *testing.T) writerPush {
	t.Helper()
	useAsc(t)
	repo := ascRepo(t, "git") // read the commits from the git directory first
	gr, err := git.OpenRepository(t.Context(), repo.RepoPath())
	require.NoError(t, err)
	t.Cleanup(func() { gr.Close() })
	tip, err := gr.GetBranchCommit("master")
	require.NoError(t, err)
	repo.StorageBackend = "asc"
	return writerPush{repo: repo, ref: "refs/heads/writer-branch", old: ascZeroID, new: tip.ID.String()}
}

// preReceive runs the whole hook for a call from asc with the given options.
func preReceive(t *testing.T, w writerPush, opts *private.HookOptions) (status int, body string) {
	t.Helper()
	gr, err := git.OpenRepository(t.Context(), w.repo.RepoPath())
	require.NoError(t, err)
	t.Cleanup(func() { gr.Close() })
	ctx, resp := contexttest.MockPrivateContext(t, "/")
	ctx.Repo = &app_context.Repository{Repository: w.repo, GitRepo: gr}
	web.SetForm(ctx, opts)
	HookPreReceive(ctx)
	return resp.Code, resp.Body.String()
}

func ascCall(w writerPush, userID int64) *private.HookOptions {
	return &private.HookOptions{
		UserID:       userID,
		PushTrigger:  ascsync.PushTriggerAsc,
		RefFullNames: []git.RefName{git.RefName(w.ref)},
		OldCommitIDs: []string{w.old},
		NewCommitIDs: []string{w.new},
	}
}

func remember(w writerPush, user, pr int64) ascsync.PushToken {
	return ascsync.RememberWriter(w.repo, user, pr, []ascsync.PushRef{{Name: w.ref, New: w.new}})
}

func TestWriterPreReceiveRunsTheChecksOfThePerson(t *testing.T) {
	w := fastForward(t)

	t.Run("a person who may write the code", func(t *testing.T) {
		tok := remember(w, 2, 0)
		defer tok.Forget()
		status, body := preReceive(t, w, ascCall(w, 0))
		assert.Equal(t, http.StatusOK, status, body)
	})

	t.Run("a person who may only read", func(t *testing.T) {
		tok := remember(w, 4, 0) // user4 reads the public repository and does not write it
		defer tok.Forget()
		status, body := preReceive(t, w, ascCall(w, 0))
		assert.Equal(t, http.StatusForbidden, status)
		assert.Contains(t, body, "is not allowed to push")
	})

	t.Run("nobody is remembered", func(t *testing.T) {
		status, _ := preReceive(t, w, ascCall(w, 0))
		assert.NotEqual(t, http.StatusOK, status, "a call with no person and no record is refused")
	})

	t.Run("a protected branch", func(t *testing.T) {
		require.NoError(t, unittest.PrepareTestDatabase())
		_, err := db.GetEngine(t.Context()).Insert(&git_model.ProtectedBranch{RepoID: w.repo.ID, RuleName: "writer-branch"})
		require.NoError(t, err)
		tok := remember(w, 2, 0)
		defer tok.Forget()
		status, body := preReceive(t, w, ascCall(w, 0))
		assert.Equal(t, http.StatusForbidden, status, "nobody pushes to a branch with no push right, not even the owner")
		assert.Contains(t, body, "protected branch")
	})
}

func TestWriterPreReceiveNeverTakesAnotherPerson(t *testing.T) {
	w := fastForward(t)

	t.Run("a call that names a person keeps that person", func(t *testing.T) {
		// asc named user4 (a push of user4's own). The writer's record of user2 is not used for it.
		tok := remember(w, 2, 0)
		defer tok.Forget()
		status, body := preReceive(t, w, ascCall(w, 4))
		assert.Equal(t, http.StatusForbidden, status, "user4 may not write, whoever else has a write recorded")
		assert.Contains(t, body, "user4")
	})

	t.Run("two people with the same write recorded are nobody", func(t *testing.T) {
		a, b := remember(w, 2, 0), remember(w, 4, 0)
		defer a.Forget()
		defer b.Forget()
		status, _ := preReceive(t, w, ascCall(w, 0))
		assert.NotEqual(t, http.StatusOK, status)
	})

	t.Run("another ref, another commit, or a git push of the person", func(t *testing.T) {
		tok := remember(w, 2, 0)
		defer tok.Forget()
		other := ascCall(w, 0)
		other.RefFullNames = []git.RefName{"refs/heads/other"}
		status, _ := preReceive(t, w, other)
		assert.NotEqual(t, http.StatusOK, status, "another ref")
		other = ascCall(w, 0)
		other.NewCommitIDs = []string{w.old}
		status, _ = preReceive(t, w, other)
		assert.NotEqual(t, http.StatusOK, status, "another commit")

		// A record made for a git push (RememberPusher) is not a writer's record: pre-receive does not use it.
		tok.Forget()
		gitTok, err := ascsync.RememberPusher(w.repo, 2, []ascsync.PushRef{{Name: w.ref, Old: w.old, New: w.new}})
		require.NoError(t, err)
		defer gitTok.Forget()
		status, _ = preReceive(t, w, ascCall(w, 0))
		assert.NotEqual(t, http.StatusOK, status, "a git-origin record gives no person to a write of the writer")
	})

	t.Run("a mix of refs of different writes", func(t *testing.T) {
		a := ascsync.RememberWriter(w.repo, 2, 0, []ascsync.PushRef{{Name: "refs/heads/a", New: w.new}})
		b := ascsync.RememberWriter(w.repo, 4, 0, []ascsync.PushRef{{Name: "refs/heads/b", New: w.new}})
		defer a.Forget()
		defer b.Forget()
		opts := ascCall(w, 0)
		opts.RefFullNames = []git.RefName{"refs/heads/a", "refs/heads/b"}
		opts.OldCommitIDs = []string{w.old, w.old}
		opts.NewCommitIDs = []string{w.new, w.new}
		status, _ := preReceive(t, w, opts)
		assert.NotEqual(t, http.StatusOK, status)
	})
}

func TestWriterRecordCarriesThePullRequestAndEndsAtPostReceive(t *testing.T) {
	w := fastForward(t)
	refs := []ascsync.PushRef{{Name: w.ref, Old: w.old, New: w.new}}

	tok := remember(w, 2, 7)
	defer tok.Forget()
	opts := ascCall(w, 0)
	ctx, _ := contexttest.MockPrivateContext(t, "/")
	ctx.Repo = &app_context.Repository{Repository: w.repo}
	attributeAscWrite(ctx, opts)
	assert.Equal(t, int64(2), opts.UserID)
	assert.Equal(t, int64(7), opts.PullRequestID, "a merge keeps its pull request, so the merge rules of the branch apply to it")

	// Pre-receive does not use the record up; post-receive claims it once.
	again := ascCall(w, 0)
	attributeAscWrite(ctx, again)
	assert.Equal(t, int64(2), again.UserID, "pre-receive may be asked again")
	id, ok := ascsync.ClaimPusher(w.repo, refs)
	assert.True(t, ok)
	assert.Equal(t, int64(2), id)
	_, ok = ascsync.ClaimPusher(w.repo, refs)
	assert.False(t, ok, "attributed once")
	gone := ascCall(w, 0)
	attributeAscWrite(ctx, gone)
	assert.Zero(t, gone.UserID, "after the claim nobody is attributed")

	// Not for a wiki, not when asc is off, not for a call that names a person.
	tok2 := remember(w, 2, 0)
	defer tok2.Forget()
	wiki := ascCall(w, 0)
	wiki.IsWiki = true
	attributeAscWrite(ctx, wiki)
	assert.Zero(t, wiki.UserID)
	plain := ascCall(w, 0)
	plain.PushTrigger = ""
	attributeAscWrite(ctx, plain)
	assert.Zero(t, plain.UserID, "only a call from asc's outbox or gate")
}

// The writer names the repository "owner/name"; the hub records the write under the repository that stands for it.
func TestWriterNoteResolvesTheRepository(t *testing.T) {
	w := fastForward(t)
	forget := ascwriter.NotePusher(t.Context(), "user2/repo1", 2, 4, []ascwriter.PushedRef{{Name: w.ref, New: w.new}})
	require.NotNil(t, forget)
	user, pr, ok := ascsync.PeekWriter(w.repo, []ascsync.PushRef{{Name: w.ref, Old: w.old, New: w.new}})
	assert.True(t, ok)
	assert.Equal(t, int64(2), user)
	assert.Equal(t, int64(4), pr)
	forget()
	_, _, ok = ascsync.PeekWriter(w.repo, []ascsync.PushRef{{Name: w.ref, Old: w.old, New: w.new}})
	assert.False(t, ok)

	refs := []ascwriter.PushedRef{{Name: w.ref, New: w.new}}
	assert.Nil(t, ascwriter.NotePusher(t.Context(), "no-such/repo", 2, 0, refs))
	assert.Nil(t, ascwriter.NotePusher(t.Context(), "nosep", 2, 0, refs))
}
