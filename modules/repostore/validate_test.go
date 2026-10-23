// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repostore_test

import (
	"errors"
	"strings"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	sig    = &repostore.Signature{Name: "A", Email: "a@example.com"}
	meta   = repostore.CommitMeta{Committer: sig, Message: "m"}
	repo   = repostore.RepoRef{Owner: "o", Name: "r", ObjectFormat: "sha1", Path: "/srv/o/r.git"}
	someID = git.MustIDFromString("1111111111111111111111111111111111111111")
)

func TestValidateRefName(t *testing.T) {
	good := []string{"refs/heads/main", "refs/heads/feature/x-1", "refs/pull/12/head", "refs/tags/v1.0", "refs/heads/ü"}
	for _, name := range good {
		assert.NoError(t, repostore.ValidateRefName(name), name)
	}
	bad := []string{
		"", "main", "heads/main", "refs/heads/", "refs/heads/a..b", "refs/heads/a b", "refs/heads/a:b", "refs/heads/a~1",
		"refs/heads/a^", "refs/heads/a?", "refs/heads/a*", "refs/heads/a[", "refs/heads/a\\b", "refs/heads/.hidden",
		"refs/heads/x.lock", "refs/heads/a//b", "refs/heads/a@{b", "refs/heads/a\nb", "refs/heads/a\x00b", "refs/heads/end.",
		"refs/heads/" + strings.Repeat("a", 2000),
	}
	for _, name := range bad {
		err := repostore.ValidateRefName(name)
		require.Error(t, err, name)
		assert.ErrorIs(t, err, repostore.ErrInvalidRequest, name)
	}
}

func TestValidateRev(t *testing.T) {
	assert.NoError(t, repostore.ValidateRev(someID.String()))
	assert.NoError(t, repostore.ValidateRev("refs/heads/main"))
	assert.Error(t, repostore.ValidateRev(""))
	assert.Error(t, repostore.ValidateRev("-x"))
	assert.Error(t, repostore.ValidateRev("main"))
	assert.Error(t, repostore.ValidateRev("HEAD"))
}

func TestValidateFilePath(t *testing.T) {
	for _, p := range []string{"a", "a/b.txt", "dir/.gitignore", "a/.github/x", "ü/ö"} {
		assert.NoError(t, repostore.ValidateFilePath(p), p)
	}
	for _, p := range []string{"", "/a", "a/", "a//b", "./a", "a/./b", "../a", "a/../b", ".git/config", "a/.GIT/x", "a\x00b", strings.Repeat("a", repostore.MaxPathBytes+1)} {
		assert.Error(t, repostore.ValidateFilePath(p), p)
	}
	var invalid *repostore.InvalidPathError
	assert.True(t, errors.As(repostore.ValidateFilePath("../a"), &invalid))
}

func TestRepoRef(t *testing.T) {
	assert.NoError(t, repo.Validate())
	assert.NoError(t, repo.RequirePath())
	assert.NoError(t, repo.RequireObjectFormat())

	assert.Error(t, repostore.RepoRef{Owner: "o"}.Validate(), "no name")
	assert.Error(t, repostore.RepoRef{Name: "r\n"}.Validate(), "control character in name")
	assert.Error(t, repostore.RepoRef{Name: "r", ObjectFormat: "md5"}.Validate(), "unknown format")
	assert.Error(t, repostore.RepoRef{Name: "r"}.RequirePath(), "no path")
	assert.Error(t, repostore.RepoRef{Name: "r", Path: "relative/r.git"}.RequirePath(), "relative path")
	assert.Error(t, repostore.RepoRef{Name: "r", Path: "/srv/r.git\n/etc"}.RequirePath(), "a newline would add an alternates line")
	assert.Error(t, repostore.RepoRef{Name: "r"}.RequireObjectFormat(), "no format")
}

func TestCommitRequestValidate(t *testing.T) {
	ok := func() repostore.CommitRequest {
		return repostore.CommitRequest{
			Repo:   repo,
			Parent: someID,
			Ops:    []repostore.FileOp{{Kind: repostore.OpPut, Path: "a.txt", Content: strings.NewReader("x")}},
			Meta:   meta,
			Update: &repostore.RefUpdate{Repo: repo, Ref: "refs/heads/main"},
		}
	}
	req := ok()
	require.NoError(t, req.Validate())

	cases := map[string]func(*repostore.CommitRequest){
		"no committer":     func(r *repostore.CommitRequest) { r.Meta.Committer = nil },
		"bad ref":          func(r *repostore.CommitRequest) { r.Update.Ref = "main" },
		"put without data": func(r *repostore.CommitRequest) { r.Ops[0].Content = nil },
		"tree mode":        func(r *repostore.CommitRequest) { r.Ops[0].Mode = git.EntryModeTree },
		"unknown kind":     func(r *repostore.CommitRequest) { r.Ops[0].Kind = 0 },
		"bad path":         func(r *repostore.CommitRequest) { r.Ops[0].Path = "../x" },
		"delete from root": func(r *repostore.CommitRequest) {
			r.Parent = nil
			r.Ops = []repostore.FileOp{{Kind: repostore.OpDelete, Path: "a"}}
		},
		"preview + publish": func(r *repostore.CommitRequest) { r.Preview = &strings.Builder{} },
		"nul in message":    func(r *repostore.CommitRequest) { r.Meta.Message = "a\x00b" },
		"huge message":      func(r *repostore.CommitRequest) { r.Meta.Message = strings.Repeat("a", repostore.MaxMessageBytes+1) },
		"too many ops": func(r *repostore.CommitRequest) {
			r.Ops = make([]repostore.FileOp, repostore.MaxFileOps+1)
		},
		"root commit needs a format": func(r *repostore.CommitRequest) { r.Parent = nil; r.Repo.ObjectFormat = "" },
		"zero parent":                func(r *repostore.CommitRequest) { r.Parent = git.Sha1ObjectFormat.EmptyObjectID() },
	}
	for name, mutate := range cases {
		req := ok()
		mutate(&req)
		err := req.Validate()
		require.Error(t, err, name)
		if name != "bad path" {
			assert.ErrorIs(t, err, repostore.ErrInvalidRequest, name)
		}
	}
}

