// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ascRepoCtx(t *testing.T, repoID, doerID int64, backend string) (*context.Context, *int) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: backend})
	require.NoError(t, err)
	ctx, resp := contexttest.MockContext(t, "GET /x/y/settings/asc/storage")
	if doerID != 0 {
		ctx.Doer = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: doerID})
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, repoID)
	code := &resp.Code
	return ctx, code
}

func TestAscByoLevelRepository(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))

	t.Run("a user's own repository: the owner's targets can be used", func(t *testing.T) {
		// repo1 is owned by user2, who administers it
		ctx, _ := ascRepoCtx(t, 1, 2, "asc")
		lvl := ascByoLevel(ctx)
		require.NotNil(t, lvl)
		assert.Equal(t, svc.KindRepo, lvl.Scope.Kind)
		assert.Equal(t, "user2/repo1", lvl.Scope.Name)
		assert.Equal(t, "/user2/repo1/settings/asc", lvl.Base)
		require.Len(t, lvl.Allowed, 2)
		assert.Equal(t, lvl.Scope, lvl.Allowed[0])
		assert.Equal(t, svc.Scope{Kind: svc.KindUser, Name: "user2"}, lvl.Allowed[1])
		assert.Equal(t, "/user/settings/asc/storage", lvl.OwnerStorageURL)
		assert.Equal(t, tplSettingsAscStorage, lvl.StorageTpl)
		assert.Equal(t, tplSettingsAscAI, lvl.AITpl)
	})

	t.Run("an organisation's repository: an owner of the organisation may use its targets", func(t *testing.T) {
		// repo3 belongs to org3, whose owner team has user2
		ctx, _ := ascRepoCtx(t, 3, 2, "asc")
		lvl := ascByoLevel(ctx)
		require.NotNil(t, lvl)
		assert.Equal(t, "org3/repo3", lvl.Scope.Name)
		require.Len(t, lvl.Allowed, 2)
		assert.Equal(t, svc.Scope{Kind: svc.KindOrg, Name: "org3"}, lvl.Allowed[1])
		assert.Equal(t, "/org/org3/settings/asc/storage", lvl.OwnerStorageURL)
	})

	t.Run("a repository administrator who does not administer the owner gets the repository's own targets only", func(t *testing.T) {
		ctx, _ := ascRepoCtx(t, 3, 2, "asc")
		// user2 holds admin permission on the repository but is not an owner of the organisation
		ctx.Repo.Permission.AccessMode = perm.AccessModeAdmin
		ctx.Doer = &user_model.User{ID: 99999, Name: "someone", LowerName: "someone"}
		lvl := ascByoLevel(ctx)
		require.NotNil(t, lvl)
		assert.Equal(t, []svc.Scope{lvl.Scope}, lvl.Allowed)
		assert.Empty(t, lvl.OwnerStorageURL)
	})

	t.Run("a repository that is not stored in asc is a 404", func(t *testing.T) {
		for _, backend := range []string{"git", "dual", ""} {
			ctx, code := ascRepoCtx(t, 1, 2, backend)
			assert.Nil(t, ascByoLevel(ctx), backend)
			assert.Equal(t, http.StatusNotFound, *code, backend)
		}
	})

	t.Run("someone who does not administer the repository gets a 403", func(t *testing.T) {
		// user4 has no access to repo1
		ctx, code := ascRepoCtx(t, 1, 4, "asc")
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusForbidden, *code)
	})

	t.Run("not signed in, or [asc] off, is a 404", func(t *testing.T) {
		ctx, code := ascRepoCtx(t, 1, 0, "asc")
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)

		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		ctx, code = ascRepoCtx(t, 1, 2, "asc")
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
}
