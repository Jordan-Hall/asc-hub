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
	"forgejo.org/services/context"
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

// callAscInbox calls the handler as doer (nil: not signed in) with the reducer given; ifNoneMatch, when not
// empty, is sent as If-None-Match.
func callAscInbox(t *testing.T, handler func(*context.APIContext), path string, doer *user_model.User, reducer authz.AuthorizationReducer, query, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, resp := contexttest.MockAPIContext(t, path+query)
	if ifNoneMatch != "" {
		ctx.Req.Header.Set("If-None-Match", ifNoneMatch)
	}
	ctx.SetDoer(doer)
	ctx.SetIsSigned(doer != nil)
	ctx.SetReducer(reducer)
	handler(ctx)
	return resp
}

// getAscInbox calls the list handler as doer (nil: not signed in) with the reducer given.
func getAscInbox(t *testing.T, doer *user_model.User, reducer authz.AuthorizationReducer, query string) *httptest.ResponseRecorder {
	t.Helper()
	return callAscInbox(t, ListAscInbox, "/api/v1/user/asc/inbox", doer, reducer, query, "")
}

// getAscInboxCount calls the count handler as doer (nil: not signed in) with the reducer given.
func getAscInboxCount(t *testing.T, doer *user_model.User, reducer authz.AuthorizationReducer, query string) *httptest.ResponseRecorder {
	t.Helper()
	return callAscInbox(t, CountAscInbox, "/api/v1/user/asc/inbox/count", doer, reducer, query, "")
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

	t.Run("ETag and If-None-Match", func(t *testing.T) {
		const path = "/api/v1/user/asc/inbox"
		resp := getAscInbox(t, reviewer, all, "")
		etag := resp.Header().Get("ETag")
		require.Regexp(t, `^"[0-9a-f]{32}"$`, etag)
		assert.Contains(t, resp.Header().Get("Vary"), "Authorization")
		assert.Contains(t, resp.Header().Get("Access-Control-Expose-Headers"), "ETag")
		assert.Equal(t, etag, getAscInbox(t, reviewer, all, "").Header().Get("ETag"), "the same inbox has the same ETag")

		for _, inm := range []string{etag, "W/" + etag, `"nope", ` + etag, "*"} {
			resp := callAscInbox(t, ListAscInbox, path, reviewer, all, "", inm)
			assert.Equal(t, http.StatusNotModified, resp.Code, inm)
			assert.Empty(t, resp.Body.String(), inm)
			assert.Equal(t, etag, resp.Header().Get("ETag"), inm)
			assert.Equal(t, "1", resp.Header().Get("X-Total-Count"), inm)
			assert.Empty(t, resp.Header().Get("Content-Type"), inm)
		}

		resp = callAscInbox(t, ListAscInbox, path, reviewer, all, "", `"stale"`)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, etag, resp.Header().Get("ETag"))
		assert.NotEmpty(t, resp.Body.String())

		// a validator of another query or another user never matches
		assert.Equal(t, http.StatusOK, callAscInbox(t, ListAscInbox, path, reviewer, all, "?section=merged", etag).Code)
		authorETag := getAscInbox(t, author, all, "").Header().Get("ETag")
		assert.NotEqual(t, etag, authorETag)
		assert.Equal(t, http.StatusOK, callAscInbox(t, ListAscInbox, path, reviewer, all, "", authorETag).Code)

		// the validator follows the content: a pin changes it, an unpin brings it back
		pinned := true
		_, err := issues_model.SetAscInboxState(db.DefaultContext, reviewerID, 2, &pinned, nil, time.Now())
		require.NoError(t, err)
		resp = callAscInbox(t, ListAscInbox, path, reviewer, all, "", etag)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NotEqual(t, etag, resp.Header().Get("ETag"))
		pinned = false
		_, err = issues_model.SetAscInboxState(db.DefaultContext, reviewerID, 2, &pinned, nil, time.Now())
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotModified, callAscInbox(t, ListAscInbox, path, reviewer, all, "", etag).Code)

		// errors carry no validator and are never 304
		resp = callAscInbox(t, ListAscInbox, path, nil, all, "", "*")
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assert.Empty(t, resp.Header().Get("ETag"))
		assert.Equal(t, http.StatusUnprocessableEntity, callAscInbox(t, ListAscInbox, path, reviewer, all, "?section=nope", "*").Code)
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

func TestAscInboxETagMatches(t *testing.T) {
	const etag = `"abc"`
	for _, tc := range []struct {
		values []string
		want   bool
	}{
		{nil, false},
		{[]string{""}, false},
		{[]string{etag}, true},
		{[]string{"W/" + etag}, true},
		{[]string{`"x", "abc"`}, true},
		{[]string{` "x" , W/"abc" `}, true},
		{[]string{`"x"`, etag}, true},
		{[]string{"*"}, true},
		{[]string{`"x", *`}, true},
		{[]string{`"abcd"`}, false},
		{[]string{`abc`}, false},
		{[]string{`"ab"`}, false},
		{[]string{`w/"abc"`}, false},
	} {
		assert.Equal(t, tc.want, ascInboxETagMatches(tc.values, etag), "%q", tc.values)
	}
}

func TestAscInboxETagCoversTotal(t *testing.T) {
	// the same page body with another total must not be answered 304
	body := []byte("[]\n")
	assert.Equal(t, ascInboxETag(body, 3), ascInboxETag(body, 3))
	assert.NotEqual(t, ascInboxETag(body, 0), ascInboxETag(body, 1))
	assert.NotEqual(t, ascInboxETag(body, 0), ascInboxETag([]byte("[{}]\n"), 0))
}

func decodeAscInboxCount(t *testing.T, resp *httptest.ResponseRecorder) *api.AscInboxCount {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Header().Get("Content-Type"), "application/json")
	out := &api.AscInboxCount{}
	contexttest.DecodeJSON(t, resp, out)
	return out
}

