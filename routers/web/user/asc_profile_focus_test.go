// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscFocusTally(t *testing.T) {
	f := ascFocusTally{}
	f.add("Rust", "/a", 3)
	f.add("rust", "/b", 2)
	f.add("Ledgers", "/c", 5)
	f.add("  ", "/d", 9)
	f.add("Retries", "/e", 1)
	assert.Equal(t, []AscProfileFocus{
		{Name: "Ledgers", Link: "/c"},
		{Name: "Rust", Link: "/a"}, // one subject in any case, the first link kept; a tie goes by name
		{Name: "Retries", Link: "/e"},
	}, f.top(6))
	assert.Len(t, f.top(2), 2)
	assert.Empty(t, ascFocusTally{}.top(6))
}

// Focus comes from the topics and main language of the repositories the person changes, as far as the viewer may
// read them.
func TestAscProfileFocus(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})

	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repo1.Topics = []string{"retries", "idempotency"}
	require.NoError(t, repo_model.UpdateRepositoryCols(ctx, repo1, "topics"))
	// the fixtures give repo1 Go (primary) and a little PHP

	_, focus := ascProfileRepos(ctx, user1, nil)
	assert.Equal(t, []AscProfileFocus{
		{Name: "Go", Link: "/explore/repos?language=Go"},
		{Name: "idempotency", Link: "/explore/repos?q=idempotency&topic=1"},
		{Name: "retries", Link: "/explore/repos?q=retries&topic=1"},
	}, focus, "the main language only, never other")

	// a private repository gives its topics only to someone who may read it
	repo3 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})
	require.True(t, repo3.IsPrivate)
	repo3.Topics = []string{"payments-secret"}
	require.NoError(t, repo_model.UpdateRepositoryCols(ctx, repo3, "topics"))
	names := func(viewer *user_model.User) []string {
		_, focus := ascProfileRepos(ctx, user2, viewer)
		var out []string
		for _, f := range focus {
			out = append(out, f.Name)
		}
		return out
	}
	assert.Contains(t, names(user2), "payments-secret")
	assert.NotContains(t, names(user5), "payments-secret")
	assert.NotContains(t, names(nil), "payments-secret")
}
