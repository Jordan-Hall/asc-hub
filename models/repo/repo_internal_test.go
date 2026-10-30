// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo_test

import (
	"testing"

	"forgejo.org/models/db"
	perm_model "forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An internal repository is private to the public and open to every signed-in person who is not restricted.
func TestInternalRepository(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2}) // private, of user 2
	require.True(t, repo.IsPrivate)
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	restricted := &user_model.User{ID: stranger.ID, LowerName: stranger.LowerName, Name: stranger.Name, IsRestricted: true}

	canRead := func(r *repo_model.Repository, u *user_model.User) bool {
		perm, err := access_model.GetUserRepoPermission(t.Context(), r, u)
		require.NoError(t, err)
		return perm.AccessMode >= perm_model.AccessModeRead && perm.CanRead(unit.TypeCode)
	}
	listed := func(u *user_model.User) bool {
		var found []*repo_model.Repository
		require.NoError(t, db.GetEngine(t.Context()).Where(repo_model.AccessibleRepositoryCondition(u, unit.TypeInvalid)).And("`repository`.id = ?", repo.ID).Find(&found))
		return len(found) > 0
	}

	// private: nobody outside sees it
	require.False(t, canRead(repo, stranger), "the stranger must not read a private repository (pick another fixture user if this fails)")
	assert.False(t, canRead(repo, nil))
	assert.False(t, listed(stranger))

	repo.IsInternal = true
	_, err := db.GetEngine(t.Context()).ID(repo.ID).Cols("is_internal").Update(repo)
	require.NoError(t, err)

	assert.True(t, canRead(repo, stranger), "a signed-in person reads an internal repository")
	assert.False(t, canRead(repo, nil), "an anonymous visitor does not")
	assert.False(t, canRead(repo, restricted), "a restricted person does not")
	assert.True(t, listed(stranger))
	assert.False(t, listed(nil))
	assert.False(t, listed(restricted))

	// reading is all they get
	perm, err := access_model.GetUserRepoPermission(t.Context(), repo, stranger)
	require.NoError(t, err)
	assert.False(t, perm.CanWrite(unit.TypeCode))
	assert.True(t, repo.IsPublicTo(stranger))
	assert.False(t, repo.IsPublicTo(nil))
	assert.False(t, repo.IsPublicTo(restricted))
}
