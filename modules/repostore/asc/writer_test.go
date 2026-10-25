// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/repostore/asc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func put(path, content string) repostore.FileOp {
	return repostore.FileOp{Kind: repostore.OpPut, Path: path, Content: strings.NewReader(content)}
}

func branch(r repostore.RepoRef, name string) *repostore.RefUpdate {
	return &repostore.RefUpdate{Repo: r, Ref: "refs/heads/" + name}
}

func TestCreateCommitBuildsThenPublishes(t *testing.T) {
	f := newFake()
	w := newWriter(t, f)
	repo := ref("Owner", "Repo")

	var order []string
	res, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
		Repo:   repo,
		Parent: gid(0x11),
		Ops: []repostore.FileOp{
			put("a.txt", "hello"),
			{Kind: repostore.OpPut, Path: "run.sh", Mode: git.EntryModeExec, Content: strings.NewReader("#!/bin/sh")},
			{Kind: repostore.OpDelete, Path: "old.txt"},
		},
		Meta:   repostore.CommitMeta{Author: sig("Ann"), Committer: sig("Bob"), Message: "subject", Signoff: true},
		Update: branch(repo, "main"),
		BeforePublish: func(_ context.Context, in repostore.PublishInput) error {
			order = append(order, "before")
			assert.Equal(t, gid(0x70).String(), in.Commit.String())
			assert.Equal(t, gid(0x11).String(), in.Base.String())
			assert.Empty(t, in.GitDir)
			assert.Equal(t, 0, f.called("UpdateRefs"), "no ref moves before BeforePublish returns")
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, gid(0x70).String(), res.Commit.String())
	assert.Equal(t, gid(0x55).String(), res.Tree.String())
	assert.Equal(t, []string{"before"}, order)

	creq := f.last("Commit").(*ascv1.CommitRequest)
	assert.Equal(t, "owner/repo", creq.GetRepo().GetName())
	assert.Equal(t, ascv1.IdMode_ID_MODE_ASC, creq.GetRepo().GetIdMode())
	assert.Equal(t, hexOf(0x11), creq.GetParent())
	assert.Empty(t, creq.GetUpdateRef(), "the commit is built without a ref")
	assert.Equal(t, "subject\n\nSigned-off-by: Bob <bob@example.com>\n", string(creq.GetMessage()))
	assert.Equal(t, "Ann", string(creq.GetAuthor().GetName()))
	assert.Equal(t, "Bob", string(creq.GetCommitter().GetName()))
	require.Len(t, creq.GetOps(), 3)
	assert.Equal(t, uint32(0o100644), creq.GetOps()[0].GetMode())
	assert.Equal(t, "hello", string(creq.GetOps()[0].GetPutContent()))
	assert.Equal(t, uint32(0o100755), creq.GetOps()[1].GetMode())
	assert.True(t, creq.GetOps()[2].GetDelete())

	ureq := f.last("UpdateRefs").(*ascv1.UpdateRefsRequest)
	assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FAST_FORWARD, ureq.GetMode())
	assert.Equal(t, ascv1.HookMode_HOOK_MODE_RUN, ureq.GetHooks())
	require.Len(t, ureq.GetUpdates(), 1)
	assert.Equal(t, "refs/heads/main", ureq.GetUpdates()[0].GetName())
	assert.True(t, ureq.GetUpdates()[0].GetAny())
	assert.Equal(t, ascid(0x70), ureq.GetUpdates()[0].GetNew().GetChangeset())
	assert.Zero(t, f.called("CopyClosure"))
}

func TestCreateCommitRootNeedsNoParentAndNoUpdateLeavesTheCommitUnpublished(t *testing.T) {
	f := newFake()
	w := newWriter(t, f)
	repo := ref("o", "r")
	res, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: repo, Ops: []repostore.FileOp{put("x", "x")}, Meta: repostore.CommitMeta{Committer: sig("Bob"), Message: "root"},
	})
	require.NoError(t, err)
	assert.NotNil(t, res.Commit)
	assert.Empty(t, f.last("Commit").(*ascv1.CommitRequest).GetParent())
	assert.Zero(t, f.called("UpdateRefs"))
	assert.Zero(t, f.called("GitMap"), "a root commit needs no id mapping")
	assert.Equal(t, "Bob", string(f.last("Commit").(*ascv1.CommitRequest).GetAuthor().GetName()), "no author means the committer")
}

