// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// branchlessCtx is a context for the branchless page of repository repoID, stored as backend, with a renderer
// and the recorded response.
func branchlessCtx(t *testing.T, repoID, doerID int64, backend string) (*context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: backend})
	require.NoError(t, err)
	ctx, resp := contexttest.MockContext(t, "GET /x/y/settings/asc/branchless", contexttest.MockContextOption{Render: templates.HTMLRenderer()})
	if doerID != 0 {
		ctx.Doer = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: doerID})
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, repoID)
	return ctx, resp
}

type branchlessAPI struct {
	ascv1.RepoClient
	resp  *ascv1.RepoConfigResponse
	err   error
	calls int
	// RepoAdmin (set_branchless)
	admin    *ascv1.RepoAdminRequest
	adminErr error
}

func (a *branchlessAPI) RepoAdmin(_ gocontext.Context, in *ascv1.RepoAdminRequest, _ ...grpc.CallOption) (*ascv1.RepoAdminResponse, error) {
	a.admin = in
	return &ascv1.RepoAdminResponse{}, a.adminErr
}

func (a *branchlessAPI) RepoConfig(gocontext.Context, *ascv1.RepoConfigRequest, ...grpc.CallOption) (*ascv1.RepoConfigResponse, error) {
	a.calls++
	return a.resp, a.err
}

func TestAscBranchlessPage(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	api := &branchlessAPI{}
	defer ascsync.UseClient(api)()

	t.Run("shows what asc reports and a form to switch the mode", func(t *testing.T) {
		api.resp, api.err, api.calls = &ascv1.RepoConfigResponse{Branchless: true, Cloud: true, SharedRefs: []string{"refs/heads/master", "refs/heads/<b>x</b>"}}, nil, 0
		ctx, resp := branchlessCtx(t, 1, 2, "asc")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, 1, api.calls)
		html := resp.Body.String()
		assert.Contains(t, html, "refs/heads/master")
		assert.NotContains(t, html, "<b>x</b>", "a reference name is escaped")
		assert.Contains(t, html, "&lt;b&gt;")
		assert.Contains(t, html, `data-testid="asc-branchless-form"`)
		assert.Contains(t, html, `value="branchless" checked`, "the mode asc reports is the one selected")
	})

	post := func(t *testing.T, userID int64, backend, mode string) (*context.Context, *httptest.ResponseRecorder) {
		t.Helper()
		ctx, resp := branchlessCtx(t, 1, userID, backend)
		ctx.Req.Form = url.Values{"mode": {mode}}
		AscBranchlessPost(ctx)
		return ctx, resp
	}

	t.Run("an administrator switches the mode; asc is asked once, with an explicit request", func(t *testing.T) {
		api.admin, api.adminErr = nil, nil
		ctx, resp := post(t, 2, "asc", "branches")
		assert.Equal(t, http.StatusSeeOther, resp.Code)
		require.NotNil(t, api.admin.GetSetBranchless(), "turning it off is a request, not an unset flag")
		assert.False(t, api.admin.GetSetBranchless().GetBranchless())
		assert.NotEmpty(t, ctx.Flash.SuccessMsg)
		post(t, 2, "asc", "branchless")
		assert.True(t, api.admin.GetSetBranchless().GetBranchless())
	})

	t.Run("asc's refusal is told to the person; an old server and a failure have their own words", func(t *testing.T) {
		api.adminErr = status.Error(codes.FailedPrecondition, "branch dev is not a shared ref")
		ctx, _ := post(t, 2, "asc", "branchless")
		assert.Contains(t, ctx.Flash.ErrorMsg, "branch dev is not a shared ref")
		api.adminErr = status.Error(codes.Unimplemented, "RepoAdmin")
		ctx, _ = post(t, 2, "asc", "branchless")
		assert.Contains(t, ctx.Flash.ErrorMsg, "unsupported")
		api.adminErr = errors.New("secret-token-123 leaked")
		ctx, _ = post(t, 2, "asc", "branchless")
		assert.NotContains(t, ctx.Flash.ErrorMsg, "secret-token-123")
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
	})

	t.Run("the switch is for an administrator of an asc repository, with one of two modes", func(t *testing.T) {
		api.admin, api.adminErr = nil, nil
		_, resp := post(t, 4, "asc", "branches")
		assert.Equal(t, http.StatusForbidden, resp.Code)
		_, resp = post(t, 0, "asc", "branches")
		assert.Equal(t, http.StatusNotFound, resp.Code)
		_, resp = post(t, 2, "git", "branches")
		assert.Equal(t, http.StatusNotFound, resp.Code)
		ctx, _ := post(t, 2, "asc", "sideways")
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
		assert.Nil(t, api.admin, "nothing was sent to asc")
	})

	t.Run("an asc failure shows a generic message and no detail", func(t *testing.T) {
		api.resp, api.err, api.calls = nil, errors.New("secret-token-123 leaked"), 0
		ctx, resp := branchlessCtx(t, 1, 2, "asc")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusBadGateway, resp.Code)
		assert.NotContains(t, resp.Body.String(), "secret-token-123")
	})

	t.Run("a repository stored in git, a non-administrator, an anonymous visitor and [asc] off are refused before asc is asked", func(t *testing.T) {
		api.calls = 0
		ctx, resp := branchlessCtx(t, 1, 2, "git")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusNotFound, resp.Code)

		ctx, resp = branchlessCtx(t, 1, 4, "asc")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusForbidden, resp.Code)

		ctx, resp = branchlessCtx(t, 1, 0, "asc")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusNotFound, resp.Code)

		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		ctx, resp = branchlessCtx(t, 1, 2, "asc")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusNotFound, resp.Code)
		require.Zero(t, api.calls)
	})
}
