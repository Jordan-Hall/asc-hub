// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	files_service "forgejo.org/services/repository/files"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commitFile commits one new file on master with the given message.
func commitFile(t *testing.T, user *user_model.User, repo *repo_model.Repository, path, message string) {
	t.Helper()
	_, err := files_service.ChangeRepoFiles(git.DefaultContext, repo, user, &files_service.ChangeRepoFilesOptions{
		Files:     []*files_service.ChangeRepoFile{{Operation: "create", TreePath: path, ContentReader: strings.NewReader(message + "\n")}},
		OldBranch: "master",
		Message:   message,
	})
	require.NoError(t, err)
}

// A release card lists the changes since the release before it: notes grouped Added, Changed, Fixed when the
// release has none, "N changes in this release", a chip per category and a filter.
func TestAscReleaseChangesList(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		session := loginUser(t, "user2")
		createNewRelease(t, session, "/user2/repo1", "v8.0", "v8.0", false, false)
		commitFile(t, user, repo, "retry.txt", "Add retry policy")
		commitFile(t, user, repo, "refund.txt", "Fix refund timeout")
		commitFile(t, user, repo, "timeouts.txt", "Move provider timeouts to config")
		createNewRelease(t, session, "/user2/repo1", "v8.1", "v8.1", false, false)

		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/releases"), http.StatusOK).Body)
		card := doc.Find(`article.rp-rel-card:has(h3 a[href$="/src/tag/v8.1"])`)
		require.Equal(t, 1, card.Length())

		assert.Equal(t, "3 changes", strings.TrimSpace(card.Find(`[data-testid="release-count"]`).Text()))

		notes := card.Find(`[data-testid="release-notes-written"]`)
		require.Equal(t, 1, notes.Length(), "a release without notes shows the notes written from each intent")
		assert.Contains(t, notes.Text(), "Edit before publishing.", "the writer is told to edit them")
		var heads []string
		notes.Find(".rp-rel-group").Each(func(_ int, s *goquery.Selection) { heads = append(heads, s.Text()) })
		assert.Equal(t, []string{"Added", "Changed", "Fixed"}, heads)
		assert.Equal(t, "Add retry policy", notes.Find(".rp-rel-notes-list li").First().Text())

		changes := card.Find(`[data-testid="release-changes"]`)
		assert.Contains(t, changes.Find("summary").Text(), "3 changes in this release")
		chips := changes.Find(".rp-rel-filter button")
		require.Equal(t, 4, chips.Length())
		assert.Equal(t, "All 3", strings.TrimSpace(chips.First().Text()))
		assert.Equal(t, "true", chips.First().AttrOr("aria-pressed", ""))
		assert.Equal(t, "fixed", chips.Last().AttrOr("data-rel-cat", ""))

		rows := changes.Find(".rp-rel-list li")
		require.Equal(t, 3, rows.Length())
		first := rows.First()
		assert.Equal(t, "changed", first.AttrOr("data-rel-cat", ""), "newest first")
		assert.Equal(t, "Changed", first.Find(".rp-rel-cat").Text())
		assert.Equal(t, 1, first.Find("img.ds-av").Length(), "the author's avatar")
		assert.Len(t, strings.TrimSpace(first.Find(".rp-hash").Text()), 10, "not a landed asc change: the short commit id")

	})
}

// The Release health card shows the median gap between releases, the rollbacks among the newest six and a line of
// releases per week; a release cut from an older commit than the release before it is a rollback.
func TestAscReleaseHealth(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		session := loginUser(t, "user2")

		createNewRelease(t, session, "/user2/repo1", "v9.0", "v9.0", false, false)
		commitFile(t, user, repo, "health.txt", "Add health check")
		createNewRelease(t, session, "/user2/repo1", "v9.1", "v9.1", false, false)
		// v9.2 is cut from the commit of v9.0: a rollback
		createNewReleaseTarget(t, session, "/user2/repo1", "v9.2", "v9.2", "v9.0", false, false)

		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/releases"), http.StatusOK).Body)
		card := doc.Find(`[data-testid="release-health"]`)
		require.Equal(t, 1, card.Length())
		assert.Equal(t, "Release health", card.Find("h2").Text())
		assert.Equal(t, "< 1 d", card.Find(`[data-testid="release-cadence"]`).Text(), "the releases were cut seconds apart")
		assert.Equal(t, "1", card.Find(`[data-testid="release-rollbacks"]`).Text())
		assert.Contains(t, card.Text(), "median gap")
		assert.Contains(t, card.Text(), "releases")
		label := card.Find("svg.rp-spark").AttrOr("aria-label", "")
		assert.Contains(t, label, "in the last 12 weeks")
	})
}
