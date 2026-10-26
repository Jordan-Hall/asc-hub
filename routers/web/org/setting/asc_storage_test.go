// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"testing"

	"forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscByoLevelOrganization(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	newCtx := func(owner bool, signedIn bool) (*context.Context, *int) {
		ctx, resp := contexttest.MockContext(t, "GET /org/Org3/settings/asc/storage")
		if signedIn {
			ctx.Doer, ctx.IsSigned = &user_model.User{ID: 2, Name: "alice", LowerName: "alice"}, true
		}
		ctx.Org.Organization = &organization.Organization{ID: 3, Name: "Org3", LowerName: "org3"}
		ctx.Org.IsOwner = owner
		return ctx, &resp.Code
	}

	t.Run("an owner gets the organisation scope", func(t *testing.T) {
		ctx, _ := newCtx(true, true)
		lvl := ascByoLevel(ctx)
		require.NotNil(t, lvl)
		assert.Equal(t, svc.Scope{Kind: svc.KindOrg, Name: "org3"}, lvl.Scope)
		assert.Equal(t, "Org3", lvl.ScopeLabel)
		assert.Equal(t, "/org/Org3/settings/asc", lvl.Base)
		assert.Equal(t, tplSettingsAscStorage, lvl.StorageTpl)
		assert.Equal(t, tplSettingsAscAI, lvl.AITpl)
	})
	t.Run("a member who is not an owner gets a 403", func(t *testing.T) {
		ctx, code := newCtx(false, true)
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusForbidden, *code)
	})
	t.Run("not signed in is a 404", func(t *testing.T) {
		ctx, code := newCtx(true, false)
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
	t.Run("no organisation in the context is a 404", func(t *testing.T) {
		ctx, code := newCtx(true, true)
		ctx.Org.Organization = nil
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
	t.Run("[asc] off is a 404", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		ctx, code := newCtx(true, true)
		assert.Nil(t, ascByoLevel(ctx))
		assert.Equal(t, http.StatusNotFound, *code)
	})
}
