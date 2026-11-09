// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows of Explore carry the changeset count of the default branch, a link to its changesets.
func TestTheExploreRowsCarryTheChangesetCount(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	require.NoError(t, repo_model.SetAscRepoChangesets(db.DefaultContext, 1, "65f1bf27bc3bf70f64657658635e66094edbcb4d", 3812))
	require.NoError(t, repo_model.SetAscRepoChangesets(db.DefaultContext, 2, "older", 7))

	t.Run("A visitor sees the count of a public repository", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1"), http.StatusOK).Body)
		chip := doc.Find(`a[href="/user2/repo1/commits/branch/master"][data-testid="repo-changesets"]`)
		require.Equal(t, 1, chip.Length())
		assert.Equal(t, "3,812 changesets", chip.Text())
		assert.Equal(t, 0, chip.Find("svg").Length(), "the count has no icon")
	})

	t.Run("The count of a private repository needs read access", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo2"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(`a[href="/user2/repo2/commits/branch/master"]`).Length())
		s := loginUser(t, "user2")
		doc = NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/user2?tab=repositories&q=repo2"), http.StatusOK).Body)
		assert.Equal(t, "7 changesets", doc.Find(`a[href="/user2/repo2/commits/branch/master"][data-testid="repo-changesets"]`).Text())
	})
}