func TestCountAscInbox(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	all := &authz.AllAccessAuthorizationReducer{}
	public := &authz.PublicReposAuthorizationReducer{}

	const reviewerID = 4
	reviewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: reviewerID})
	author := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	require.True(t, issue.IsPull)
	require.NoError(t, db.Insert(db.DefaultContext, &issues_model.Review{
		Type: issues_model.ReviewTypeRequest, IssueID: issue.ID, ReviewerID: reviewerID,
	}))
	setPrivate := func(p bool) {
		_, err := db.GetEngine(db.DefaultContext).ID(issue.RepoID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: p})
		require.NoError(t, err)
	}
	t.Cleanup(func() { setPrivate(false) })
	setPrivate(false)

	t.Run("counts in total and by section", func(t *testing.T) {
		resp := getAscInboxCount(t, reviewer, all, "")
		out := decodeAscInboxCount(t, resp)
		assert.Equal(t, int64(1), out.Total)
		assert.Equal(t, "1", resp.Header().Get("X-Total-Count"))
		require.Len(t, out.Sections, len(ascinbox.SectionOrder), "every section is listed, zero included")
		var sum int64
		for _, s := range ascinbox.SectionOrder {
			want := int64(0)
			if s == issues_model.AscInboxNeedsReview {
				want = 1
			}
			assert.Equal(t, want, out.Sections[string(s)], s)
			sum += out.Sections[string(s)]
		}
		assert.Equal(t, out.Total, sum)
	})

	t.Run("agrees with the list", func(t *testing.T) {
		for _, u := range []*user_model.User{reviewer, author} {
			for _, q := range []string{"", "?include_snoozed=true", "?section=needs_review", "?section=merged"} {
				list := getAscInbox(t, u, all, q)
				count := getAscInboxCount(t, u, all, q)
				assert.Equal(t, list.Header().Get("X-Total-Count"), count.Header().Get("X-Total-Count"), "%d %s", u.ID, q)
				assert.Equal(t, int64(len(decodeAscInbox(t, list))), decodeAscInboxCount(t, count).Total, "%d %s", u.ID, q)
			}
		}
	})

	t.Run("section filter, page and limit", func(t *testing.T) {
		out := decodeAscInboxCount(t, getAscInboxCount(t, reviewer, all, "?section=needs_review,merged"))
		assert.Equal(t, int64(1), out.Total)
		assert.Equal(t, map[string]int64{"needs_review": 1, "merged": 0}, out.Sections)

		out = decodeAscInboxCount(t, getAscInboxCount(t, reviewer, all, "?section=merged"))
		assert.Equal(t, int64(0), out.Total)
		assert.Equal(t, map[string]int64{"merged": 0}, out.Sections)

		out = decodeAscInboxCount(t, getAscInboxCount(t, reviewer, all, "?page=9&limit=1"))
		assert.Equal(t, int64(1), out.Total, "page and limit do not change the count")

		assert.Equal(t, http.StatusUnprocessableEntity, getAscInboxCount(t, reviewer, all, "?section=nope").Code)
	})

	t.Run("anonymous, no reducer and disabled", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, getAscInboxCount(t, nil, all, "").Code)
		assert.Equal(t, http.StatusInternalServerError, getAscInboxCount(t, reviewer, nil, "").Code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.Equal(t, http.StatusNotFound, getAscInboxCount(t, reviewer, all, "").Code)
	})

	t.Run("read access is rechecked, including the reducer of the token", func(t *testing.T) {
		setPrivate(true)
		defer setPrivate(false)
		assert.Equal(t, int64(0), decodeAscInboxCount(t, getAscInboxCount(t, reviewer, all, "")).Total, "a reviewer without access counts nothing")
		assert.Positive(t, decodeAscInboxCount(t, getAscInboxCount(t, author, all, "")).Total)
		resp := getAscInboxCount(t, author, public, "")
		assert.Equal(t, int64(0), decodeAscInboxCount(t, resp).Total, "a public-only token does not count a private repository")
		assert.Equal(t, "0", resp.Header().Get("X-Total-Count"))
	})

	t.Run("ETag and If-None-Match", func(t *testing.T) {
		const path = "/api/v1/user/asc/inbox/count"
		now := time.Now()

		resp := getAscInboxCount(t, reviewer, all, "")
		etag := resp.Header().Get("ETag")
		require.Regexp(t, `^"[0-9a-f]{32}"$`, etag)
		assert.Equal(t, etag, getAscInboxCount(t, reviewer, all, "").Header().Get("ETag"))

		for _, inm := range []string{etag, "W/" + etag, "*"} {
			resp := callAscInbox(t, CountAscInbox, path, reviewer, all, "", inm)
			assert.Equal(t, http.StatusNotModified, resp.Code, inm)
			assert.Empty(t, resp.Body.String(), inm)
			assert.Equal(t, etag, resp.Header().Get("ETag"), inm)
			assert.Equal(t, "1", resp.Header().Get("X-Total-Count"), inm)
		}
		assert.Equal(t, http.StatusOK, callAscInbox(t, CountAscInbox, path, reviewer, all, "", `"stale"`).Code)
		assert.Equal(t, http.StatusOK, callAscInbox(t, CountAscInbox, path, reviewer, all, "?section=merged", etag).Code)

		// a snooze takes the pull request out of the count, which changes the validator
		snooze := now.Add(time.Hour).Truncate(time.Second)
		_, err := issues_model.SetAscInboxState(db.DefaultContext, reviewerID, 2, nil, &snooze, now)
		require.NoError(t, err)
		resp = callAscInbox(t, CountAscInbox, path, reviewer, all, "", etag)
		assert.Equal(t, http.StatusOK, resp.Code)
		assert.NotEqual(t, etag, resp.Header().Get("ETag"))
		assert.Equal(t, int64(0), decodeAscInboxCount(t, resp).Total, "snoozed pull requests are not counted")
		assert.Equal(t, int64(1), decodeAscInboxCount(t, getAscInboxCount(t, reviewer, all, "?include_snoozed=true")).Total, "unless asked for")

		resp = callAscInbox(t, CountAscInbox, path, nil, all, "", "*")
		assert.Equal(t, http.StatusUnauthorized, resp.Code)
		assert.Empty(t, resp.Header().Get("ETag"))
	})
}
