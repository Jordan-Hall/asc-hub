// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/test"
	"forgejo.org/services/automerge"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows of "Needs you" on the dashboard act in place (POST /-/asc/dash/act): approve, add to the merge queue and
// the undo of it. asc is faked as in asc_queue_test.go.
func TestTheDashboardRowsActInPlace(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := newAscQueueEnv(t)
	act := func(s *TestSession, pull, do string, status int) *httptest.ResponseRecorder {
		t.Helper()
		return s.MakeRequest(t, NewRequestWithValues(t, "POST", "/-/asc/dash/act", map[string]string{"pull": pull, "do": do}), status)
	}

	t.Run("Add to merge queue, then Undo", func(t *testing.T) {
		session := loginUser(t, "user2")
		resp := act(session, "5", "queue", http.StatusSeeOther)
		assert.Equal(t, "/?queued=5", test.RedirectURL(resp))
		entries := ascQueueEntries(t, e.top.BaseRepoID)
		require.Len(t, entries, 2, "the stack up to #5")
		for _, en := range entries {
			assert.Equal(t, automerge.AscQueueQueued, en.State)
		}

		resp = act(session, "5", "unqueue", http.StatusSeeOther)
		assert.Equal(t, "/", test.RedirectURL(resp))
		for _, en := range ascQueueEntries(t, e.top.BaseRepoID) {
			assert.Equal(t, automerge.AscQueueCancelled, en.State)
		}
		assert.Contains(t, ascFlash(t, session), "success=")
	})

	t.Run("Approve", func(t *testing.T) {
		session := loginUser(t, "user2")
		resp := act(session, "2", "approve", http.StatusSeeOther)
		assert.Equal(t, "/", test.RedirectURL(resp))
		assert.Contains(t, ascFlash(t, session), "success=You approved user2/repo1#3.")
		unittest.AssertExistsAndLoadBean(t, &issues_model.Review{IssueID: 3, ReviewerID: 2, Type: issues_model.ReviewTypeApprove})
	})

	t.Run("One's own pull request is not approved", func(t *testing.T) {
		session := loginUser(t, "user1")
		before := unittest.GetCount(t, &issues_model.Review{IssueID: 3, ReviewerID: 1, Type: issues_model.ReviewTypeApprove})
		act(session, "2", "approve", http.StatusSeeOther)
		assert.Contains(t, ascFlash(t, session), "error=")
		assert.Equal(t, before, unittest.GetCount(t, &issues_model.Review{IssueID: 3, ReviewerID: 1, Type: issues_model.ReviewTypeApprove}))
	})

	t.Run("Someone who may not merge cannot queue", func(t *testing.T) {
		before := len(ascQueueEntries(t, e.top.BaseRepoID))
		session := loginUser(t, "user5")
		resp := act(session, "5", "queue", http.StatusSeeOther)
		assert.Equal(t, "/", test.RedirectURL(resp))
		assert.Contains(t, ascFlash(t, session), "error=")
		assert.Len(t, ascQueueEntries(t, e.top.BaseRepoID), before)
	})

	t.Run("A pull request one may not read is not found", func(t *testing.T) {
		// pull request 6 is in the private repository org3/repo3, which user5 may not read
		pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 6})
		require.NoError(t, pr.LoadBaseRepo(t.Context()))
		require.True(t, pr.BaseRepo.IsPrivate, "the fixture is private")
		act(loginUser(t, "user5"), "6", "approve", http.StatusNotFound)
		act(loginUser(t, "user5"), "999999", "approve", http.StatusNotFound)
	})

	t.Run("Bad forms", func(t *testing.T) {
		session := loginUser(t, "user2")
		act(session, "5", "merge", http.StatusBadRequest)
		act(session, "x", "approve", http.StatusBadRequest)
		act(session, "0", "approve", http.StatusBadRequest)
	})

	t.Run("Signed out", func(t *testing.T) {
		req := NewRequestWithValues(t, "POST", "/-/asc/dash/act", map[string]string{"pull": "5", "do": "queue", "_csrf": "x"})
		resp := MakeRequest(t, req, http.StatusSeeOther)
		u, err := url.Parse(test.RedirectURL(resp))
		require.NoError(t, err)
		assert.Equal(t, "/user/login", u.Path)
	})
}