func TestMergeRequestValidate(t *testing.T) {
	ok := func() repostore.MergeRequest {
		return repostore.MergeRequest{
			Base: repo, BaseRef: "refs/heads/main",
			Head: repo, HeadRev: "refs/heads/topic",
			Style:   repostore.MergeStyleMerge,
			Meta:    meta,
			Publish: &repostore.RefUpdate{Repo: repo, Ref: "refs/heads/main"},
		}
	}
	req := ok()
	require.NoError(t, req.Validate())

	dry := ok()
	dry.DryRun, dry.Publish, dry.Style = true, nil, ""
	require.NoError(t, dry.Validate())

	cases := map[string]func(*repostore.MergeRequest){
		"unknown style":      func(r *repostore.MergeRequest) { r.Style = "octopus" },
		"no publish":         func(r *repostore.MergeRequest) { r.Publish = nil },
		"short base ref":     func(r *repostore.MergeRequest) { r.BaseRef = "main" },
		"head option":        func(r *repostore.MergeRequest) { r.HeadRev = "--upload-pack=x" },
		"dry run publishing": func(r *repostore.MergeRequest) { r.DryRun = true },
		"bad glob":           func(r *repostore.MergeRequest) { r.ProtectedGlobs = []string{""} },
		"too many globs":     func(r *repostore.MergeRequest) { r.ProtectedGlobs = make([]string, repostore.MaxGlobs+1) },
		"no committer":       func(r *repostore.MergeRequest) { r.Meta.Committer = nil },
		"bad publish ref":    func(r *repostore.MergeRequest) { r.Publish.Ref = "refs/heads/a:b" },
		"zero expected head": func(r *repostore.MergeRequest) { r.ExpectHead = git.Sha1ObjectFormat.EmptyObjectID() },
	}
	for name, mutate := range cases {
		req := ok()
		mutate(&req)
		require.Error(t, req.Validate(), name)
	}
}

func TestUpdateRefsRequestValidate(t *testing.T) {
	ok := repostore.UpdateRefsRequest{
		Repo:    repo,
		Updates: []repostore.RefUpdateSpec{{Name: "refs/heads/a", NewRev: someID.String()}, {Name: "refs/heads/b", Delete: true}},
	}
	require.NoError(t, ok.Validate())

	for name, mutate := range map[string]func(*repostore.UpdateRefsRequest){
		"none":      func(r *repostore.UpdateRefsRequest) { r.Updates = nil },
		"duplicate": func(r *repostore.UpdateRefsRequest) { r.Updates[1].Name = "refs/heads/a" },
		"refspec":   func(r *repostore.UpdateRefsRequest) { r.Updates[0].Name = "refs/heads/a:refs/heads/b" },
		"new rev":   func(r *repostore.UpdateRefsRequest) { r.Updates[0].NewRev = "+refs/heads/x" },
		"too many": func(r *repostore.UpdateRefsRequest) {
			r.Updates = make([]repostore.RefUpdateSpec, repostore.MaxRefUpdates+1)
		},
	} {
		req := repostore.UpdateRefsRequest{Repo: ok.Repo, Updates: append([]repostore.RefUpdateSpec(nil), ok.Updates...)}
		mutate(&req)
		require.Error(t, req.Validate(), name)
	}
}

func TestPatchPickInitForkValidate(t *testing.T) {
	patch := repostore.PatchRequest{Repo: repo, Parent: someID, Patch: "diff --git a/x b/x\n", Meta: meta}
	require.NoError(t, patch.Validate())
	patch.Patch = ""
	require.Error(t, patch.Validate())
	patch.Patch = strings.Repeat("x", repostore.MaxPatchBytes+1)
	require.Error(t, patch.Validate())

	pick := repostore.PickRequest{Repo: repo, Parent: someID, Commit: someID, Meta: meta}
	require.NoError(t, pick.Validate())
	pick.Parent = nil
	require.Error(t, pick.Validate())

	initReq := repostore.InitRequest{Repo: repo, DefaultBranch: "main"}
	require.NoError(t, initReq.Validate())
	initReq.DefaultBranch = "a b"
	require.Error(t, initReq.Validate())
	initReq = repostore.InitRequest{Repo: repostore.RepoRef{Name: "r", Path: "/x"}}
	require.Error(t, initReq.Validate(), "init needs an object format")

	fork := repostore.ForkRequest{Repo: repo, From: repo, SingleBranch: "main"}
	require.NoError(t, fork.Validate())
	fork.SingleBranch = "--x"
	require.Error(t, fork.Validate(), "a branch name that looks like an option")
	fork.SingleBranch = "a b"
	require.Error(t, fork.Validate())
}

type fakeWriter struct{ repostore.Writer }

func TestRegistry(t *testing.T) {
	_, err := repostore.Get("nothing")
	assert.ErrorIs(t, err, repostore.ErrNoWriter)

	w := &fakeWriter{}
	repostore.Register("fake-for-test", w)
	got, err := repostore.Get("fake-for-test")
	require.NoError(t, err)
	assert.Same(t, w, got)
	assert.Contains(t, repostore.Names(), "fake-for-test")

	assert.Panics(t, func() { repostore.Register("fake-for-test", w) })
	assert.Panics(t, func() { repostore.Register("nil-writer", nil) })
}
