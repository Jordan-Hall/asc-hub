// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThePeopleDirectoryFilters(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := db.GetEngine(db.DefaultContext)
	_, err := e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	require.NoError(t, err)

	names := func(doc *HTMLDoc) []string {
		return doc.doc.Find(".ds-user-row .ds-li-title > a").Map(func(_ int, s *goquery.Selection) string {
			return strings.TrimSpace(s.Text())
		})
	}
	get := func(t *testing.T, sess *TestSession, url string) *HTMLDoc {
		t.Helper()
		if sess == nil {
			return NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
		}
		return NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
	}

	t.Run("Show offers people and agents, people, agents", func(t *testing.T) {
		doc := get(t, nil, "/explore/users?q=user30")
		assert.Equal(t, []string{"all", "people", "agents"}, doc.doc.Find(`[data-testid="explore-users-show"] option`).Map(func(_ int, s *goquery.Selection) string { return s.AttrOr("value", "") }))
		assert.Equal(t, "all", doc.doc.Find(`[data-testid="explore-users-show"] option[selected]`).AttrOr("value", ""))
		assert.Equal(t, []string{"user30"}, names(doc), "agents are in the list by default")
		assert.Equal(t, 1, doc.doc.Find(`[data-testid="user-agent-chip"]`).Length())

		assert.Empty(t, names(get(t, nil, "/explore/users?q=user30&show=people")))
		doc = get(t, nil, "/explore/users?show=agents")
		assert.Equal(t, []string{"user30"}, names(doc))
		assert.Equal(t, "agents", doc.doc.Find(`[data-testid="explore-users-show"] option[selected]`).AttrOr("value", ""))
	})

	t.Run("Organization keeps its members the viewer may see", func(t *testing.T) {
		doc := get(t, nil, "/explore/users?org=org3&sort=alphabetically")
		assert.Equal(t, []string{"user2", "user28"}, names(doc), "a visitor sees the public members")
		assert.Equal(t, "org3", doc.doc.Find(`[data-testid="explore-users-org"]`).AttrOr("value", ""))
		assert.Positive(t, doc.doc.Find(`#explore-users-orgs option[value="org3"]`).Length())

		doc = get(t, loginUser(t, "user2"), "/explore/users?org=org3&sort=alphabetically")
		assert.Equal(t, []string{"user2", "user28", "user4"}, names(doc), "a member sees every member")
	})

	t.Run("An organization the viewer may not see finds no one", func(t *testing.T) {
		for _, org := range []string{"privated_org", "no-such-org"} {
			doc := get(t, nil, "/explore/users?org="+org)
			assert.Empty(t, names(doc), org)
			assert.Equal(t, 1, doc.doc.Find("[data-asc-users-filtered-none]").Length(), org)
			assert.Equal(t, 0, doc.doc.Find(`#explore-users-orgs option[value="privated_org"]`).Length())
		}
	})

	t.Run("Most active puts the most active first", func(t *testing.T) {
		_, err := e.Exec("DELETE FROM `action`")
		require.NoError(t, err)
		at := time.Now().Add(-time.Hour).Unix()
		for _, actor := range []int64{28, 28, 4} {
			_, err = e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (?, 5, ?, 1, ?, ?)", actor, actor, false, at)
			require.NoError(t, err)
		}
		doc := get(t, loginUser(t, "user2"), "/explore/users?org=org3&sort=mostactive")
		assert.Equal(t, []string{"user28", "user4", "user2"}, names(doc))
		assert.Equal(t, "mostactive", doc.doc.Find(`select[name="sort"] option[selected]`).AttrOr("value", ""))
	})

	t.Run("The pager keeps the filters", func(t *testing.T) {
		defer test.MockVariableValue(&setting.UI.ExplorePagingNum, 1)()
		doc := get(t, nil, "/explore/users?show=people&org=org3&sort=alphabetically")
		assert.Equal(t, []string{"user2"}, names(doc))
		next := doc.doc.Find(`a[href*="page=2"]`).First().AttrOr("href", "")
		assert.Contains(t, next, "show=people")
		assert.Contains(t, next, "org=org3")
	})
}
