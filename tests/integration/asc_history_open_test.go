// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The History of a git repository keeps its plain list: no open changes section and no landed stack brackets, even
// with an open pull request onto the branch.
func TestAscHistoryOpenChangesGitRepository(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testEditFileToNewBranch(t, session, "user1", "repo1", "master", "retry", "README.md", "Retry with backoff\n")
		testPullCreate(t, session, "user1", "repo1", true, "master", "retry", "Retry with backoff")

		resp := session.MakeRequest(t, NewRequest(t, "GET", "/user1/repo1/commits/branch/master"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		assert.Zero(t, doc.Find(`[data-testid="asc-open-changes"]`).Length())
		assert.Zero(t, doc.Find(`[data-landed]`).Length())
		assert.Positive(t, doc.Find(".rp-commit").Length(), "the history still lists the changes")
	})
}
