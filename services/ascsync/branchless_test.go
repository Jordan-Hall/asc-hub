// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"testing"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAscBranchlessSharedRefs(t *testing.T) {
	got, err := NormalizeSharedRefs([]string{"master", "refs/heads/release/*", "master", " refs/tags/* "})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/master", "refs/heads/release/*", "refs/tags/*"}, got)

	for _, bad := range []string{
		"", "refs/users/*", "refs/users/5/x", "refs/*", "refs/u*", "refs/", "*x/../y", "a b", "a\x00b",
		"refs/heads/a//b", "refs/heads/x.lock", "refs/heads/a~1", "refs/heads/a[", "refs/heads/@{u}",
	} {
		_, err := NormalizeSharedRefs([]string{bad})
		require.ErrorIs(t, err, ErrBadRequest, "%q", bad)
	}
	// A bare "*" means refs/heads/*; it cannot reach the user namespace.
	got, err = NormalizeSharedRefs([]string{"*"})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/*"}, got)

	many := make([]string, MaxSharedRefs+1)
	for i := range many {
		many[i] = "b"
	}
	_, err = NormalizeSharedRefs(many)
	require.ErrorIs(t, err, ErrBadRequest)
	_, err = NormalizeSharedRefs([]string{string(make([]byte, MaxSharedRefBytes+1))})
	require.ErrorIs(t, err, ErrBadRequest)
}

func TestAscBranchlessValidate(t *testing.T) {
	private := &repo_model.Repository{IsPrivate: true}
	public := &repo_model.Repository{}
	_, err := ValidateBranchlessSettings(private, BranchlessSettings{Branchless: true, Cloud: true})
	require.NoError(t, err)
	// Cloud is never on for a public repository, nor without branchless.
	_, err = ValidateBranchlessSettings(public, BranchlessSettings{Branchless: true, Cloud: true})
	require.ErrorIs(t, err, ErrBadRequest)
	_, err = ValidateBranchlessSettings(private, BranchlessSettings{Cloud: true})
	require.ErrorIs(t, err, ErrBadRequest)
	_, err = ValidateBranchlessSettings(nil, BranchlessSettings{})
	require.ErrorIs(t, err, ErrBadRequest)
}

func TestAscBranchlessNoRowsForUserRefs(t *testing.T) {
	s := BranchlessSettings{Branchless: true, SharedRefs: []string{"refs/heads/master", "refs/heads/release/*", "refs/tags/*"}}
	assert.True(t, s.RecordsBranchRow("refs/heads/master"))
	assert.True(t, s.RecordsBranchRow("refs/heads/release/1.0"))
	assert.False(t, s.RecordsBranchRow("refs/heads/feature"))
	assert.False(t, s.RecordsBranchRow("refs/users/5/feature"))
	assert.False(t, s.RecordsBranchRow("refs/tags/v1"), "a tag is not a branch")

	assert.True(t, s.EmitsPushEvent("refs/heads/master"))
	assert.True(t, s.EmitsPushEvent("refs/tags/v1"))
	assert.False(t, s.EmitsPushEvent("refs/heads/feature"))
	assert.False(t, s.EmitsPushEvent("refs/users/5/feature"))

	// Even a hostile shared_refs stored in asc never makes the user namespace shared.
	h := BranchlessSettings{Branchless: true, SharedRefs: []string{"refs/*"}}
	assert.False(t, h.EmitsPushEvent("refs/users/5/x"))
	assert.False(t, h.RecordsBranchRow("refs/users/5/x"))

	branchMode := BranchlessSettings{}
	assert.True(t, branchMode.RecordsBranchRow("refs/heads/feature"))
	assert.True(t, branchMode.EmitsPushEvent("refs/heads/feature"))
}

type configAPI struct {
	ascv1.RepoClient
	resp *ascv1.RepoConfigResponse
}

func (c configAPI) RepoConfig(context.Context, *ascv1.RepoConfigRequest, ...grpc.CallOption) (*ascv1.RepoConfigResponse, error) {
	return c.resp, nil
}

type recordWriter struct {
	name string
	got  BranchlessSettings
}

func (w *recordWriter) SetRepoSettings(_ context.Context, name string, s BranchlessSettings) error {
	w.name, w.got = name, s
	return nil
}

func TestAscBranchlessLoadAndSave(t *testing.T) {
	fx := newFixture(t)
	defer UseClient(configAPI{resp: &ascv1.RepoConfigResponse{Branchless: true, Cloud: true, SharedRefs: []string{"refs/heads/master"}}})()
	got, err := LoadBranchlessSettings(t.Context(), fx.repo)
	require.NoError(t, err)
	assert.Equal(t, BranchlessSettings{Branchless: true, Cloud: true, SharedRefs: []string{"refs/heads/master"}}, got)

	// Until asc has the settings op, a save fails closed.
	err = SaveBranchlessSettings(t.Context(), fx.repo, BranchlessSettings{Branchless: true})
	require.ErrorIs(t, err, ErrSettingsUnsupported)

	w := &recordWriter{}
	defer UseSettingsWriter(w)()
	fx.repo.IsPrivate = true
	require.NoError(t, SaveBranchlessSettings(t.Context(), fx.repo, BranchlessSettings{Branchless: true, Cloud: true, SharedRefs: []string{"main"}}))
	assert.Equal(t, "Owner/Repo", w.name)
	assert.Equal(t, []string{"refs/heads/main"}, w.got.SharedRefs)

	// An invalid change is refused before anything is sent.
	w.name = ""
	require.ErrorIs(t, SaveBranchlessSettings(t.Context(), fx.repo, BranchlessSettings{Branchless: true, SharedRefs: []string{"refs/users/*"}}), ErrBadRequest)
	assert.Empty(t, w.name)

	// A repository not stored in asc is refused.
	plain := *fx.repo
	plain.StorageBackend = "git"
	require.ErrorIs(t, SaveBranchlessSettings(t.Context(), &plain, BranchlessSettings{}), ErrNotAsc)
}
