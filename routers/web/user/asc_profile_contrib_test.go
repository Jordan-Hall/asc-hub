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
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/translation"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscHeatLayout(t *testing.T) {
	loc := time.UTC
	// Wednesday 1 January 2025 to Friday 10 January 2025
	first := time.Date(2025, 1, 1, 0, 0, 0, 0, loc)
	last := time.Date(2025, 1, 10, 0, 0, 0, 0, loc)
	perDay := map[string]int64{"2025-01-01": 2, "2025-01-03": 1, "2025-01-10": 4}
	agents := map[string]int64{"2025-01-03": 3, "2025-01-10": 1}
	h := ascHeatLayout(perDay, agents, first, last, translation.NewLocale("en-US"), func(day string) string { return "/u?date=" + day })

	assert.EqualValues(t, 11, h.Total)
	assert.EqualValues(t, 4, h.Agents)
	assert.Equal(t, 3, h.Days)
	require.Len(t, h.Weeks, 2, "Sunday 29 December to Saturday 11 January")
	assert.True(t, h.Weeks[0][0].Blank, "29 December is before the range")
	assert.True(t, h.Weeks[1][6].Blank, "11 January is after the range")

	wed := h.Weeks[0][3]
	assert.Equal(t, "/u?date=2025-01-01", wed.Link)
	assert.False(t, wed.Agent)
	fri := h.Weeks[0][5]
	assert.True(t, fri.Agent, "three of four changes of 3 January came from agent runs")
	assert.Equal(t, ascHeatLevels, fri.Level)
	assert.Contains(t, fri.Label, "by_agents") // the test locale shows keys and arguments
	assert.Contains(t, fri.Label, "int64=3")
	assert.False(t, h.Weeks[1][5].Agent, "one of five is not most of the day")
	assert.Empty(t, h.Weeks[0][4].Link, "a day without activity opens nothing")

	require.Len(t, h.Rows, 3)
	assert.Equal(t, "Fri, 10 Jan 2025", h.Rows[0].Date, "newest first")
	assert.EqualValues(t, 5, h.Rows[0].Count)
	assert.EqualValues(t, 1, h.Rows[0].Agents)
	assert.Equal(t, "/u?date=2025-01-10", h.Rows[0].Link)
	assert.Equal(t, "Wed, 1 Jan 2025", h.Rows[2].Date)
}

func TestAscContribYears(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, loc)
	person := &user_model.User{CreatedUnix: timeutil.TimeStamp(time.Date(2023, 6, 1, 0, 0, 0, 0, loc).Unix())}
	assert.Equal(t, []int{2026, 2025, 2024, 2023}, ascContribYearList(person, now, loc))
	old := &user_model.User{CreatedUnix: timeutil.TimeStamp(time.Date(2001, 1, 1, 0, 0, 0, 0, loc).Unix())}
	assert.Len(t, ascContribYearList(old, now, loc), ascContribYears)

	assert.Equal(t, 2024, ascContribYear("2024", person, now, loc))
	for _, raw := range []string{"", "x", "2022", "2027", "0"} {
		assert.Equal(t, 0, ascContribYear(raw, person, now, loc), raw)
	}

	first, last := ascContribWindow(2025, now, loc)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, loc), first)
	assert.Equal(t, time.Date(2025, 12, 31, 0, 0, 0, 0, loc), last)
	first, last = ascContribWindow(2026, now, loc)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, loc), first)
	assert.Equal(t, time.Date(2026, 3, 4, 0, 0, 0, 0, loc), last, "this year runs to today")
	first, last = ascContribWindow(0, now, loc)
	assert.Equal(t, time.Sunday, first.Weekday())
	assert.Equal(t, ascHeatWeeks*7-3, int(last.Sub(first).Hours()/24)+1, "53 weeks to the Wednesday of today")
}