func TestCreateCommitBeforePublishErrorStopsTheRequest(t *testing.T) {
	f := newFake()
	w := newWriter(t, f)
	repo := ref("o", "r")
	stop := errors.New("lfs said no")
	_, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: repo, Parent: gid(0x11), Ops: []repostore.FileOp{put("x", "x")},
		Meta: repostore.CommitMeta{Committer: sig("Bob"), Message: "m"}, Update: branch(repo, "main"),
		BeforePublish: func(context.Context, repostore.PublishInput) error { return stop },
	})
	require.ErrorIs(t, err, stop)
	assert.Zero(t, f.called("UpdateRefs"))
}

func TestCreateCommitPublishErrors(t *testing.T) {
	repo := ref("o", "r")
	req := func() repostore.CommitRequest {
		return repostore.CommitRequest{
			Repo: repo, Parent: gid(0x11), Ops: []repostore.FileOp{put("x", "x")},
			Meta: repostore.CommitMeta{Committer: sig("Bob"), Message: "m"}, Update: branch(repo, "main"),
		}
	}
	t.Run("not a fast-forward", func(t *testing.T) {
		f := newFake()
		f.updateErr = failure(codes.FailedPrecondition, "NOT_FAST_FORWARD", "not a fast-forward")
		_, err := newWriter(t, f).CreateCommit(t.Context(), req())
		var stale *git.ErrPushOutOfDate
		require.ErrorAs(t, err, &stale)
	})
	t.Run("a hook refuses", func(t *testing.T) {
		f := newFake()
		f.updateErr = failure(codes.FailedPrecondition, "HOOK_REJECTED", "protected branch\nrequires review")
		_, err := newWriter(t, f).CreateCommit(t.Context(), req())
		var rejected *git.ErrPushRejected
		require.ErrorAs(t, err, &rejected)
		assert.Equal(t, "protected branch\nrequires review", rejected.Message)
	})
	t.Run("a forced update", func(t *testing.T) {
		f := newFake()
		r := req()
		r.Update.Force = true
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		require.NoError(t, err)
		assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FORCE, f.last("UpdateRefs").(*ascv1.UpdateRefsRequest).GetMode())
	})
	t.Run("another repository gets the closure first", func(t *testing.T) {
		f := newFake()
		r := req()
		r.Update = branch(ref("o", "fork"), "main")
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		require.NoError(t, err)
		cc := f.last("CopyClosure").(*ascv1.CopyClosureRequest)
		assert.Equal(t, "o/r", cc.GetFrom().GetName())
		assert.Equal(t, "o/fork", cc.GetTo().GetName())
		assert.Equal(t, "o/fork", f.last("UpdateRefs").(*ascv1.UpdateRefsRequest).GetRepo().GetName())
	})
}

