// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascagentui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscDashGreeting(t *testing.T) {
	assert.Equal(t, "user.asc_dash.good_morning", ascGreetingKey(7))
	assert.Equal(t, "user.asc_dash.good_afternoon", ascGreetingKey(12))
	assert.Equal(t, "user.asc_dash.good_afternoon", ascGreetingKey(17))
	assert.Equal(t, "user.asc_dash.good_evening", ascGreetingKey(18))
	assert.Equal(t, "Jordan", ascGreetName(&user_model.User{Name: "jordan", FullName: "Jordan Upton"}))
	assert.Equal(t, "jordan", ascGreetName(&user_model.User{Name: "jordan"}))
}

func TestAscDashSummary(t *testing.T) {
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	assert.Equal(t, "Nothing needs you right now. Nothing is blocked.", ascDashSummary(l, 0, 0))
	assert.Equal(t, "1 thing needs you. Nothing is blocked.", ascDashSummary(l, 1, 0))
	assert.Equal(t, "7 things need you. 2 blocked.", ascDashSummary(l, 7, 2))
}

func TestAscDashRows(t *testing.T) {
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	now := time.Now()

	ready := ascDashRowFor(l, &AscInboxEntry{Title: "t", RepoFullName: "o/r", Index: 4, Section: issues_model.AscInboxReady, Updated: now},
		&AscInboxInfo{Approvals: 2, ChecksPass: 9, HasDiff: true, Files: 1, Additions: 18, Deletions: 2})
	assert.Equal(t, "green", ready.Tone)
	assert.Equal(t, "Merge", ready.Action)
	assert.Equal(t, "o/r #4", ready.Where)
	assert.Equal(t, "+18 −2 in 1 file", ready.Meta)
	var texts []string
	for _, c := range ready.Chips {
		texts = append(texts, c.Text)
	}
	assert.Equal(t, []string{"9 checks pass", "2 approvals"}, texts)

	blocked := ascDashRowFor(l, &AscInboxEntry{Title: "t", RepoFullName: "o/r", Index: 4, Section: issues_model.AscInboxBlocked, Conflicted: true},
		&AscInboxInfo{ChecksFail: 1, StackIndex: 3, StackTotal: 4})
	assert.Equal(t, "red", blocked.Tone)
	assert.Empty(t, blocked.Action)
	assert.Equal(t, "o/r · stack 3 of 4", blocked.Where)
	assert.Equal(t, "Conflicts with its base", blocked.Chips[0].Text)
	assert.Equal(t, "1 check failing", blocked.Chips[1].Text)

	review := ascDashRowFor(l, &AscInboxEntry{Section: issues_model.AscInboxNeedsReview}, nil)
	assert.Equal(t, "amber", review.Tone)
	assert.Equal(t, "Review requested", review.Chips[0].Text)
	assert.Empty(t, review.Meta)

	assert.False(t, ascDashActionable(issues_model.AscInboxWaiting))
	assert.False(t, ascDashActionable(issues_model.AscInboxMerged))
	assert.True(t, ascDashActionable(issues_model.AscInboxBlocked))
}

func TestAscDashStacks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ascRenderSeed(t)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")

	stacks, count := ascDashStacks(t.Context(), user2, l)
	require.Equal(t, 1, count)
	require.Len(t, stacks, 1)
	assert.Equal(t, "Add retry policy type", stacks[0].Name)
	require.Len(t, stacks[0].Segs, 3)
	assert.Equal(t, "green", stacks[0].Segs[0].Tone, "the merged change")
	assert.Contains(t, stacks[0].Segs[0].Label, "Merged")
	assert.Contains(t, stacks[0].Link, "/pulls/")

	// a person who wrote none of the pull requests has no stack
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	stacks, count = ascDashStacks(t.Context(), user4, l)
	assert.Empty(t, stacks)
	assert.Zero(t, count)

	// a pull request of the stack the person cannot read leaves the stack out (fail closed)
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET is_private = ? WHERE id = 1", true)
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Exec("DELETE FROM collaboration WHERE repo_id = 1")
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Exec("UPDATE repository SET owner_id = 5 WHERE id = 1")
	require.NoError(t, err)
	stacks, count = ascDashStacks(t.Context(), user2, l)
	assert.Empty(t, stacks)
	assert.Zero(t, count)
}

func TestAscDashPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascRenderSeed(t)
	ascRenderEntries(t)

	ctx, body := ascRenderContext(t, "/", 2)
	Dashboard(ctx)
	require.Equal(t, 200, ctx.Resp.Status())
	html := string(body())
	assert.Contains(t, html, `class="ds-main wk-page wk-dash"`)
	assert.Contains(t, html, "Wire retry policy into the client")
	assert.Contains(t, html, "Ready to merge:")
	assert.Contains(t, html, "Agents working now")
	assert.Contains(t, html, "Paused: needs your answer")
	assert.Contains(t, html, "Your stacks")
	assert.NotContains(t, html, "Forgejo is", "no stock copy")
	// only the actionable sections are rows: the waiting and the merged entries are not
	assert.NotContains(t, html, "Docs and metrics</div>")
	assert.NotContains(t, html, "Add jitter to the retry delay")

	t.Run("agents are hidden while the person's switch is off", func(t *testing.T) {
		user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, false))
		ctx, body := ascRenderContext(t, "/", 2)
		Dashboard(ctx)
		html := string(body())
		assert.NotContains(t, html, "Agents working now")
		assert.NotContains(t, html, "New agent task")
		assert.NotContains(t, html, "Agent runs")
	})

}
