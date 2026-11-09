// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscProfileKnows checks Knows best on a profile (board Profile): the directories of the pull requests the
// person opened in the last six months, read from the hub's repository and kept per head.
func TestAscProfileKnows(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// nothing in the last six months: no card
	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user1"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-pf-knows]", false)

	// user1 opened the pull requests 2 and 3 of user2/repo1: make them recent
	now := time.Now().Unix()
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE issue SET created_unix = ?, updated_unix = ? WHERE id IN (2, 3)", now, now)
	require.NoError(t, err)

	page = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user1"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-pf-knows] .wk-knows li", true)
	assert.Contains(t, page.Find("[data-asc-pf-knows] .wk-knows-row strong").First().Text(), "%")
	kept := unittest.GetCount(t, &issues_model.PullRequestAscDirs{})
	assert.Positive(t, kept, "the directories are kept for the next view")

	again := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user1"), http.StatusOK).Body)
	assert.Equal(t, page.Find("[data-asc-pf-knows]").Text(), again.Find("[data-asc-pf-knows]").Text())
	assert.Equal(t, kept, unittest.GetCount(t, &issues_model.PullRequestAscDirs{}))
}
