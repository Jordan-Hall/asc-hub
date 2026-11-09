// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"crypto/sha1"
	"fmt"
	"net/http"
	"testing"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows of an organization page say how the checks of the default branch went and let a member watch the
// repository from the row.
func TestTheOrgRowsShowChecksAndAWatchToggle(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	ctx := t.Context()
	// repo3 of org3: its master head has two checks, one of them failing; repo5 has a head without checks
	head := "65f1bf27bc3bf70f64657658635e66094edbcb4d"
	require.NoError(t, db.Insert(ctx, &git_model.Branch{RepoID: 3, Name: "master", CommitID: head}))
	require.NoError(t, db.Insert(ctx, &git_model.Branch{RepoID: 5, Name: "master", CommitID: head}))
	status := func(index int64, check string, state api.CommitStatusState) *git_model.CommitStatus {
		return &git_model.CommitStatus{
			Index: index, RepoID: 3, SHA: head, State: state, Context: check,
			ContextHash: fmt.Sprintf("%x", sha1.Sum([]byte(check))),
		}
	}
	require.NoError(t, db.Insert(ctx, status(1, "ci/build", api.CommitStatusPending)))
	require.NoError(t, db.Insert(ctx, status(2, "ci/build", api.CommitStatusSuccess)))
	require.NoError(t, db.Insert(ctx, status(3, "ci/test", api.CommitStatusFailure)))

	page := func(t *testing.T, s *TestSession) *HTMLDoc {
		t.Helper()
		return NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/org3"), http.StatusOK).Body)
	}
	owner := loginUser(t, "user2")

	t.Run("A reader of the code sees the checks", func(t *testing.T) {
		doc := page(t, owner)
		check := doc.Find(`.ds-repo-row:has(a[href="/org3/repo3"]) .ds-repo-check`)
		require.Equal(t, 1, check.Length())
		assert.Equal(t, "failing", check.AttrOr("data-check", ""))
		assert.Equal(t, "1 check failing", check.Text())
		assert.Equal(t, "/org3/repo3/commits/branch/master", check.AttrOr("href", ""))

		none := doc.Find(`.ds-repo-row:has(a[href="/org3/repo5"]) .ds-repo-check`)
		require.Equal(t, 1, none.Length())
		assert.Equal(t, "none", none.AttrOr("data-check", ""))
		assert.Equal(t, "No checks", none.Text())
	})

	t.Run("The latest status of each check counts", func(t *testing.T) {
		require.NoError(t, db.Insert(ctx, status(4, "ci/test", api.CommitStatusSuccess)))
		check := page(t, owner).Find(`.ds-repo-row:has(a[href="/org3/repo3"]) .ds-repo-check`)
		assert.Equal(t, "passing", check.AttrOr("data-check", ""))
		assert.Equal(t, "Checks passing", check.Text())
	})

	t.Run("A member who may not read the code sees no checks", func(t *testing.T) {
		// user15 is in test_team, which has the issues of its repositories and not the code
		require.NoError(t, db.Insert(ctx, &organization.TeamRepo{OrgID: 3, TeamID: 7, RepoID: 3}))
		doc := page(t, loginUser(t, "user15"))
		require.Equal(t, 1, doc.Find(`.ds-repo-row a[href="/org3/repo3"]`).Length())
		assert.Equal(t, 0, doc.Find(`.ds-repo-row:has(a[href="/org3/repo3"]) .ds-repo-check`).Length())
	})

	t.Run("The Watch toggle watches the repository and comes back to the page", func(t *testing.T) {
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 5})
		before := repo.NumWatches
		watching := repo_model.IsWatcher(ctx, 2, 5)
		doc := page(t, owner)
		form := doc.Find(`.ds-repo-row:has(a[href="/org3/repo5"]) form[data-testid="row-watch"]`)
		require.Equal(t, 1, form.Length())
		btn := form.Find("button")
		assert.Equal(t, fmt.Sprint(watching), btn.AttrOr("aria-pressed", ""))
		assert.Equal(t, fmt.Sprint(before), btn.Find(`[data-watch-count]`).Text())
		assert.Equal(t, "/org3", form.Find(`input[name="redirect_to"]`).AttrOr("value", ""))

		action := form.AttrOr("action", "")
		resp := owner.MakeRequest(t, NewRequestWithValues(t, "POST", action, map[string]string{"redirect_to": "/org3"}), http.StatusSeeOther)
		assert.Equal(t, "/org3", test.RedirectURL(resp))
		assert.Equal(t, !watching, repo_model.IsWatcher(ctx, 2, 5))
		after := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 5}).NumWatches
		if watching {
			assert.Equal(t, before-1, after)
		} else {
			assert.Equal(t, before+1, after)
		}
		btn = page(t, owner).Find(`.ds-repo-row:has(a[href="/org3/repo5"]) form[data-testid="row-watch"] button`)
		assert.Equal(t, fmt.Sprint(!watching), btn.AttrOr("aria-pressed", ""))
		assert.Equal(t, fmt.Sprint(after), btn.Find(`[data-watch-count]`).Text())
	})

	t.Run("An address outside the hub is not followed back", func(t *testing.T) {
		resp := owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/org3/repo5/action/watch", map[string]string{"redirect_to": "https://example.org/"}), http.StatusSeeOther)
		assert.Equal(t, "/org3/repo5", test.RedirectURL(resp))
	})

	t.Run("The header control still swaps in place", func(t *testing.T) {
		req := NewRequest(t, "POST", "/org3/repo5/action/unwatch")
		req.Header.Set("HX-Request", "true")
		owner.MakeRequest(t, req, http.StatusOK)
	})

	t.Run("A visitor has no toggle", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/org3"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(`form[data-testid="row-watch"]`).Length())
	})
}
