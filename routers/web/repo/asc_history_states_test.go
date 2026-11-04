// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryPartialWhenTheFilterStops(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	history := func(query string) map[string]any {
		ctx, _ := contexttest.MockContext(t, "user2/repo2/commits/branch/master"+query)
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 2)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		contexttest.LoadRepoCommit(t, ctx)
		Commits(ctx)
		return ctx.Data
	}
	// repo2 has three changes on master
	assert.Nil(t, history("?who=people")["HistoryPartial"], "every change was looked at")
	defer test.MockVariableValue(&historyFilterMax, 2)()
	assert.Equal(t, 2, history("?who=people")["HistoryPartial"])
	assert.Nil(t, history("")["HistoryPartial"], "no filter: nothing is left out")
}

func TestHistoryStates(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	page := func(set func(d map[string]any)) string {
		return renderPart(t, "repo/commits_table", func(d map[string]any) {
			d["PageIsCommits"] = true
			d["CommitCount"] = 0
			d["BranchNameSubURL"] = "branch/master"
			set(d)
		})
	}
	agents := page(func(d map[string]any) { d["HistoryWho"] = "agents" })
	assert.Contains(t, agents, "asc_rp.no_commits_who_agents", "the filter is the reason")
	assert.Contains(t, agents, `href="/user2/repo1/commits/branch/master">asc_rp.clear_filters`)
	assert.NotContains(t, agents, "history-partial")

	partial := page(func(d map[string]any) {
		d["HistoryWho"] = "people"
		d["HistoryPartial"] = 500
	})
	assert.Contains(t, partial, `data-testid="history-partial"`)
	assert.Contains(t, partial, "asc_rp.history_partial_all")

	assert.Contains(t, agents, `data-loading-skel data-testid="history-loading" hidden`)
	assert.Equal(t, 8, strings.Count(agents, "rp-histload-row"))
	assert.Contains(t, agents, "data-loading-body")
	assert.Contains(t, agents, "data-loading-form")

	other := renderPart(t, "repo/commits_table", func(d map[string]any) {
		d["PageIsPullCommits"] = true
		d["CommitCount"] = 0
	})
	assert.NotContains(t, other, "history-loading", "a pull request's commits have their own page")
}
