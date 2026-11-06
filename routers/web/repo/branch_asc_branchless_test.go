// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	repo_service "forgejo.org/services/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchesOfABranchlessRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	page := func(t *testing.T, backend string, bs ascsync.BranchlessSettings, bsErr error) (*context.Context, string) {
		t.Helper()
		defer test.MockVariableValue(&ascLoadBranchless, func(*context.Context, *repo_model.Repository) (ascsync.BranchlessSettings, error) {
			return bs, bsErr
		})()
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/branches")
		ctx.Render = templates.HTMLRenderer()
		ctx.Req.Header.Set("Accept", "text/html")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		ctx.Repo.Repository.StorageBackend = backend
		Branches(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		return ctx, resp.Body.String()
	}
	names := func(ctx *context.Context) []string {
		var out []string
		for _, b := range ctx.Data["Branches"].([]*repo_service.Branch) {
			out = append(out, b.DBBranch.Name)
		}
		return out
	}

	t.Run("only the shared refs, with the branchless wording", func(t *testing.T) {
		ctx, body := page(t, "asc", ascsync.BranchlessSettings{Branchless: true, SharedRefs: []string{"refs/heads/master", "refs/heads/branch2"}}, nil)
		assert.Equal(t, []string{"branch2"}, names(ctx))
		assert.EqualValues(t, 2, ctx.Data["AscSharedRefCount"])
		assert.Contains(t, body, `data-testid="refs-branchless"`)
		assert.Contains(t, body, `asc_rp.refs_branchless_sub_1`, "the mock locale names the first plural key")
		assert.Contains(t, body, `data-testid="refs-shared"`)
		assert.Contains(t, body, `asc_rp.shared_refs_sub`)
	})
	t.Run("a repository with branches lists them all", func(t *testing.T) {
		ctx, body := page(t, "asc", ascsync.BranchlessSettings{SharedRefs: []string{"refs/heads/master"}}, nil)
		assert.Greater(t, len(names(ctx)), 1)
		assert.NotContains(t, body, `data-testid="refs-branchless"`)
	})
	t.Run("asc cannot say: every branch, as before", func(t *testing.T) {
		ctx, body := page(t, "asc", ascsync.BranchlessSettings{}, errors.New("asc is down"))
		assert.Greater(t, len(names(ctx)), 1)
		assert.NotContains(t, body, `data-testid="refs-shared"`)
	})
	t.Run("a git repository is never branchless", func(t *testing.T) {
		ctx, _ := page(t, "git", ascsync.BranchlessSettings{Branchless: true}, nil)
		assert.Nil(t, ctx.Data["AscBranchless"])
	})
}
