// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func laneCommit(id string, parents ...string) *git.Commit {
	c := &git.Commit{ID: git.MustIDFromString(strings.Repeat(id, 40))}
	for _, p := range parents {
		c.Parents = append(c.Parents, git.MustIDFromString(strings.Repeat(p, 40)))
	}
	return c
}

func laneID(id string) string { return strings.Repeat(id, 40) }

func TestHistoryLanesOfALine(t *testing.T) {
	rows, width := historyLanes([]*git.Commit{laneCommit("c", "b"), laneCommit("b", "a"), laneCommit("a")})
	assert.Equal(t, 2*historyLaneX, width, "one lane")
	for _, id := range []string{"c", "b", "a"} {
		assert.Equal(t, historyLaneX, rows[laneID(id)].NodeX, id)
	}
	assert.Equal(t, []historyLaneLine{{X1: 8, Y1: "25", X2: 8, Y2: "100%"}}, rows[laneID("c")].Lines, "the newest row only goes down")
	assert.Equal(t, []historyLaneLine{{X1: 8, Y1: "0", X2: 8, Y2: "25"}, {X1: 8, Y1: "25", X2: 8, Y2: "100%"}}, rows[laneID("b")].Lines)
	assert.Equal(t, []historyLaneLine{{X1: 8, Y1: "0", X2: 8, Y2: "25"}}, rows[laneID("a")].Lines, "the root only comes in")
}

func TestHistoryLanesOfAMerge(t *testing.T) {
	// d merges c into b; e and b both come from a
	rows, width := historyLanes([]*git.Commit{laneCommit("e", "a"), laneCommit("d", "b", "c"), laneCommit("c", "b"), laneCommit("b", "a"), laneCommit("a")})
	lane := func(n int) int { return historyLaneX + n*historyLaneGap }
	assert.Equal(t, 2*historyLaneX+2*historyLaneGap, width, "three lanes at the widest")
	d := rows[laneID("d")]
	assert.True(t, d.Merge)
	assert.Equal(t, lane(1), d.NodeX, "the merge sits beside e's line")
	assert.Contains(t, d.Lines, historyLaneLine{X1: lane(0), Y1: "0", X2: lane(0), Y2: "100%"}, "e's line passes by")
	assert.Contains(t, d.Lines, historyLaneLine{X1: lane(1), Y1: "25", X2: lane(2), Y2: "100%", Color: 2}, "the merged side opens a lane")
	c := rows[laneID("c")]
	assert.Equal(t, lane(2), c.NodeX)
	assert.Contains(t, c.Lines, historyLaneLine{X1: lane(2), Y1: "25", X2: lane(1), Y2: "100%", Color: 1}, "c goes back to b's lane")
	b := rows[laneID("b")]
	assert.Equal(t, lane(1), b.NodeX)
	assert.Equal(t, 1, b.NodeColor)
	assert.Contains(t, b.Lines, historyLaneLine{X1: lane(1), Y1: "25", X2: lane(0), Y2: "100%"}, "b joins e's line to a")
	a := rows[laneID("a")]
	assert.Equal(t, lane(0), a.NodeX, "the lines come back together at the root")
	assert.False(t, a.Merge)
}

func TestHistoryLanesStayBounded(t *testing.T) {
	// twelve heads, each with a parent off the page: each opens a lane, drawn at most historyMaxLanes apart
	var commits []*git.Commit
	for n := 1; n <= 12; n++ {
		c := &git.Commit{ID: git.MustIDFromString(fmt.Sprintf("%040x", n))}
		c.Parents = []git.ObjectID{git.MustIDFromString(fmt.Sprintf("%040x", 100+n))}
		commits = append(commits, c)
	}
	rows, width := historyLanes(commits)
	assert.Equal(t, 2*historyLaneX+(historyMaxLanes-1)*historyLaneGap, width)
	for _, r := range rows {
		assert.LessOrEqual(t, r.NodeX, historyLaneX+(historyMaxLanes-1)*historyLaneGap)
		assert.Less(t, r.NodeColor, historyMaxLanes)
	}
}

