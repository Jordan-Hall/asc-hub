// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"errors"
	"net/http"
	"net/http/httptest"
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
}

func (a *branchlessAPI) RepoConfig(gocontext.Context, *ascv1.RepoConfigRequest, ...grpc.CallOption) (*ascv1.RepoConfigResponse, error) {
	a.calls++
	return a.resp, a.err
}

func TestAscBranchlessPage(t *testing.T) {
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	api := &branchlessAPI{}
	defer ascsync.UseClient(api)()

	t.Run("shows what asc reports and has no form", func(t *testing.T) {
		api.resp, api.err, api.calls = &ascv1.RepoConfigResponse{Branchless: true, Cloud: true, SharedRefs: []string{"refs/heads/master", "refs/heads/<b>x</b>"}}, nil, 0
		ctx, resp := branchlessCtx(t, 1, 2, "asc")
		AscBranchless(ctx)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, 1, api.calls)
		html := resp.Body.String()
		assert.Contains(t, html, "refs/heads/master")
		assert.NotContains(t, html, "<b>x</b>", "a reference name is escaped")
		assert.Contains(t, html, "&lt;b&gt;")
		assert.NotContains(t, html, `method="post"`, "the page cannot write a setting")
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
