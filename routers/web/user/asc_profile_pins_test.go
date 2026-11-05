// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscPinRows(t *testing.T) {
	repos := []*repo_model.Repository{
		{ID: 1, OwnerName: "o", Name: "a"},
		{ID: 2, OwnerName: "o", Name: "b"},
		{ID: 3, OwnerName: "o", Name: "c"},
	}
	// the pinned ones first in the order of the profile, a pin that is no candidate any more is dropped
	rows := ascPinRows(repos, []int64{3, 99, 1})
	assert.Equal(t, []AscPinRow{
		{ID: 3, FullName: "o/c", Pinned: true},
		{ID: 1, FullName: "o/a", Pinned: true},
		{ID: 2, FullName: "o/b"},
	}, rows)
	assert.Len(t, ascPinRows(repos, nil), 3)
}

// postPins sends the Customize form of user 2 as the script of the profile does.
func postPins(t *testing.T, inline bool, ids ...string) int {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "POST /user/settings/pins")
	contexttest.LoadUser(t, ctx, 2)
	form := url.Values{"repo": ids}
	if inline {
		form.Set("inline", "1")
	}
	ctx.Req.PostForm = form
	ctx.Req.Form = form
	AscPinSettingsPost(ctx)
	return ctx.Resp.Status()
}

func TestAscPinSettingsPostInline(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	// the order posted is the order kept, and the answer has no redirect
	assert.Equal(t, http.StatusNoContent, postPins(t, true, "16", "2"))
	ids, err := user_model.ListAscProfilePins(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{16, 2}, ids)

	// more than six, or a repository the person cannot pin, is refused and changes nothing
	assert.Equal(t, http.StatusBadRequest, postPins(t, true, "1", "2", "15", "16", "31", "33", "36"))
	assert.Equal(t, http.StatusBadRequest, postPins(t, true, "99999"))
	ids, _ = user_model.ListAscProfilePins(ctx, 2)
	assert.Equal(t, []int64{16, 2}, ids)

	// the settings form without script still redirects to the profile
	assert.Equal(t, http.StatusSeeOther, postPins(t, false, "1"))
	ids, _ = user_model.ListAscProfilePins(ctx, 2)
	assert.Equal(t, []int64{1}, ids)
}

func TestAscPinSettingsPageListsThePinsFirst(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NoError(t, user_model.SetAscProfilePins(t.Context(), 2, []int64{16, 2}))

	ctx, body := ascRenderContext(t, "/user/settings/pins", 2)
	AscPinSettings(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())
	html := string(body())
	assert.Contains(t, html, `data-asc-pins-form`)
	assert.NotContains(t, html, `data-asc-pins-inline`, "the settings page posts the form itself")
	at := func(id string) int { return strings.Index(html, `data-id="`+id+`"`) }
	require.Positive(t, at("16"))
	assert.Less(t, at("16"), at("2"))
	assert.Less(t, at("2"), at("1"), "the pinned ones come before the others")
	assert.Contains(t, html, `id="pin-16" type="checkbox" name="repo" value="16" checked`)
	assert.Contains(t, html, `data-asc-pin-up`)
	assert.Contains(t, html, `data-asc-pin-down`)
	assert.Contains(t, html, `role="status" aria-live="polite" data-asc-pins-status`)
}

func TestAscProfilePinFormIsForTheOwnerOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	for doer, want := range map[int64]bool{2: true, 5: false, 0: false} {
		ctx, _ := contexttest.MockContext(t, "GET /user2")
		if doer != 0 {
			contexttest.LoadUser(t, ctx, doer)
		}
		ctx.ContextUser = person
		ascProfilePinForm(ctx)
		_, ok := ctx.Data["PinRows"]
		assert.Equal(t, want, ok, "doer %d", doer)
	}
}

func TestAscProfilePinsCountStacksAndChanges(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	require.NoError(t, user_model.SetAscProfilePins(ctx, 2, []int64{1}))
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	pins := ascProfilePins(ctx, person, person)
	require.Len(t, pins, 1)
	assert.Equal(t, int64(1), pins[0].ID)
	assert.Equal(t, repo.NumOpenAscStacks(ctx), pins[0].Stacks)
	assert.Positive(t, pins[0].Changes)
}

func TestAscDashReposPutThePinsFirst(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	latest := ascDashRepos(ctx, doer)
	require.Len(t, latest, ascDashMaxRepos)

	// a repository that is gone is left out; the pinned one leads and does not show twice
	require.NoError(t, user_model.SetAscProfilePins(ctx, 2, []int64{99999, 16}))
	repos := ascDashRepos(ctx, doer)
	require.Len(t, repos, ascDashMaxRepos)
	assert.Equal(t, AscDashRepo{FullName: "user2/repo16", Link: "/user2/repo16", Pinned: true}, repos[0])
	seen := map[string]bool{}
	for _, r := range repos {
		assert.False(t, seen[r.FullName], "%s shows once", r.FullName)
		seen[r.FullName] = true
	}
	for _, r := range repos[1:] {
		assert.False(t, r.Pinned)
	}
}

func TestAscDashCountsServeTheWorkSwitcher(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascRenderSeed(t)
	ascRenderEntries(t)

	ctx, body := ascRenderContext(t, "/-/asc/dash/counts", 2)
	AscDashCounts(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())
	assert.Equal(t, "private, no-store", ctx.Resp.Header().Get("Cache-Control"))
	var counts map[string]int
	require.NoError(t, json.Unmarshal(body(), &counts))
	assert.ElementsMatch(t, []string{"needs", "stacks", "reviews", "agents"}, keysOf(counts))
	assert.Positive(t, counts["needs"])

	ctx, page := ascRenderContext(t, "/", 2)
	Dashboard(ctx)
	html := string(page())
	assert.Contains(t, html, `data-asc-dash-counts="/-/asc/dash/counts"`)
	for _, k := range []string{"needs", "stacks", "reviews"} {
		assert.Contains(t, html, `data-asc-live-count="`+k+`"`)
	}
}

func keysOf(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
