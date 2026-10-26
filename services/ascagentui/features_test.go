// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatures(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	other := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	// Off until the person turns it on, and for nobody who is not signed in.
	assert.False(t, FeaturesEnabled(db.DefaultContext, user))
	assert.False(t, FeaturesEnabled(db.DefaultContext, nil))
	assert.False(t, FeaturesEnabled(db.DefaultContext, &user_model.User{}))
	require.Error(t, SetFeatures(db.DefaultContext, nil, true))

	require.NoError(t, SetFeatures(db.DefaultContext, user, true))
	assert.True(t, FeaturesEnabled(db.DefaultContext, user))
	assert.False(t, FeaturesEnabled(db.DefaultContext, other), "a setting of one person is not another's")

	require.NoError(t, SetFeatures(db.DefaultContext, user, false))
	assert.False(t, FeaturesEnabled(db.DefaultContext, user))

	// Anything but "true" reads as off.
	require.NoError(t, user_model.SetUserSetting(db.DefaultContext, user.ID, FeaturesKey, "yes"))
	assert.False(t, FeaturesEnabled(db.DefaultContext, user))
	require.NoError(t, user_model.SetUserSetting(db.DefaultContext, user.ID, FeaturesKey, "true"))
	assert.True(t, FeaturesEnabled(db.DefaultContext, user))
}

func TestChangeLinks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	open := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})

	raw := func(b byte) []byte {
		out := make([]byte, 16)
		for i := range out {
			out[i] = b + byte(i)
		}
		return out
	}
	c1, c2, c3 := raw(10), raw(40), raw(70)
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, merged, ChangeText(c1), 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, open, ChangeText(c2), merged.ID))

	links, err := ChangeLinks(db.DefaultContext, repo, "/user2/repo1", [][]byte{c1, c2, c3}, true)
	require.NoError(t, err)
	require.Len(t, links, 3)

	assert.Equal(t, 1, links[0].Position)
	assert.Equal(t, ChangeText(c1), links[0].Text)
	assert.True(t, links[0].HasPull)
	assert.True(t, links[0].Merged)
	assert.Equal(t, "/user2/repo1/pulls/2", links[0].URL, "the pull request index, not its id")

	assert.True(t, links[1].HasPull)
	assert.False(t, links[1].Merged)
	assert.Equal(t, "/user2/repo1/pulls/3", links[1].URL)

	assert.False(t, links[2].HasPull, "never submitted for review")
	assert.Empty(t, links[2].URL)
	assert.Equal(t, 3, links[2].Position)

	t.Run("no links when the viewer cannot read pull requests", func(t *testing.T) {
		links, err := ChangeLinks(db.DefaultContext, repo, "/user2/repo1", [][]byte{c1}, false)
		require.NoError(t, err)
		require.Len(t, links, 1)
		assert.False(t, links[0].HasPull)
		assert.Empty(t, links[0].URL)
	})

	t.Run("another repository's stack row is not used", func(t *testing.T) {
		repo2 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
		links, err := ChangeLinks(db.DefaultContext, repo2, "/user2/repo2", [][]byte{c1}, true)
		require.NoError(t, err)
		require.Len(t, links, 1)
		assert.False(t, links[0].HasPull)
	})

	t.Run("ids of the wrong length are skipped, the list is bounded", func(t *testing.T) {
		links, err := ChangeLinks(db.DefaultContext, repo, "/user2/repo1", [][]byte{nil, {1, 2, 3}, c1}, true)
		require.NoError(t, err)
		require.Len(t, links, 1)
		assert.Equal(t, 1, links[0].Position)

		many := make([][]byte, MaxChangeLinks+10)
		for i := range many {
			many[i] = raw(byte(i))
		}
		links, err = ChangeLinks(db.DefaultContext, repo, "/user2/repo1", many, true)
		require.NoError(t, err)
		assert.Len(t, links, MaxChangeLinks)
	})

	t.Run("nothing to resolve", func(t *testing.T) {
		links, err := ChangeLinks(db.DefaultContext, repo, "/user2/repo1", nil, true)
		require.NoError(t, err)
		assert.Empty(t, links)
	})
}
