// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"

	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mapSession is the three methods of a session the token card uses; the rest of the interface is unused.
type mapSession struct {
	session.Store
	values map[any]any
}

func (m *mapSession) Get(k any) any      { return m.values[k] }
func (m *mapSession) Set(k, v any) error { m.values[k] = v; return nil }
func (m *mapSession) Delete(k any) error { delete(m.values, k); return nil }

func TestNewAccessTokenShownOnce(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "user/settings/applications")
	ctx.Session = &mapSession{values: map[any]any{}}

	assert.Empty(t, takeNewAccessToken(ctx), "nothing was made yet")

	rememberNewAccessToken(ctx, "secret-token-value")
	assert.Empty(t, ctx.Flash.InfoMsg, "the token is not a flash message any more")

	assert.Equal(t, "secret-token-value", takeNewAccessToken(ctx))
	assert.Empty(t, takeNewAccessToken(ctx), "the second view does not show it again")
}

func TestSettingsOrgRows(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "user/settings/organization")
	contexttest.LoadUser(t, ctx, 2)

	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	rows, err := settingsOrgRows(ctx, []*organization.Organization{org}, ctx.Doer.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	owned, err := org.IsOwnedBy(ctx, ctx.Doer.ID)
	require.NoError(t, err)
	assert.Equal(t, owned, rows[0].IsOwner)
	if !rows[0].IsOwner {
		assert.False(t, rows[0].LastOwner, "only an owner can be the last owner")
	}
}
