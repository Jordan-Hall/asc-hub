// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurgeLegacyCallTokens(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	mk := func(uid int64, name string, all bool) *auth_model.AccessToken {
		tok := &auth_model.AccessToken{UID: uid, Name: name, Scope: auth_model.AccessTokenScopeWriteRepository, ResourceAllRepos: all}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, tok))
		return tok
	}
	old := mk(2, LegacyCallTokenPrefix+"abc", false)
	require.NoError(t, db.Insert(db.DefaultContext, &auth_model.AccessTokenResourceRepo{TokenID: old.ID, RepoID: 1}))
	old2 := mk(4, LegacyCallTokenPrefix+"def", true)
	mine := mk(2, "my own token", true)
	similar := mk(2, "asc-hub-callable", true) // the prefix ends with a dash

	require.NoError(t, PurgeLegacyCallTokens(db.DefaultContext))

	unittest.AssertNotExistsBean(t, &auth_model.AccessToken{ID: old.ID})
	unittest.AssertNotExistsBean(t, &auth_model.AccessToken{ID: old2.ID})
	unittest.AssertNotExistsBean(t, &auth_model.AccessTokenResourceRepo{TokenID: old.ID})
	unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: mine.ID})
	unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: similar.ID})

	// A second run, with nothing left, changes nothing.
	before, err := db.GetEngine(db.DefaultContext).Count(&auth_model.AccessToken{})
	require.NoError(t, err)
	require.NoError(t, PurgeLegacyCallTokens(db.DefaultContext))
	after, err := db.GetEngine(db.DefaultContext).Count(&auth_model.AccessToken{})
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