func TestAscTurnaround(t *testing.T) {
	l := translation.NewLocale("en-US")
	assert.Empty(t, ascTurnaround(nil, l))
	// the test locale shows keys and arguments
	assert.Equal(t, "user.asc_profile.turn_minutes%!(EXTRA int64=1)", ascTurnaround([]int64{5}, l))
	assert.Equal(t, "user.asc_profile.turn_hours%!(EXTRA int64=3)", ascTurnaround([]int64{2 * 3600, 4 * 3600}, l), "the median of an even count is the middle pair's mean")
	assert.Equal(t, "user.asc_profile.turn_days%!(EXTRA int64=2)", ascTurnaround([]int64{3600, 2 * 86400, 9 * 86400}, l))
}

func TestAscContribCounts(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	// a session forgets NoAutoTime after each statement
	e := func() db.Engine { return db.GetEngine(db.DefaultContext).NoAutoTime() }
	t0 := timeutil.TimeStamp(time.Date(2030, 5, 6, 9, 0, 0, 0, time.UTC).Unix())
	pull := func(id, repo, poster int64, created timeutil.TimeStamp) {
		_, err := e().Insert(&issues_model.Issue{ID: id, RepoID: repo, Index: id, PosterID: poster, Title: "p", IsPull: true, CreatedUnix: created, UpdatedUnix: created})
		require.NoError(t, err)
		_, err = e().Insert(&issues_model.PullRequest{ID: id, IssueID: id, BaseRepoID: repo, HeadRepoID: repo})
		require.NoError(t, err)
	}
	review := func(issue, reviewer int64, kind issues_model.ReviewType, created timeutil.TimeStamp) {
		_, err := e().Insert(&issues_model.Review{IssueID: issue, ReviewerID: reviewer, Type: kind, CreatedUnix: created, UpdatedUnix: created})
		require.NoError(t, err)
	}
	// repo1 is public, repo2 is private to user2
	pull(9001, 1, 2, t0)
	review(9001, 5, issues_model.ReviewTypeApprove, t0+2*3600)
	review(9001, 5, issues_model.ReviewTypeComment, t0+5*3600)
	pull(9002, 1, 2, t0)
	review(9002, 5, issues_model.ReviewTypeReject, t0+4*3600)
	review(9002, 5, issues_model.ReviewTypePending, t0+3600)
	pull(9003, 1, 5, t0+86400)
	pull(9004, 1, 5, t0+86400)
	_, err := e().Insert(&issues_model.PullRequestAscRun{PullID: 9004, RepoID: 1, RunID: 7, Agent: "fixer", StartedBy: "User5"})
	require.NoError(t, err)
	pull(9005, 1, 2, t0+2*86400)
	_, err = e().Insert(&issues_model.PullRequestAscRun{PullID: 9005, RepoID: 1, RunID: 8, Agent: "fixer", StartedBy: "user5"})
	require.NoError(t, err)
	pull(9006, 2, 5, t0)
	review(9006, 5, issues_model.ReviewTypeComment, t0+60)

	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	from, until := t0-86400, t0+7*86400
	ctx, _ := contexttest.MockContext(t, "/user5")

	c, err := ascContribCounts(ctx, person, nil, from, until, time.UTC)
	require.NoError(t, err)
	assert.EqualValues(t, 1, c.changes, "9003; 9004 is an agent run and 9006 is private")
	assert.EqualValues(t, 2, c.agentChanges)
	assert.EqualValues(t, map[string]int64{"2030-05-07": 1, "2030-05-08": 1}, c.agents)
	assert.EqualValues(t, map[string]int64{"2030-05-07": 1}, c.agentOwn, "only 9004 was posted by the person")
	assert.EqualValues(t, 3, c.reviews, "the pending review does not count")
	assert.ElementsMatch(t, []int64{2 * 3600, 4 * 3600}, c.turnaround, "the first review of each pull request")

	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	c, err = ascContribCounts(ctx, person, owner, from, until, time.UTC)
	require.NoError(t, err)
	assert.EqualValues(t, 2, c.changes, "the owner of repo2 reads 9006")
	assert.EqualValues(t, 4, c.reviews)

	c, err = ascContribCounts(ctx, person, nil, until, until+86400, time.UTC)
	require.NoError(t, err)
	assert.Zero(t, c.changes+c.agentChanges+c.reviews)
}
