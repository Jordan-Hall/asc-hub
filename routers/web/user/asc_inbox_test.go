// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/authz"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAscInbox(t *testing.T, doer *user_model.User, query string) (int, string) {
	t.Helper()
	oldInfo := ascInboxInfoLoad
	ascInboxInfoLoad = func(gocontext.Context, *AscInboxEntry) *AscInboxInfo { return &AscInboxInfo{} }
	t.Cleanup(func() { ascInboxInfoLoad = oldInfo })
	ctx, resp := contexttest.MockContext(t, "/-/asc/inbox"+query)
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	AscInbox(ctx)
	return resp.Code, resp.Body.String()
}

func postAscInboxState(t *testing.T, doer *user_model.User, form map[string]string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /-/asc/inbox/state")
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	for k, v := range form {
		ctx.Req.Form.Set(k, v)
	}
	AscInboxStatePost(ctx)
	return resp.Code, resp.Header().Get("Location")
}

func useAscInboxEntries(t *testing.T, es ...*AscInboxEntry) {
	t.Helper()
	old := ascInboxLoad
	ascInboxLoad = func(gocontext.Context, *user_model.User, time.Time) ([]*ascinbox.Entry, error) { return es, nil }
	t.Cleanup(func() { ascInboxLoad = old })
}

func TestAscInboxPageGroup(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	t0 := now.Add(-time.Hour)
	es := []*ascinbox.Entry{
		{PullID: 1, Section: issues_model.AscInboxWaiting, Updated: t0},
		{PullID: 2, Section: issues_model.AscInboxNeedsReview, Updated: t0},
		{PullID: 3, Section: issues_model.AscInboxNeedsReview, Updated: t0.Add(time.Minute)},
		{PullID: 4, Section: issues_model.AscInboxNeedsReview, Updated: t0.Add(2 * time.Minute)},
		{PullID: 5, Section: issues_model.AscInboxMerged, Updated: t0},
	}
	states := map[int64]*issues_model.AscInboxState{
		2: {Pinned: true},
		4: {SnoozedUntil: timeutil.TimeStamp(now.Add(time.Hour).Unix())},
		5: {SnoozedUntil: timeutil.TimeStamp(now.Add(-time.Hour).Unix())}, // expired
	}
	groups, snoozed := ascInboxGroup(es, states, now)
	assert.Equal(t, 1, snoozed)
	require.Len(t, groups, 3)
	assert.Equal(t, issues_model.AscInboxNeedsReview, groups[0].Section)
	require.Len(t, groups[0].Entries, 2)
	assert.Equal(t, int64(2), groups[0].Entries[0].PullID, "pinned first")
	assert.Equal(t, int64(3), groups[0].Entries[1].PullID)
	assert.Equal(t, issues_model.AscInboxWaiting, groups[1].Section)
	assert.Equal(t, issues_model.AscInboxMerged, groups[2].Section, "an expired snooze shows again")
}

func TestAscInboxPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	t.Run("renders sections and escapes titles", func(t *testing.T) {
		useAscInboxEntries(t, &ascinbox.Entry{PullID: 1, Title: "<script>alert(1)</script>", HTMLURL: "/o/r/pulls/1",
			RepoFullName: "o/r", Index: 1, Section: issues_model.AscInboxNeedsReview})
		code, body := getAscInbox(t, user2, "?user=user1&section=merged")
		assert.Equal(t, http.StatusOK, code)
		assert.NotContains(t, body, "<script>alert(1)</script>")
		assert.Contains(t, body, "&lt;script&gt;")
		assert.Contains(t, body, `data-section="needs_review"`)
	})

	t.Run("only the signed-in user's marks apply", func(t *testing.T) {
		now := time.Now()
		pinned := true
		_, err := issues_model.SetAscInboxState(db.DefaultContext, 1, 7001, &pinned, nil, now)
		require.NoError(t, err)
		snooze := now.Add(time.Hour)
		_, err = issues_model.SetAscInboxState(db.DefaultContext, 2, 7002, nil, &snooze, now)
		require.NoError(t, err)
		useAscInboxEntries(t,
			&AscInboxEntry{PullID: 7001, Title: "other-pinned", Section: issues_model.AscInboxWaiting},
			&AscInboxEntry{PullID: 7002, Title: "mine-snoozed", Section: issues_model.AscInboxWaiting})
		_, body := getAscInbox(t, user2, "?tab=waiting")
		assert.Contains(t, body, "other-pinned")
		states, err := issues_model.ListAscInboxStates(db.DefaultContext, user2.ID, []int64{7001, 7002})
		require.NoError(t, err)
		assert.NotContains(t, states, int64(7001), "user1's pin is not user2's")
		assert.NotContains(t, body, "mine-snoozed")
	})

	t.Run("anonymous and disabled", func(t *testing.T) {
		useAscInboxEntries(t)
		code, _ := getAscInbox(t, nil, "")
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _ = getAscInbox(t, user2, "")
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestAscInboxLoad(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	const reviewerID = 4
	reviewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: reviewerID})
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	require.True(t, issue.IsPull)
	require.NoError(t, issue.LoadRepo(db.DefaultContext))
	repoID := issue.RepoID
	require.NoError(t, db.Insert(db.DefaultContext, &issues_model.Review{
		Type: issues_model.ReviewTypeRequest, IssueID: issue.ID, ReviewerID: reviewerID,
	}))
	setPrivate := func(p bool) {
		_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: p})
		require.NoError(t, err)
	}
	find := func() *ascinbox.Entry {
		es, err := ascinbox.Load(db.DefaultContext, reviewer, &authz.AllAccessAuthorizationReducer{}, time.Now())
		require.NoError(t, err)
		for _, e := range es {
			if e.PullID == 2 {
				return e
			}
		}
		return nil
	}

	setPrivate(false)
	e := find()
	require.NotNil(t, e, "a requested reviewer sees the pull request")
	assert.Equal(t, issues_model.AscInboxNeedsReview, e.Section)

	t.Cleanup(func() { setPrivate(false) })
	setPrivate(true)
	assert.Nil(t, find(), "a reviewer who cannot read the repository does not see its pull request")

	setPrivate(false)
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
	es, err := ascinbox.Load(db.DefaultContext, stranger, &authz.AllAccessAuthorizationReducer{}, time.Now())
	require.NoError(t, err)
	for _, e := range es {
		assert.NotEqual(t, int64(2), e.PullID, "an uninvolved user does not see it")
	}
}