func TestHistoryGraphView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	history := func(query string) (map[string]any, string) {
		ctx, resp := contexttest.MockContext(t, "user2/repo2/commits/branch/master"+query, contexttest.MockContextOption{Render: templates.HTMLRenderer()})
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 2)
		contexttest.LoadGitRepo(t, ctx)
		defer ctx.Repo.GitRepo.Close()
		contexttest.LoadRepoCommit(t, ctx)
		ctx.Data["CommitID"] = ctx.Repo.CommitID
		ctx.Data["BranchName"] = "master"
		ctx.Data["BranchNameSubURL"] = "branch/master"
		ctx.Data["TreeNames"] = []string{}
		ctx.Data["Paths"] = []string{}
		Commits(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		return ctx.Data, resp.Body.String()
	}
	data, html := history("?view=graph")
	assert.Equal(t, "graph", data["HistoryView"])
	lanes := data["HistoryLanes"].(map[string]*historyLaneRow)
	assert.Len(t, lanes, 3, "repo2 has three changes on master")
	assert.Equal(t, 3, strings.Count(html, `class="rp-lanes"`), "every row has its lines")
	assert.Zero(t, strings.Count(html, `class="rp-node`), "the graph replaces the list's nodes")
	assert.Contains(t, html, "rp-hist commit-list is-graph")
	assert.Contains(t, html, `?view=graph" aria-current="true" data-testid="history-view-graph"`)
	assert.Contains(t, html, `data-testid="history-all-branches"`)

	data, html = history("")
	assert.Nil(t, data["HistoryLanes"])
	assert.Equal(t, 3, strings.Count(html, `class="rp-node`))
	assert.NotContains(t, html, "history-all-branches")

	data, _ = history("?view=graph&who=people")
	assert.Nil(t, data["HistoryLanes"], "a filtered page has the List only")
}

func TestHistoryPhoneChips(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	page := func(set func(d map[string]any)) string {
		return renderPart(t, "repo/commits", func(d map[string]any) {
			d["PageIsCommits"] = true
			d["CommitCount"] = 0
			d["BranchName"] = "master"
			d["BranchNameSubURL"] = "branch/master"
			d["IsViewBranch"] = true
			d["CommitID"] = "abc"
			d["TreeNames"] = []string{}
			d["Paths"] = []string{}
			set(d)
		})
	}
	plain := page(func(map[string]any) {})
	assert.Contains(t, plain, `data-testid="history-chips"`)
	for _, chip := range []string{"who", "path", "stack", "date"} {
		assert.Contains(t, plain, `data-history-chip="`+chip+`"`)
		assert.Contains(t, plain, `data-history-part="`+chip+`"`)
	}
	assert.Contains(t, plain, "asc_rp.chip_stack: master")
	assert.Contains(t, plain, `data-ref-sub="branch/master"`)
	assert.Contains(t, plain, "rp-histbar ds-hide-xs", "the chips replace the desktop bar on a phone")
	assert.NotContains(t, plain, "asc_rp.chip_clear")

	dated := page(func(d map[string]any) {
		d["HistoryAfter"] = "2026-01-02"
		d["HistoryBefore"] = "2026-02-01"
		d["HistoryWords"] = "fix <x>"
		d["Keyword"] = "after:2026-01-02 before:2026-02-01 fix <x>"
	})
	assert.Contains(t, dated, "asc_rp.chip_date_range")
	assert.Contains(t, dated, `value="2026-01-02"`)
	assert.Contains(t, dated, `data-words="fix &lt;x&gt;"`)
	assert.Contains(t, dated, "asc_rp.chip_clear")

	path := page(func(d map[string]any) { d["TreePath"] = "docs/a.md" })
	assert.Contains(t, path, `data-tree-path="docs/a.md"`)
	assert.Contains(t, path, "asc_rp.chip_date_whole", "dates do not keep the path, and the sheet says so")
}

func TestHistorySearchWords(t *testing.T) {
	assert.Equal(t, "fix author:ada", historySearchWords("after:2026-01-01  fix before:2026-02-01 author:ada"))
	assert.Empty(t, historySearchWords("after:2026-01-01"))
}
