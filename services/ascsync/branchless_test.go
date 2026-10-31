// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type configAPI struct {
	ascv1.RepoClient
	resp *ascv1.RepoConfigResponse
	name string
}

func (c *configAPI) RepoConfig(_ context.Context, in *ascv1.RepoConfigRequest, _ ...grpc.CallOption) (*ascv1.RepoConfigResponse, error) {
	c.name = in.GetRepo().GetName()
	return c.resp, nil
}

func TestAscBranchlessLoad(t *testing.T) {
	fx := newFixture(t)
	api := &configAPI{resp: &ascv1.RepoConfigResponse{Branchless: true, Cloud: true, SharedRefs: []string{"refs/heads/master"}}}
	defer UseClient(api)()
	got, err := LoadBranchlessSettings(t.Context(), fx.repo)
	require.NoError(t, err)
	assert.Equal(t, BranchlessSettings{Branchless: true, Cloud: true, SharedRefs: []string{"refs/heads/master"}}, got)
	assert.Equal(t, "owner/repo", strings.ToLower(api.name))

	// A repository not stored in asc is refused, and nothing is sent.
	api.name = ""
	plain := *fx.repo
	plain.StorageBackend = "git"
	_, err = LoadBranchlessSettings(t.Context(), &plain)
	require.ErrorIs(t, err, ErrNotAsc)
	assert.Empty(t, api.name)

	_, err = LoadBranchlessSettings(t.Context(), nil)
	require.ErrorIs(t, err, ErrBadRequest)
}

type adminAPI struct {
	ascv1.RepoClient
	req *ascv1.RepoAdminRequest
	err error
}

func (a *adminAPI) RepoAdmin(_ context.Context, in *ascv1.RepoAdminRequest, _ ...grpc.CallOption) (*ascv1.RepoAdminResponse, error) {
	a.req = in
	return &ascv1.RepoAdminResponse{}, a.err
}

func TestAscSetBranchless(t *testing.T) {
	fx := newFixture(t)
	api := &adminAPI{}
	defer UseClient(api)()
	require.NoError(t, SetBranchless(t.Context(), fx.repo, true))
	assert.Equal(t, "owner/repo", strings.ToLower(api.req.GetRepo().GetName()))
	assert.True(t, api.req.GetSetBranchless().GetBranchless())
	require.NoError(t, SetBranchless(t.Context(), fx.repo, false))
	assert.NotNil(t, api.req.GetSetBranchless(), "turning it off is an explicit request, not an unset flag")
	assert.False(t, api.req.GetSetBranchless().GetBranchless())

	// an older server: no such operation, or nothing implemented
	api.err = status.Error(codes.InvalidArgument, "no operation")
	require.ErrorIs(t, SetBranchless(t.Context(), fx.repo, true), ErrBranchlessUnsupported)
	api.err = status.Error(codes.Unimplemented, "RepoAdmin")
	require.ErrorIs(t, SetBranchless(t.Context(), fx.repo, true), ErrBranchlessUnsupported)
	api.err = status.Error(codes.InvalidArgument, "default branch name")
	require.NotErrorIs(t, SetBranchless(t.Context(), fx.repo, true), ErrBranchlessUnsupported)

	// asc's refusal is told to the person; a repository not kept in asc sends nothing
	api.err = status.Error(codes.FailedPrecondition, "branch dev is not a shared ref")
	err := SetBranchless(t.Context(), fx.repo, true)
	var rej *RejectError
	require.ErrorAs(t, err, &rej)
	assert.Contains(t, rej.Error(), "dev")
	api.req = nil
	plain := *fx.repo
	plain.StorageBackend = "git"
	require.ErrorIs(t, SetBranchless(t.Context(), &plain, true), ErrNotAsc)
	assert.Nil(t, api.req)
}
