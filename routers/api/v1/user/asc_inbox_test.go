// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/authz"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscInboxSections(t *testing.T) {
	got, err := ascInboxSections(nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = ascInboxSections([]string{"needs_review,returned", " merged ", "needs_review", ""})
	require.NoError(t, err)
	assert.Equal(t, []issues_model.AscInboxSection{
		issues_model.AscInboxNeedsReview, issues_model.AscInboxReturned, issues_model.AscInboxMerged,
	}, got, "repeated and comma separated values are merged without duplicates")

	for _, bad := range [][]string{
		{"nope"},
		{"needs_review,nope"},
		{"NEEDS_REVIEW"},
		{strings.Repeat("a", 33)},
		{strings.Repeat("needs_review,", 1000)},
		make([]string, ascInboxMaxSectionValues+1),
	} {
		_, err := ascInboxSections(bad)
		require.ErrorIs(t, err, errAscInboxBadSection)
	}
}

func ascInboxPullIDs(es []*ascinbox.Entry) []int64 {
	out := make([]int64, 0, len(es))
	for _, e := range es {
		out = append(out, e.PullID)
	}
	return out
}

func TestAscInboxPage(t *testing.T) {
	es := []*ascinbox.Entry{{PullID: 1}, {PullID: 2}, {PullID: 3}, {PullID: 4}, {PullID: 5}}
	assert.Equal(t, []int64{1, 2}, ascInboxPullIDs(ascInboxPage(es, 1, 2)))
	assert.Equal(t, []int64{3, 4}, ascInboxPullIDs(ascInboxPage(es, 2, 2)))
	assert.Equal(t, []int64{5}, ascInboxPullIDs(ascInboxPage(es, 3, 2)))
	assert.Empty(t, ascInboxPage(es, 4, 2))
	assert.Equal(t, []int64{1, 2, 3, 4, 5}, ascInboxPullIDs(ascInboxPage(es, 0, 50)), "page 0 is page 1")
	assert.Empty(t, ascInboxPage(es, 1, 0))
	assert.Empty(t, ascInboxPage(nil, 1, 10))
	assert.Empty(t, ascInboxPage(es, int(^uint(0)>>1), 50), "no overflow")
}

// getAscInbox calls the handler as doer (nil: not signed in) with the reducer given.
func getAscInbox(t *testing.T, doer *user_model.User, reducer authz.AuthorizationReducer, query string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, resp := contexttest.MockAPIContext(t, "/api/v1/user/asc/inbox"+query)
	ctx.SetDoer(doer)
	ctx.SetIsSigned(doer != nil)
	ctx.SetReducer(reducer)
	ListAscInbox(ctx)
	return resp
}

func decodeAscInbox(t *testing.T, resp *httptest.ResponseRecorder) []*api.AscInboxEntry {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Header().Get("Content-Type"), "application/json")
	var out []*api.AscInboxEntry
	contexttest.DecodeJSON(t, resp, &out)
	return out
}

