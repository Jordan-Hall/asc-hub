// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"testing"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscByoLevelUser(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	newCtx := func(doer *user_model.User) (*context.Context, *int) {
		ctx, resp := contexttest.MockContext(t, "GET /user/settings/asc/storage")
		if doer != nil {
			ctx.Doer, ctx.IsSigned = doer, true
		}
		return ctx, &resp.Code
	}

	t.Run("the scope is the signed-in user", func(t *testing.T) {
		ctx, _ := newCtx(&user_model.User{ID: 2, Name: "Alice", LowerName: "alice"})
		lvl := ascByoLevel(ctx)
		require.NotNil(t, lvl)
		assert.Equal(t, svc.Scope{Kind: svc.KindUser, Name: "alice"}, lvl.Scope)
		assert.Equal(t, "Alice", lvl.ScopeLabel)
		assert.Equal(t, "/user/settings/asc", lvl.Base)
		assert.Equal(t, tplSettingsAscStorage, lvl.StorageTpl)
		assert.Equal(t, tplSettingsAscAI, lvl.AITpl)
		assert.Empty(t, lvl.Allowed)
	})
	t.Run("a query string or form cannot name another account", func(t *testing.T) {
		ctx, _ := newCtx(&user_model.User{ID: 2, Name: "alice", LowerName: "alice"})
		ctx.Req.Form = map[string][]string{"owner": {"bob"}, "user": {"bob"}, "name": {"bob"}}
		lvl := ascByoLevel(ctx)
		require.NotNil(t, lvl)
		assert.Equal(t, "alice", lvl.Scope.Name)
	})
	t.Run("not signed in is a 404", func(t *testing.T) {
		ctx, code := newCtx(nil)
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
	t.Run("an organisation account is a 404", func(t *testing.T) {
		ctx, code := newCtx(&user_model.User{ID: 3, Name: "org3", LowerName: "org3", Type: user_model.UserTypeOrganization})
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
	t.Run("an account that may not sign in gets a 403", func(t *testing.T) {
		ctx, code := newCtx(&user_model.User{ID: 9, Name: "locked", LowerName: "locked", ProhibitLogin: true})
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusForbidden, *code)
	})
	t.Run("[asc] off is a 404", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		ctx, code := newCtx(&user_model.User{ID: 2, Name: "alice", LowerName: "alice"})
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
}