func TestCreateCommitRefusals(t *testing.T) {
	repo := ref("o", "r")
	base := func() repostore.CommitRequest {
		return repostore.CommitRequest{
			Repo: repo, Parent: gid(0x11), Ops: []repostore.FileOp{put("x", "x")},
			Meta: repostore.CommitMeta{Committer: sig("Bob"), Message: "m"}, Update: branch(repo, "main"),
		}
	}
	t.Run("a signing decision cannot be honoured", func(t *testing.T) {
		f := newFake()
		r := base()
		r.Meta.Sign = func(context.Context, repostore.SignInput) (*repostore.SignDecision, error) {
			return &repostore.SignDecision{KeyID: "k"}, nil
		}
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		require.ErrorIs(t, err, repostore.ErrUnsupported)
		assert.Empty(t, f.calls, "nothing is sent")
	})
	t.Run("a SignFunc that declines lets the commit through", func(t *testing.T) {
		f := newFake()
		r := base()
		var in repostore.SignInput
		r.Meta.Sign = func(_ context.Context, got repostore.SignInput) (*repostore.SignDecision, error) {
			in = got
			return nil, nil
		}
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		require.NoError(t, err)
		assert.Equal(t, repo.Path, in.GitDir)
		assert.Equal(t, gid(0x11).String(), in.Base.String())
	})
	t.Run("a submodule", func(t *testing.T) {
		f := newFake()
		r := base()
		r.Ops = []repostore.FileOp{{Kind: repostore.OpPut, Path: "sub", Mode: git.EntryModeCommit, Content: strings.NewReader(strings.Repeat("a", 40))}}
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		require.ErrorIs(t, err, repostore.ErrUnsupported)
	})
	t.Run("too much content", func(t *testing.T) {
		f := newFake()
		r := base()
		r.Ops = []repostore.FileOp{{Kind: repostore.OpPut, Path: "big", Content: bytes.NewReader(make([]byte, asc.MaxCommitBytes+1))}}
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		require.ErrorIs(t, err, asc.ErrTooLarge)
		assert.Empty(t, f.calls)
	})
	t.Run("sha256", func(t *testing.T) {
		f := newFake()
		r := base()
		r.Repo.ObjectFormat = "sha256"
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		// Either the request is refused as unusable (the format is not registered in this binary) or asc refuses it.
		require.True(t, errors.Is(err, repostore.ErrUnsupported) || errors.Is(err, repostore.ErrInvalidRequest), "%v", err)
		assert.Empty(t, f.calls)
	})
	t.Run("an invalid request fails before any call", func(t *testing.T) {
		f := newFake()
		r := base()
		r.Ops = []repostore.FileOp{put("../x", "x")}
		_, err := newWriter(t, f).CreateCommit(t.Context(), r)
		var bad *repostore.InvalidPathError
		require.ErrorAs(t, err, &bad)
		assert.Empty(t, f.calls)
	})
	t.Run("a parent asc does not know", func(t *testing.T) {
		f := newFake()
		f.unknown[gid(0x11).String()] = true
		_, err := newWriter(t, f).CreateCommit(t.Context(), base())
		var missing *repostore.RefNotFoundError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, gid(0x11).String(), missing.Ref)
		assert.Zero(t, f.called("Commit"))
	})
	t.Run("a deleted file that is not there", func(t *testing.T) {
		f := newFake()
		f.commitErr = failure(codes.NotFound, "", "old.txt does not exist")
		_, err := newWriter(t, f).CreateCommit(t.Context(), base())
		var missing *repostore.FileNotFoundError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, "old.txt", missing.Path)
	})
	t.Run("a path the policy refuses", func(t *testing.T) {
		f := newFake()
		f.commitErr = failure(codes.FailedPrecondition, "PATH_POLICY", "path policy", "aux")
		_, err := newWriter(t, f).CreateCommit(t.Context(), base())
		var bad *repostore.InvalidPathError
		require.ErrorAs(t, err, &bad)
		assert.Equal(t, "aux", bad.Path)
	})
}

func TestCreateCommitPreview(t *testing.T) {
	f := newFake()
	f.diffText[ascv1.DiffFormat_DIFF_FORMAT_NUMSTAT] = "3\t1\ta.txt\n-\t-\timg.png\n"
	f.diffText[ascv1.DiffFormat_DIFF_FORMAT_PATCH] = "diff --git a/a.txt b/a.txt\n"
	w := newWriter(t, f)
	var out bytes.Buffer
	res, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: ref("o", "r"), Parent: gid(0x11), Ops: []repostore.FileOp{put("a.txt", "x")}, Preview: &out,
	})
	require.NoError(t, err)
	assert.Nil(t, res.Commit)
	assert.Equal(t, git.DiffStat{NumFiles: 2, Additions: 3, Deletions: 1}, res.Stat)
	assert.Equal(t, "diff --git a/a.txt b/a.txt\n", out.String())
	assert.Zero(t, f.called("UpdateRefs"))
	diff := f.last("Diff").(*ascv1.DiffRequest)
	assert.Equal(t, ascv1.IdMode_ID_MODE_GIT, diff.GetRepo().GetIdMode())
	assert.Equal(t, gid(0x11).String(), diff.GetBase())
	assert.Equal(t, gid(0x70).String(), diff.GetHead())
}

