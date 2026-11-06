// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Propose change bar's stack buttons belong to asc repositories: a git repository keeps its one Edit file button,
// even for a person with an open pull request onto the shown branch.
func TestAscProposeChangeBarGitRepository(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testEditFileToNewBranch(t, session, "user1", "repo1", "master", "retry", "README.md", "Retry with backoff\n")
		testPullCreate(t, session, "user1", "repo1", true, "master", "retry", "Retry with backoff")

		resp := session.MakeRequest(t, NewRequest(t, "GET", "/user1/repo1/src/branch/master/README.md"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		assert.Zero(t, doc.Find(".rp-editnote-acts").Length())
		edit := doc.Find(".rp-editnote a.ds-btn-primary")
		require.Equal(t, 1, edit.Length())
		assert.Equal(t, "/user1/repo1/_edit/master/README.md", edit.AttrOr("href", ""))
	})
}