func TestAscInboxTabsAndDetail(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	now := time.Now()
	useAscInboxEntries(t,
		&AscInboxEntry{PullID: 11, Title: "review me", Link: "/o/r/pulls/1", RepoFullName: "o/r", Index: 1, Section: issues_model.AscInboxNeedsReview, Updated: now, Author: "alice"},
		&AscInboxEntry{PullID: 12, Title: "mine ready", Link: "/o/s/pulls/2", RepoFullName: "o/s", Index: 2, Section: issues_model.AscInboxReady, Updated: now.Add(-time.Hour), Author: "user2"},
		&AscInboxEntry{PullID: 13, Title: "waiting one", Link: "/o/r/pulls/3", RepoFullName: "o/r", Index: 3, Section: issues_model.AscInboxWaiting, Updated: now},
		&AscInboxEntry{PullID: 14, Title: "merged one", Link: "/o/r/pulls/4", RepoFullName: "o/r", Index: 4, Section: issues_model.AscInboxMerged, Updated: now},
	)
	oldComments := ascInboxCommentsLoad
	ascInboxCommentsLoad = func(_ gocontext.Context, id int64) []AscInboxComment {
		return []AscInboxComment{{Poster: user2, Body: "<b>hello</b>", Created: now}}
	}
	t.Cleanup(func() { ascInboxCommentsLoad = oldComments })

	t.Run("tabs count the sections and the first item is selected", func(t *testing.T) {
		code, body := getAscInbox(t, user2, "")
		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, "review me")
		assert.Contains(t, body, "mine ready")
		assert.NotContains(t, body, "waiting one")
		assert.NotContains(t, body, "merged one")
		assert.Contains(t, body, `aria-current="true"`)
		// the thread is escaped
		assert.NotContains(t, body, "<b>hello</b>")
		assert.Contains(t, body, "&lt;b&gt;hello&lt;/b&gt;")
		// a reviewer who is not the author may approve and request changes
		assert.Contains(t, body, `name="type" value="approve"`)
		assert.Contains(t, body, `name="type" value="reject"`)
	})

	t.Run("the author cannot approve their own pull request", func(t *testing.T) {
		_, body := getAscInbox(t, user2, "?pull=12")
		assert.Contains(t, body, "mine ready")
		assert.NotContains(t, body, `name="type" value="approve"`)
		assert.Contains(t, body, `name="type" value="comment"`)
	})

	t.Run("an unknown tab is the first tab and an unknown repository filters nothing", func(t *testing.T) {
		_, body := getAscInbox(t, user2, "?tab=nonsense&repo=evil/repo")
		assert.Contains(t, body, "review me")
		assert.Contains(t, body, "mine ready")
	})

	t.Run("the repository filter narrows", func(t *testing.T) {
		_, body := getAscInbox(t, user2, "?repo=o/s")
		assert.NotContains(t, body, "review me")
		assert.Contains(t, body, "mine ready")
	})

	t.Run("done and waiting tabs", func(t *testing.T) {
		_, body := getAscInbox(t, user2, "?tab=done")
		assert.Contains(t, body, "merged one")
		assert.NotContains(t, body, "waiting one")
		_, body = getAscInbox(t, user2, "?tab=waiting")
		assert.Contains(t, body, "waiting one")
	})

	t.Run("a pull request id outside the tab selects the first item", func(t *testing.T) {
		_, body := getAscInbox(t, user2, "?pull=14")
		assert.Contains(t, body, "review me")
	})
}