func TestPickRevertAndPatch(t *testing.T) {
	repo := ref("o", "r")
	meta := repostore.CommitMeta{Author: sig("Ann"), Committer: sig("Bob"), Message: "picked"}

	t.Run("cherry-pick", func(t *testing.T) {
		f := newFake()
		res, err := newWriter(t, f).CherryPick(t.Context(), repostore.PickRequest{
			Repo: repo, Parent: gid(0x11), Commit: gid(0x22), Meta: meta, Update: branch(repo, "main"),
		})
		require.NoError(t, err)
		assert.Equal(t, gid(0x70).String(), res.Commit.String())
		areq := f.last("Apply").(*ascv1.ApplyRequest)
		assert.Equal(t, ascv1.ApplyMode_APPLY_MODE_CHERRY_PICK, areq.GetMode())
		assert.Equal(t, hexOf(0x11), areq.GetOnto())
		assert.Equal(t, hexOf(0x22), areq.GetChangeset())
		assert.Empty(t, areq.GetUpdateRef())
		assert.Equal(t, 1, f.called("UpdateRefs"))
	})
	t.Run("revert", func(t *testing.T) {
		f := newFake()
		_, err := newWriter(t, f).Revert(t.Context(), repostore.PickRequest{Repo: repo, Parent: gid(0x11), Commit: gid(0x22), Meta: meta})
		require.NoError(t, err)
		assert.Equal(t, ascv1.ApplyMode_APPLY_MODE_REVERT, f.last("Apply").(*ascv1.ApplyRequest).GetMode())
		assert.Zero(t, f.called("UpdateRefs"))
	})
	t.Run("a conflict is a ConflictError and moves nothing", func(t *testing.T) {
		f := newFake()
		f.applyResp = &ascv1.ApplyResponse{Conflicts: [][]byte{[]byte("a.txt")}}
		_, err := newWriter(t, f).CherryPick(t.Context(), repostore.PickRequest{
			Repo: repo, Parent: gid(0x11), Commit: gid(0x22), Meta: meta, Update: branch(repo, "main"),
		})
		var conflict *repostore.ConflictError
		require.ErrorAs(t, err, &conflict)
		assert.Equal(t, []string{"a.txt"}, conflict.Files)
		assert.Zero(t, f.called("UpdateRefs"))
	})
	t.Run("patch", func(t *testing.T) {
		f := newFake()
		_, err := newWriter(t, f).ApplyPatch(t.Context(), repostore.PatchRequest{
			Repo: repo, Parent: gid(0x11), Patch: "diff --git a/x b/x\n", Meta: meta, Update: branch(repo, "feature"),
		})
		require.NoError(t, err)
		areq := f.last("Apply").(*ascv1.ApplyRequest)
		assert.Equal(t, ascv1.ApplyMode_APPLY_MODE_PATCH, areq.GetMode())
		assert.Equal(t, "diff --git a/x b/x\n", string(areq.GetPatch()))
		assert.Equal(t, "refs/heads/feature", f.last("UpdateRefs").(*ascv1.UpdateRefsRequest).GetUpdates()[0].GetName())
	})
}

