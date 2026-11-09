// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheRepositoryListsShowWhoContributes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := db.GetEngine(db.DefaultContext)
	_, err := e.Exec("DELETE FROM `action`")
	require.NoError(t, err)
	_, err = e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	require.NoError(t, err)
	// in user2/repo1: user2 twice, the agent user30 once and user31 (a private profile) once
	at := time.Now().Add(-time.Hour).Unix()
	for _, actor := range []int64{2, 2, 30, 31} {
		_, err = e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (?, 5, ?, 1, ?, ?)",
			actor, actor, false, at)
		require.NoError(t, err)
	}

	stack := func(t *testing.T, doc *HTMLDoc, repo string) *goquery.Selection {
		t.Helper()
		row := doc.doc.Find(".ds-repo-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return s.Find(`.ds-li-name a[href="/`+repo+`"]`).Length() > 0
		})
		require.Equal(t, 1, row.Length(), "the row of %s", repo)
		return row.Find(`[data-testid="repo-contributors"]`)
	}
	who := func(s *goquery.Selection) []string {
		return s.Find("a.ds-av").Map(func(_ int, a *goquery.Selection) string { return a.AttrOr("aria-label", "") })
	}

	t.Run("A visitor sees the people and agents they may see, most active first", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		s := stack(t, doc, "user2/repo1")
		assert.Equal(t, "2 contributors in the last 90 days", s.AttrOr("aria-label", ""))
		assert.Equal(t, []string{"user2", "user30 (agent)"}, who(s))
		assert.Equal(t, 1, s.Find("a.ds-av-agent").Length())
		assert.Equal(t, "/user30", s.Find("a.ds-av-agent").AttrOr("href", ""))
		assert.Equal(t, 0, stack(t, doc, "user5/repo4").Length(), "no actions, no stack")
	})

	t.Run("An admin also sees the private profile", func(t *testing.T) {
		doc := NewHTMLParser(t, loginUser(t, "user1").MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		s := stack(t, doc, "user2/repo1")
		assert.Equal(t, "3 contributors in the last 90 days", s.AttrOr("aria-label", ""))
		assert.Len(t, who(s), 3)
	})

	t.Run("The profile list has the stack too", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2?tab=repositories"), http.StatusOK).Body)
		assert.Equal(t, []string{"user2", "user30 (agent)"}, who(stack(t, doc, "user2/repo1")))
	})

	t.Run("A person who keeps their activity private is left out, but sees themselves", func(t *testing.T) {
		_, err := e.Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 2", true)
		require.NoError(t, err)
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		s := stack(t, doc, "user2/repo1")
		assert.Equal(t, "1 contributor in the last 90 days", s.AttrOr("aria-label", ""))
		assert.Equal(t, []string{"user30 (agent)"}, who(s))
		doc = NewHTMLParser(t, loginUser(t, "user2").MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		assert.Equal(t, []string{"user2", "user30 (agent)"}, who(stack(t, doc, "user2/repo1")))
	})
}
