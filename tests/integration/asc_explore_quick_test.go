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
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheQuickChipsOfExplore(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := db.GetEngine(db.DefaultContext)
	// one open pull request, in repo1; an agent (the bot account user30) acted in repo4 an hour ago
	_, err := e.Exec("UPDATE `issue` SET is_closed = ? WHERE is_pull = ?", true, true)
	require.NoError(t, err)
	_, err = e.Exec("UPDATE `issue` SET is_closed = ? WHERE id = 3", false)
	require.NoError(t, err)
	_, err = e.Exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	require.NoError(t, err)
	_, err = e.Exec("INSERT INTO `action` (user_id, op_type, act_user_id, repo_id, is_private, created_unix) VALUES (30, 1, 30, 4, ?, ?)",
		false, time.Now().Add(-time.Hour).Unix())
	require.NoError(t, err)

	pills := func(doc *HTMLDoc) []string {
		return doc.doc.Find(".ds-filterbar-pills a.ds-chip-btn").Map(func(_ int, s *goquery.Selection) string {
			text := strings.TrimSpace(s.Text())
			if s.AttrOr("aria-current", "") == "true" {
				text += " (current)"
			}
			return text
		})
	}

	t.Run("A visitor gets every chip but Starred", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		assert.Equal(t, []string{"All (current)", "Has open stacks", "Agent activity", "Templates", "Archived"}, pills(doc))
		assert.Equal(t, "An agent acted here in the last 7 days.", doc.doc.Find(`.ds-filterbar-pills a[href$="&agents=1"]`).AttrOr("title", ""))
	})

	t.Run("A signed-in person also gets Starred", func(t *testing.T) {
		sess := loginUser(t, "user2")
		doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", "/explore/repos?starred=1"), http.StatusOK).Body)
		assert.Equal(t, []string{"All", "Has open stacks", "Agent activity", "Starred (current)", "Templates", "Archived"}, pills(doc))
		assert.Equal(t, []string{"user2/repo2", "user5/repo4"}, repoRows(doc))
		assert.Equal(t, "1", doc.doc.Find(`#repo-search-form input[name="starred"]`).AttrOr("value", ""), "a search keeps the chip")
		assert.Equal(t, 1, doc.doc.Find(`#repo-filter-sheet input[name="starred"][checked]`).Length())
	})

	t.Run("Has open stacks and Agent activity filter the list", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?open_stacks=1"), http.StatusOK).Body)
		assert.Equal(t, []string{"user2/repo1"}, repoRows(doc))
		assert.Contains(t, pills(doc), "Has open stacks (current)")
		doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?agents=1"), http.StatusOK).Body)
		assert.Equal(t, []string{"user5/repo4"}, repoRows(doc))
	})

	t.Run("No result names the chip with a way to remove it", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?open_stacks=1&agents=1"), http.StatusOK).Body)
		empty := doc.doc.Find("[data-asc-explore-nomatch]")
		require.Equal(t, 1, empty.Length())
		assert.Equal(t, "Nothing matches these filters", empty.Find("h2").Text())
		chip := empty.Find(".ds-active-filters a.ds-chip-btn")
		require.Equal(t, 1, chip.Length())
		assert.Contains(t, chip.Text(), "Has open stacks")
		assert.Equal(t, "?q=&sort=recentupdate&language=", chip.AttrOr("href", ""))
	})

	t.Run("The organization and profile lists take the chips too", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2?tab=repositories&open_stacks=1"), http.StatusOK).Body)
		assert.Equal(t, []string{"user2/repo1"}, repoRows(doc))
		doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/org3?open_stacks=1"), http.StatusOK).Body)
		assert.Empty(t, repoRows(doc))
	})
}

// repoRows is the full names of the rows of a repository list, in order.
func repoRows(doc *HTMLDoc) []string {
	return doc.doc.Find(".ds-repo-row .ds-li-name > a:last-of-type").Map(func(_ int, s *goquery.Selection) string {
		return strings.TrimPrefix(s.AttrOr("href", ""), "/")
	})
}
