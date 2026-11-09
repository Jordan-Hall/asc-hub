// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheDirectoriesShowPublicActivity(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := db.GetEngine(db.DefaultContext)
	act := func(actor, repoID int64, ago time.Duration) {
		_, err := e.NoAutoTime().Insert(&activities_model.Action{
			UserID: actor, ActUserID: actor, RepoID: repoID, OpType: activities_model.ActionCreateIssue,
			CreatedUnix: timeutil.TimeStamp(time.Now().Add(-ago).Unix()),
		})
		require.NoError(t, err)
	}
	act(2, 1, time.Hour)       // user2 in its public repo1
	act(2, 1, 2*24*time.Hour)  // and again this week
	act(5, 23, 30*time.Minute) // user5 in repo23 of org17
	act(2, 2, time.Minute)     // repo2 is private: not counted
	act(5, 41, 10*time.Minute) // repo41 of privated_org is private: not counted

	row := func(t *testing.T, sess *TestSession, url, name string) *goquery.Selection {
		t.Helper()
		req := NewRequest(t, "GET", url)
		var resp *httptest.ResponseRecorder
		if sess == nil {
			resp = MakeRequest(t, req, http.StatusOK)
		} else {
			resp = sess.MakeRequest(t, req, http.StatusOK)
		}
		doc := NewHTMLParser(t, resp.Body)
		found := doc.doc.Find(".ds-user-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.TrimSpace(s.Find(".ds-li-title > a").First().Text()) == name
		})
		require.Equal(t, 1, found.Length(), "a row for %s", name)
		return found
	}

	t.Run("A person shows the actions of the last weeks, the changesets and the reviews", func(t *testing.T) {
		r := row(t, nil, "/explore/users?q=user2", "user2")
		a := r.Find("[data-asc-activity]")
		require.Equal(t, 1, a.Length())
		assert.Equal(t, "2", a.AttrOr("data-total", ""))
		assert.Equal(t, "2 public actions in the last 12 weeks", a.AttrOr("aria-label", ""))
		assert.Equal(t, "img", a.AttrOr("role", ""))
		assert.Equal(t, 1, a.Find("polyline").Length())
		assert.Equal(t, 1, r.Find("[data-asc-user-changesets]").Length())
		assert.Equal(t, 1, r.Find("[data-asc-user-reviews]").Length())
	})

	t.Run("A person without activity shows an empty line and says so", func(t *testing.T) {
		a := row(t, nil, "/explore/users?q=user8", "user8").Find("[data-asc-activity]")
		require.Equal(t, 1, a.Length())
		assert.Equal(t, "0", a.AttrOr("data-total", ""))
		assert.Equal(t, "No public activity in the last 12 weeks", a.AttrOr("aria-label", ""))
		assert.Zero(t, a.Find("polyline").Length())
	})

	t.Run("A person who keeps their activity private shows none, except to themself and an admin", func(t *testing.T) {
		_, err := e.Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 2", true)
		require.NoError(t, err)
		defer func() {
			_, err := e.Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 2", false)
			require.NoError(t, err)
		}()
		r := row(t, nil, "/explore/users?q=user2", "user2")
		assert.Zero(t, r.Find("[data-asc-activity]").Length())
		assert.Zero(t, r.Find("[data-asc-user-changesets]").Length())
		assert.Zero(t, row(t, loginUser(t, "user5"), "/explore/users?q=user2", "user2").Find("[data-asc-activity]").Length())
		assert.Equal(t, 1, row(t, loginUser(t, "user2"), "/explore/users?q=user2", "user2").Find("[data-asc-activity]").Length())
		assert.Equal(t, 1, row(t, loginUser(t, "user1"), "/explore/users?q=user2", "user2").Find("[data-asc-activity]").Length())
	})

	t.Run("An organization shows the actions in its public repositories and when it was last active", func(t *testing.T) {
		r := row(t, nil, "/explore/organizations?q=org17", "org17")
		a := r.Find("[data-asc-activity]")
		require.Equal(t, 1, a.Length())
		assert.Equal(t, "1", a.AttrOr("data-total", ""))
		active := r.Find("[data-asc-org-active]")
		require.Equal(t, 1, active.Length())
		assert.Contains(t, active.Text(), "Active")
		assert.Equal(t, 1, active.Find("relative-time").Length(), "the time is shown relative to now")
		assert.Zero(t, r.Find("[data-asc-user-changesets]").Length(), "changesets and reviews are for people")
	})

	t.Run("A private organization tells nothing", func(t *testing.T) {
		r := row(t, loginUser(t, "user5"), "/explore/organizations?q=privated_org", "privated_org")
		assert.Equal(t, "0", r.Find("[data-asc-activity]").AttrOr("data-total", ""))
		assert.Zero(t, r.Find("[data-asc-org-active]").Length())
	})
}