func TestUpdateRefs(t *testing.T) {
	repo := ref("o", "r")
	t.Run("create, move and delete", func(t *testing.T) {
		f := newFake()
		f.refs["o/r|refs/heads/old"] = gid(0x01)
		f.refs["o/r|refs/heads/gone"] = gid(0x02)
		f.refs["o/r|refs/heads/src"] = gid(0x33)
		res, err := newWriter(t, f).UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo,
			Updates: []repostore.RefUpdateSpec{
				{Name: "refs/heads/new", NewRev: "refs/heads/src"},
				{Name: "refs/heads/old", NewRev: gid(0x44).String()},
				{Name: "refs/heads/gone", Delete: true},
			},
		})
		require.NoError(t, err)
		require.Len(t, res, 3)
		assert.Nil(t, res[0].Old)
		assert.Equal(t, gid(0x33).String(), res[0].New.String())
		assert.Equal(t, gid(0x01).String(), res[1].Old.String())
		assert.Equal(t, gid(0x44).String(), res[1].New.String())
		assert.Equal(t, gid(0x02).String(), res[2].Old.String())
		assert.Nil(t, res[2].New)
		ureq := f.last("UpdateRefs").(*ascv1.UpdateRefsRequest)
		assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FAST_FORWARD, ureq.GetMode())
		assert.True(t, ureq.GetUpdates()[2].GetDelete())
		assert.Zero(t, f.called("CopyClosure"))
	})
	t.Run("forced", func(t *testing.T) {
		f := newFake()
		_, err := newWriter(t, f).UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo, Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/x", NewRev: gid(0x44).String(), Force: true}},
		})
		require.NoError(t, err)
		assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FORCE, f.last("UpdateRefs").(*ascv1.UpdateRefsRequest).GetMode())
	})
	t.Run("forced and unforced updates cannot share a call", func(t *testing.T) {
		f := newFake()
		_, err := newWriter(t, f).UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo, Updates: []repostore.RefUpdateSpec{
				{Name: "refs/heads/x", NewRev: gid(0x44).String(), Force: true},
				{Name: "refs/heads/y", NewRev: gid(0x44).String()},
			},
		})
		require.ErrorIs(t, err, repostore.ErrUnsupported)
		assert.Zero(t, f.called("UpdateRefs"))
	})
	t.Run("from another repository", func(t *testing.T) {
		f := newFake()
		from := ref("o", "fork")
		_, err := newWriter(t, f).UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo, From: &from, Updates: []repostore.RefUpdateSpec{{Name: "refs/pull/7/head", NewRev: gid(0x44).String(), Force: true}},
		})
		require.NoError(t, err)
		cc := f.last("CopyClosure").(*ascv1.CopyClosureRequest)
		assert.Equal(t, "o/fork", cc.GetFrom().GetName())
		assert.Equal(t, "o/r", cc.GetTo().GetName())
		assert.Equal(t, [][]byte{ascid(0x44)}, cc.GetChangesets())
	})
	t.Run("a revision that names nothing", func(t *testing.T) {
		f := newFake()
		_, err := newWriter(t, f).UpdateRefs(t.Context(), repostore.UpdateRefsRequest{
			Repo: repo, Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/x", NewRev: "refs/heads/nothing"}},
		})
		var missing *repostore.RefNotFoundError
		require.ErrorAs(t, err, &missing)
		assert.Zero(t, f.called("UpdateRefs"))
	})
}

