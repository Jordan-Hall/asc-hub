// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExploreLanguagesFollowOwnerVisibility(t *testing.T) {
	unittest.PrepareTestEnv(t)

	// a public repository of a public owner, and public repositories whose owner is limited or private
	visible := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.False(t, visible.IsPrivate)
	hidden := 0
	for _, id := range []int64{38, 40} {
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: id})
		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})
		if owner.Visibility != structs.VisibleTypePublic && !repo.IsPrivate {
			require.NoError(t, db.Insert(t.Context(), &repo_model.LanguageStat{RepoID: id, Language: "HiddenLang" + owner.Name, IsPrimary: true, Size: 10}))
			hidden++
		}
	}
	require.NotZero(t, hidden, "the fixtures have a public repository under a limited or private owner")
	require.NoError(t, db.Insert(t.Context(), &repo_model.LanguageStat{RepoID: 1, Language: "VisibleLang", IsPrimary: true, Size: 10}))

	anon, _ := contexttest.MockContext(t, "GET /explore/repos")
	anon.Doer = nil
	got := exploreLanguages(anon, "")
	assert.Contains(t, got, "VisibleLang")
	for _, name := range got {
		assert.NotContains(t, name, "HiddenLang", "an anonymous viewer is not offered a language only a limited or private owner's repository has")
	}

	// a chosen language stays in the list so the select shows it
	assert.Contains(t, exploreLanguages(anon, "Zig"), "Zig")
}
