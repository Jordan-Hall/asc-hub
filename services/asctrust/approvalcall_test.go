// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The gate services/pull uses for a verdict is the central one: a person who may write gets a signed claim, one who may not is
// skipped without an error, and an archived repository is read only.
func TestAscWriteCallIsTheCentralGate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	reader := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	ctx, ok, err := ascWriteCall(t.Context(), owner, repo)
	require.NoError(t, err)
	require.True(t, ok)
	_, role, verr := claimIn(t, ctx, "user2/repo1")
	require.NoError(t, verr)
	assert.Equal(t, ascidentity.RoleAdmin, role)

	_, ok, err = ascWriteCall(t.Context(), reader, repo)
	require.NoError(t, err)
	assert.False(t, ok, "a reader gives no verdict")

	_, ok, err = ascWriteCall(t.Context(), nil, repo)
	require.NoError(t, err)
	assert.False(t, ok, "nobody gives none")

	archived := *repo
	archived.IsArchived = true
	_, ok, err = ascWriteCall(t.Context(), owner, &archived)
	require.NoError(t, err)
	assert.False(t, ok, "an archived repository is read only")
}