func TestListAscInbox(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	all := &authz.AllAccessAuthorizationReducer{}
	public := &authz.PublicReposAuthorizationReducer{}

	const reviewerID = 4
	reviewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: reviewerID})
	author := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	require.True(t, issue.IsPull)
	require.Equal(t, int64(1), issue.PosterID)
	repoID := issue.RepoID
	require.NoError(t, db.Insert(db.DefaultContext, &issues_model.Review{
		Type: issues_model.ReviewTypeRequest, IssueID: issue.ID, ReviewerID: reviewerID,
	}))
	setPrivate := func(p bool) {
		_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: p})
		require.NoError(t, err)
	}
	t.Cleanup(func() { setPrivate(false) })
	setPrivate(false)

	t.Run("lists the doer's pull request with its section", func(t *testing.T) {
		resp := getAscInbox(t, reviewer, all, "")
		out := decodeAscInbox(t, resp)
		require.Len(t, out, 1)
		e := out[0]
		assert.Equal(t, int64(2), e.PullRequestID)
		assert.Equal(t, int64(3), e.Number)
		assert.Equal(t, "needs_review", e.Section)
		assert.Equal(t, "issue3", e.Title)
		assert.Contains(t, e.HTMLURL, "/pulls/3")
		assert.NotEmpty(t, e.Repository)
		assert.False(t, e.Pinned)
		assert.Nil(t, e.SnoozedUntil)
		assert.False(t, e.Updated.IsZero())
		assert.Equal(t, "1", resp.Header().Get("X-Total-Count"))
	})

	t.Run("no parameter names another user", func(t *testing.T) {
		out := decodeAscInbox(t, getAscInbox(t, reviewer, all, "?user=user1&uid=1&username=user1&doer=1"))
		require.Len(t, out, 1)
		assert.Equal(t, "needs_review", out[0].Section, "the reviewer's own section, not the author's")
	})

	t.Run("section filter", func(t *testing.T) {
		assert.Len(t, decodeAscInbox(t, getAscInbox(t, reviewer, all, "?section=needs_review")), 1)
		assert.Len(t, decodeAscInbox(t, getAscInbox(t, reviewer, all, "?section=merged,needs_review")), 1)
		assert.Len(t, decodeAscInbox(t, getAscInbox(t, reviewer, all, "?section=merged&section=needs_review")), 1)
		resp := getAscInbox(t, reviewer, all, "?section=merged")
		assert.Equal(t, "0", resp.Header().Get("X-Total-Count"))
		assert.Equal(t, "[]", strings.TrimSpace(resp.Body.String()), "an empty page is an empty array, not null")
	})

	t.Run("bad section is refused", func(t *testing.T) {
		resp := getAscInbox(t, reviewer, all, "?section=nope")
		assert.Equal(t, http.StatusUnprocessableEntity, resp.Code)
	})

	t.Run("anonymous, no reducer and disabled", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, getAscInbox(t, nil, all, "").Code)
		assert.Equal(t, http.StatusInternalServerError, getAscInbox(t, reviewer, nil, "").Code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.Equal(t, http.StatusNotFound, getAscInbox(t, reviewer, all, "").Code)
	})

	t.Run("read access is rechecked, including the reducer of the token", func(t *testing.T) {
		setPrivate(true)
		defer setPrivate(false)
		assert.Empty(t, decodeAscInbox(t, getAscInbox(t, reviewer, all, "")), "a reviewer without access to the repository sees nothing")
		assert.NotEmpty(t, decodeAscInbox(t, getAscInbox(t, author, all, "")))
		assert.Empty(t, decodeAscInbox(t, getAscInbox(t, author, public, "")), "a public-only token does not list a private repository")
	})

	t.Run("marks are the doer's own", func(t *testing.T) {
		now := time.Now()
		pinned := true
		_, err := issues_model.SetAscInboxState(db.DefaultContext, author.ID, 2, &pinned, nil, now)
		require.NoError(t, err)
		snooze := now.Add(time.Hour).Truncate(time.Second)
		_, err = issues_model.SetAscInboxState(db.DefaultContext, reviewerID, 2, nil, &snooze, now)
		require.NoError(t, err)

		for _, e := range decodeAscInbox(t, getAscInbox(t, author, all, "")) {
			if e.PullRequestID == 2 {
				assert.True(t, e.Pinned)
				assert.Nil(t, e.SnoozedUntil)
			}
		}
		assert.Empty(t, decodeAscInbox(t, getAscInbox(t, reviewer, all, "")), "snoozed pull requests are left out")
		out := decodeAscInbox(t, getAscInbox(t, reviewer, all, "?include_snoozed=true"))
		require.Len(t, out, 1)
		assert.False(t, out[0].Pinned, "the author's pin is not the reviewer's")
		require.NotNil(t, out[0].SnoozedUntil)
		assert.Equal(t, snooze.Unix(), out[0].SnoozedUntil.Unix())
	})

	t.Run("paging walks the whole list in order", func(t *testing.T) {
		resp := getAscInbox(t, author, all, "?include_snoozed=true")
		full := decodeAscInbox(t, resp)
		require.NotEmpty(t, full)
		total := resp.Header().Get("X-Total-Count")
		assert.Equal(t, strconv.Itoa(len(full)), total)

		var walked, want []int64
		for page := 1; page <= len(full)+1; page++ {
			resp := getAscInbox(t, author, all, "?include_snoozed=true&limit=1&page="+strconv.Itoa(page))
			out := decodeAscInbox(t, resp)
			assert.Equal(t, total, resp.Header().Get("X-Total-Count"))
			assert.LessOrEqual(t, len(out), 1)
			for _, e := range out {
				walked = append(walked, e.PullRequestID)
			}
		}
		for _, e := range full {
			want = append(want, e.PullRequestID)
		}
		assert.Equal(t, want, walked)
	})
}
