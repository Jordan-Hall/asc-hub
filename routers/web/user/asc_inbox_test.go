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
	ctx, resp := contexttest.MockContext(t, "/-/asc/inbox"+query)
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	AscInbox(ctx)
	return resp.Code, resp.Body.String()
}

func useAscInboxEntries(t *testing.T, es ...*ascinbox.Entry) {
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
			&ascinbox.Entry{PullID: 7001, Title: "other-pinned", Section: issues_model.AscInboxWaiting},
			&ascinbox.Entry{PullID: 7002, Title: "mine-snoozed", Section: issues_model.AscInboxWaiting})
		_, body := getAscInbox(t, user2, "")
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
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	es, err := ascinbox.Load(db.DefaultContext, stranger, &authz.AllAccessAuthorizationReducer{}, time.Now())
	require.NoError(t, err)
	for _, e := range es {
		assert.NotEqual(t, int64(2), e.PullID, "an uninvolved user does not see it")
	}
}
