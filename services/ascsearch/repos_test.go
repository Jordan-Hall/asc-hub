// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/authz"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setBackend moves repositories into asc. The fixtures give repository 1 (user2, public), 2 (user2,
// private), 3 (org3, private), 4 (user5, public) and 10 (user12, public).
func setBackend(t *testing.T, backend string, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		_, err := db.GetEngine(db.DefaultContext).ID(id).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: backend})
		require.NoError(t, err)
	}
}

func names(r *Resolved) []string {
	var out []string
	for _, x := range r.Repos {
		out = append(out, x.Name)
	}
	return out
}

func TestResolve(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, "asc", 1, 2, 3, 4)
	setBackend(t, "dual", 10) // a dual repository lives in git, asc only shadows it
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	ctx := t.Context()

	t.Run("anonymous sees the public asc repositories only", func(t *testing.T) {
		r, err := Resolve(ctx, nil, nil, Selector{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"user2/repo1", "user5/repo4"}, names(r))
		assert.False(t, r.Truncated)
		assert.Equal(t, 2, r.Total)
	})

	t.Run("a user sees the private repositories they may read", func(t *testing.T) {
		r, err := Resolve(ctx, user2, nil, Selector{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2", "org3/repo3", "user5/repo4"}, names(r))

		r, err = Resolve(ctx, user5, nil, Selector{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"user2/repo1", "user5/repo4"}, names(r))
	})

	t.Run("a repository outside asc is never returned", func(t *testing.T) {
		r, err := Resolve(ctx, user2, nil, Selector{Repos: []string{"user12/repo10", "user2/repo1"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"user2/repo1"}, names(r))
	})

	t.Run("a named repository the user cannot read leaves no trace", func(t *testing.T) {
		r, err := Resolve(ctx, user5, nil, Selector{Repos: []string{"user2/repo2", "org3/repo3", "user2/repo1", "nobody/none"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"user2/repo1"}, names(r))
		assert.Equal(t, 1, r.Total)

		r, err = Resolve(ctx, nil, nil, Selector{Repos: []string{"user2/repo2"}})
		require.NoError(t, err)
		assert.Empty(t, r.Repos)
	})

	t.Run("case does not matter and a duplicate counts once", func(t *testing.T) {
		r, err := Resolve(ctx, user2, nil, Selector{Repos: []string{"USER2/Repo1", "user2/repo1"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"user2/repo1"}, names(r))
		assert.Equal(t, "refs/heads/master", r.Repos[0].Rev)
		assert.Equal(t, "master", r.Repos[0].Branch)
		x, ok := r.ByName("user2/repo1")
		assert.True(t, ok)
		assert.NotZero(t, x.ID)
		_, ok = r.ByName("user2/repo2")
		assert.False(t, ok)
		assert.Equal(t, []string{"user2/repo1"}, r.Names())
	})

	t.Run("an owner narrows the search", func(t *testing.T) {
		r, err := Resolve(ctx, user2, nil, Selector{Owner: "user2"})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2"}, names(r))

		r, err = Resolve(ctx, user5, nil, Selector{Owner: "user2"})
		require.NoError(t, err)
		assert.Equal(t, []string{"user2/repo1"}, names(r))

		r, err = Resolve(ctx, user2, nil, Selector{Owner: "no-such-owner"})
		require.NoError(t, err)
		assert.Empty(t, r.Repos)
	})

	t.Run("bad names are refused", func(t *testing.T) {
		for _, sel := range []Selector{
			{Repos: []string{"nodash"}},
			{Repos: []string{"a/b/c"}},
			{Repos: []string{"/b"}},
			{Repos: []string{"a/"}},
			{Repos: []string{"a/b\x00"}},
			{Repos: make([]string, MaxRepos+1)},
			{Owner: "a/b"},
			{Owner: "a\nb"},
		} {
			_, err := Resolve(ctx, user2, nil, sel)
			require.ErrorIs(t, err, ErrBadSelector, "%v", sel)
		}
	})

	t.Run("the list is cut at Max, newest first", func(t *testing.T) {
		_, err := db.GetEngine(db.DefaultContext).ID(4).Cols("updated_unix").NoAutoTime().Update(&repo_model.Repository{UpdatedUnix: 9_000_000_000})
		require.NoError(t, err)
		r, err := Resolve(ctx, user2, nil, Selector{Max: 1})
		require.NoError(t, err)
		assert.Equal(t, []string{"user5/repo4"}, names(r))
		assert.True(t, r.Truncated)
		assert.Equal(t, 4, r.Total)
	})

	t.Run("a token limited to public repositories hides the private ones", func(t *testing.T) {
		reducer := &authz.PublicReposAuthorizationReducer{}
		r, err := Resolve(ctx, user2, reducer, Selector{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"user2/repo1", "user5/repo4"}, names(r))

		r, err = Resolve(ctx, user2, reducer, Selector{Repos: []string{"user2/repo2", "user2/repo1"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"user2/repo1"}, names(r))
	})
}

func TestCanRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setBackend(t, "asc", 1, 2)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})

	r, ok := CanRead(t.Context(), user2, nil, "user2/repo2")
	assert.True(t, ok)
	assert.Equal(t, "user2/repo2", r.Name)
	_, ok = CanRead(t.Context(), user5, nil, "user2/repo2")
	assert.False(t, ok, "private to another user")
	_, ok = CanRead(t.Context(), nil, nil, "user2/repo2")
	assert.False(t, ok, "anonymous")
	_, ok = CanRead(t.Context(), user2, nil, "user2/missing")
	assert.False(t, ok)
	_, ok = CanRead(t.Context(), user2, nil, "garbage")
	assert.False(t, ok)
	_, ok = CanRead(t.Context(), user2, nil, "user12/repo10")
	assert.False(t, ok, "not an asc repository")
	_, ok = CanRead(t.Context(), user2, &authz.PublicReposAuthorizationReducer{}, "user2/repo2")
	assert.False(t, ok, "the token is limited to public repositories")
}
