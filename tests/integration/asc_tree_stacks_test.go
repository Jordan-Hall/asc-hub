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

// The folder rail of the code page counts the open stacks onto the shown branch that change each file.
func TestAscTreeTouchedByOpenStacks(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		session := loginUser(t, "user1")
		testRepoFork(t, session, "user2", "repo1", "user1", "repo1")
		testEditFileToNewBranch(t, session, "user1", "repo1", "master", "retry", "README.md", "Retry with backoff\n")
		testPullCreate(t, session, "user1", "repo1", true, "master", "retry", "Retry with backoff")

		resp := session.MakeRequest(t, NewRequest(t, "GET", "/user1/repo1/src/branch/master/README.md"), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		badge := doc.Find(`.rp-tree-file[aria-current="page"] .rp-tree-count`)
		require.Equal(t, 1, badge.Length(), "README.md is changed by one open stack")
		assert.Equal(t, "Touched by open stacks", badge.AttrOr("title", ""))
		assert.Contains(t, badge.Text(), "1")
		assert.Contains(t, doc.Find(".rp-tree-hint").Text(), "file touched by open stacks")

		// the stack's own branch is not the base of an open stack: no counts
		resp = session.MakeRequest(t, NewRequest(t, "GET", "/user1/repo1/src/branch/retry/README.md"), http.StatusOK)
		assert.Zero(t, NewHTMLParser(t, resp.Body).Find(".rp-tree-count").Length())
	})
}