func TestAscInboxStatePost(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	useAscInboxEntries(t, &AscInboxEntry{PullID: 31, Title: "t", Section: issues_model.AscInboxNeedsReview})

	mark := func(u *user_model.User) *issues_model.AscInboxState {
		st, err := issues_model.GetAscInboxState(db.DefaultContext, u.ID, 31)
		require.NoError(t, err)
		return st
	}

	code, loc := postAscInboxState(t, user2, map[string]string{"pull": "31", "do": "pin", "tab": "waiting"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "/-/asc/inbox?tab=waiting", loc)
	require.NotNil(t, mark(user2))
	assert.True(t, mark(user2).Pinned)
	assert.Nil(t, mark(user4), "only the signed-in person's mark is written")

	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "31", "do": "snooze", "for": "1h"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.True(t, mark(user2).IsSnoozed(time.Now()))
	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "31", "do": "wake"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.False(t, mark(user2).IsSnoozed(time.Now()))
	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "31", "do": "unpin"})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Nil(t, mark(user2), "no mark left, no row")

	for name, form := range map[string]map[string]string{
		"unknown action":   {"pull": "31", "do": "delete"},
		"bad snooze":       {"pull": "31", "do": "snooze", "for": "forever"},
		"no pull":          {"do": "pin"},
		"negative pull":    {"pull": "-3", "do": "pin"},
		"not a number":     {"pull": "abc", "do": "pin"},
		"pull of no inbox": {"pull": "999", "do": "pin"},
	} {
		code, _ := postAscInboxState(t, user2, form)
		assert.NotEqual(t, http.StatusSeeOther, code, name)
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound}, code, name)
	}
	assert.Nil(t, mark(user2))

	code, _ = postAscInboxState(t, nil, map[string]string{"pull": "31", "do": "pin"})
	assert.Equal(t, http.StatusNotFound, code)
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	code, _ = postAscInboxState(t, user2, map[string]string{"pull": "31", "do": "pin"})
	assert.Equal(t, http.StatusNotFound, code)
}

func TestAscInboxSnoozeUntil(t *testing.T) {
	now := time.Date(2026, 10, 26, 22, 0, 0, 0, time.UTC)
	got, ok := ascInboxSnoozeUntil("1h", now)
	assert.True(t, ok)
	assert.Equal(t, now.Add(time.Hour), got)
	got, ok = ascInboxSnoozeUntil("week", now)
	assert.True(t, ok)
	assert.Equal(t, now.AddDate(0, 0, 7), got)
	got, ok = ascInboxSnoozeUntil("tomorrow", now)
	assert.True(t, ok)
	assert.True(t, got.After(now))
	assert.Equal(t, 9, got.Hour())
	_, ok = ascInboxSnoozeUntil("", now)
	assert.False(t, ok)
	assert.Equal(t, "short", ascInboxClip("short", 100))
	assert.Equal(t, "ab…", ascInboxClip("abcdef", 2))
	assert.Equal(t, "é…", ascInboxClip("ééé", 3), "never cuts a character")
}

func TestAscInboxCount(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	now := time.Now()
	useAscInboxEntries(t,
		&AscInboxEntry{PullID: 41, Section: issues_model.AscInboxNeedsReview, Updated: now},
		&AscInboxEntry{PullID: 42, Section: issues_model.AscInboxBlocked, Updated: now},
		&AscInboxEntry{PullID: 43, Section: issues_model.AscInboxWaiting, Updated: now},
		&AscInboxEntry{PullID: 44, Section: issues_model.AscInboxMerged, Updated: now},
	)
	get := func(u *user_model.User) (int, string, string) {
		ctx, resp := contexttest.MockContext(t, "/-/asc/inbox/count")
		ctx.Doer = u
		AscInboxCount(ctx)
		return resp.Code, resp.Body.String(), resp.Header().Get("Cache-Control")
	}
	code, body, cc := get(user2)
	assert.Equal(t, http.StatusOK, code)
	assert.JSONEq(t, `{"count":2}`, body, "only what needs the person is counted")
	assert.Contains(t, cc, "no-store")
	assert.Contains(t, cc, "private")

	// a snoozed item does not count
	until := now.Add(time.Hour)
	_, err := issues_model.SetAscInboxState(db.DefaultContext, user2.ID, 42, nil, &until, now)
	require.NoError(t, err)
	_, body, _ = get(user2)
	assert.JSONEq(t, `{"count":1}`, body)

	code, _, _ = get(nil)
	assert.Equal(t, http.StatusNotFound, code)
}
