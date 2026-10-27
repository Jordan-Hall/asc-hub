// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asc_test

import (
	"context"
	"testing"

	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/asc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

type notedWrite struct {
	repo      string
	pusher    int64
	pr        int64
	refs      []asc.PushedRef
	forgotten bool
}

// useNotes installs a recorder for asc.NotePusher for one test and returns what it recorded.
func useNotes(t *testing.T) *[]*notedWrite {
	t.Helper()
	var notes []*notedWrite
	old := asc.NotePusher
	asc.NotePusher = func(_ context.Context, repo string, pusher, pr int64, refs []asc.PushedRef) func() {
		n := &notedWrite{repo: repo, pusher: pusher, pr: pr, refs: refs}
		notes = append(notes, n)
		return func() { n.forgotten = true }
	}
	t.Cleanup(func() { asc.NotePusher = old })
	return &notes
}

// The hub's hooks are told whose change a write is before asc is asked to make it, and are told to forget it when the
// write fails.
func TestWritesNoteTheirPusher(t *testing.T) {
	repo := ref("Owner", "Repo")
	env := []string{"GITEA_PUSHER_ID=2", "GITEA_PR_ID=9"}

	t.Run("a commit", func(t *testing.T) {
		notes := useNotes(t)
		f := newFake()
		_, err := newWriter(t, f).CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: gid(0x11), Ops: []repostore.FileOp{put("a", "a")},
			Meta:   repostore.CommitMeta{Committer: sig("Bob"), Message: "m"},
			Update: branch(repo, "main"), HookEnv: env,
		})
		require.NoError(t, err)
		require.Len(t, *notes, 1)
		n := (*notes)[0]
		assert.Equal(t, "owner/repo", n.repo)
		assert.Equal(t, int64(2), n.pusher)
		assert.Equal(t, int64(9), n.pr)
		assert.Equal(t, []asc.PushedRef{{Name: "refs/heads/main", New: gid(0x70).String()}}, n.refs)
		assert.False(t, n.forgotten, "a write that succeeded stays noted until post-receive claims it")
	})

	t.Run("a write that asc refuses is forgotten", func(t *testing.T) {
		notes := useNotes(t)
		f := newFake()
		f.updateErr = failure(codes.FailedPrecondition, "HOOK_REJECTED", "no")
		_, err := newWriter(t, f).CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: gid(0x11), Ops: []repostore.FileOp{put("a", "a")},
			Meta:   repostore.CommitMeta{Committer: sig("Bob"), Message: "m"},
			Update: branch(repo, "main"), HookEnv: env,
		})
		require.Error(t, err)
		require.Len(t, *notes, 1)
		assert.True(t, (*notes)[0].forgotten)
	})

	t.Run("no person in the environment: nothing is noted", func(t *testing.T) {
		notes := useNotes(t)
		f := newFake()
		_, err := newWriter(t, f).CreateCommit(t.Context(), repostore.CommitRequest{
			Repo: repo, Parent: gid(0x11), Ops: []repostore.FileOp{put("a", "a")},
			Meta: repostore.CommitMeta{Committer: sig("Bob"), Message: "m"}, Update: branch(repo, "main"),
		})
		require.NoError(t, err)
		assert.Empty(t, *notes)
	})

	t.Run("refs updated together: a move, a creation and a deletion", func(t *testing.T) {
		notes := useNotes(t)
		f := newFake()
		f.refs["o/r|refs/heads/gone"] = gid(0x02)
		_, err := newWriter(t, f).UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: ref("o", "r"), HookEnv: []string{"GITEA_PUSHER_ID=5"},
			Updates: []repostore.RefUpdateSpec{
				{Name: "refs/heads/new", NewRev: gid(0x44).String()},
				{Name: "refs/heads/gone", Delete: true},
			},
		})
		require.NoError(t, err)
		require.Len(t, *notes, 1)
		assert.Equal(t, []asc.PushedRef{{Name: "refs/heads/new", New: gid(0x44).String()}, {Name: "refs/heads/gone"}}, (*notes)[0].refs)
		assert.Equal(t, int64(5), (*notes)[0].pusher)
	})
}
