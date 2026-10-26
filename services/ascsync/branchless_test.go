// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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
	assert.Equal(t, "Owner/Repo", api.name)

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