func TestAdmin(t *testing.T) {
	repo := ref("Owner", "Repo")
	t.Run("init", func(t *testing.T) {
		f := newFake()
		require.NoError(t, newWriter(t, f).Init(t.Context(), repostore.InitRequest{Repo: repo, DefaultBranch: "main"}))
		req := f.last("RepoAdmin").(*ascv1.RepoAdminRequest)
		assert.Equal(t, "owner/repo", req.GetRepo().GetName())
		assert.Equal(t, "main", req.GetInit().GetDefaultBranch())
		assert.Equal(t, "primary", req.GetInit().GetMode())
	})
	t.Run("init of an existing repository", func(t *testing.T) {
		f := newFake()
		f.adminErr = failure(codes.AlreadyExists, "", "repository exists")
		err := newWriter(t, f).Init(t.Context(), repostore.InitRequest{Repo: repo})
		require.ErrorIs(t, err, repostore.ErrExists)
	})
	t.Run("a wiki is a repository of its own", func(t *testing.T) {
		f := newFake()
		require.NoError(t, newWriter(t, f).Init(t.Context(), repostore.InitRequest{Repo: ref("o", "r.wiki")}))
		assert.Equal(t, "o/r.wiki", f.last("RepoAdmin").(*ascv1.RepoAdminRequest).GetRepo().GetName())
	})
	t.Run("fork", func(t *testing.T) {
		f := newFake()
		require.NoError(t, newWriter(t, f).Fork(t.Context(), repostore.ForkRequest{Repo: ref("u", "fork"), From: repo}))
		req := f.last("RepoAdmin").(*ascv1.RepoAdminRequest)
		assert.Equal(t, "u/fork", req.GetRepo().GetName())
		assert.Equal(t, "owner/repo", req.GetFork().GetFrom().GetName())
	})
	t.Run("a fork of one branch is not served", func(t *testing.T) {
		f := newFake()
		err := newWriter(t, f).Fork(t.Context(), repostore.ForkRequest{Repo: ref("u", "fork"), From: repo, SingleBranch: "main"})
		require.ErrorIs(t, err, repostore.ErrUnsupported)
		assert.Empty(t, f.calls)
	})
	t.Run("delete", func(t *testing.T) {
		f := newFake()
		require.NoError(t, newWriter(t, f).Delete(t.Context(), repo))
		assert.True(t, f.last("RepoAdmin").(*ascv1.RepoAdminRequest).GetDelete())
	})
	t.Run("deleting a repository that does not exist is not an error", func(t *testing.T) {
		f := newFake()
		f.adminErr = failure(codes.NotFound, "", "no such repository")
		require.NoError(t, newWriter(t, f).Delete(t.Context(), repo))
	})
	t.Run("a refused delete is an error", func(t *testing.T) {
		f := newFake()
		f.adminErr = failure(codes.PermissionDenied, "", "denied")
		require.Error(t, newWriter(t, f).Delete(t.Context(), repo))
	})
	t.Run("size", func(t *testing.T) {
		f := newFake()
		f.adminResp = &ascv1.RepoAdminResponse{LogicalSize: 100, UniqueSize: 40}
		size, err := newWriter(t, f).Size(t.Context(), repo)
		require.NoError(t, err)
		assert.Equal(t, repostore.Size{Logical: 100, Unique: 40}, size)
	})
	t.Run("verify and rename", func(t *testing.T) {
		f := newFake()
		f.adminResp = &ascv1.RepoAdminResponse{Report: "ok"}
		w := newWriter(t, f)
		report, err := w.Verify(t.Context(), repo)
		require.NoError(t, err)
		assert.Equal(t, "ok", report)
		require.NoError(t, w.Rename(t.Context(), repo, ref("New", "Name")))
		assert.Equal(t, "new/name", f.last("RepoAdmin").(*ascv1.RepoAdminRequest).GetRenameTo())
	})
	t.Run("bad names", func(t *testing.T) {
		f := newFake()
		for _, bad := range []repostore.RepoRef{ref("o/x", "r"), ref("o", ".."), ref("", "r"), ref("o", "r r")} {
			err := newWriter(t, f).Init(t.Context(), repostore.InitRequest{Repo: bad})
			require.Error(t, err, bad.Owner+"/"+bad.Name)
		}
		assert.Empty(t, f.calls)
	})
}

func TestNotConfigured(t *testing.T) {
	asc.Reset()
	w, err := repostore.Get(asc.Name)
	require.NoError(t, err, "the package registers its Writer")
	_, err = w.Size(t.Context(), ref("o", "r"))
	require.ErrorIs(t, err, asc.ErrNotConfigured)
}

func TestCredentialErrorStopsTheCall(t *testing.T) {
	f := newFake()
	w, err := asc.New(f, asc.Options{Auth: func(context.Context) (context.Context, error) { return nil, errors.New("no token") }})
	require.NoError(t, err)
	require.Error(t, w.Delete(t.Context(), ref("o", "r")))
	assert.Empty(t, f.calls)
}
