// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http/httptest"
	"strings"
	"testing"

	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
)

func testChange(id, parent int64, branch string, poster *user_model.User, mut func(*stackChange)) *stackChange {
	c := &stackChange{
		pr:     &issues_model.PullRequest{ID: id, HeadBranch: branch},
		parent: parent,
	}
	c.pr.Issue = &issues_model.Issue{Index: id, Title: "title " + branch, PosterID: poster.ID, Poster: poster, UpdatedUnix: 100}
	if poster.Type == user_model.UserTypeBot {
		c.agent = true
	}
	if mut != nil {
		mut(c)
	}
	return c
}

func TestStackStatusOf(t *testing.T) {
	cases := []struct {
		name string
		in   []*stackChange
		want string
	}{
		{"needs me first", []*stackChange{{needsMe: true}, {failing: true}}, stackNeedsReview},
		{"a draft is nobody's turn", []*stackChange{{needsMe: true, draft: true}}, stackDraft},
		{"failing", []*stackChange{{failing: true}, {}}, stackFailing},
		{"queued", []*stackChange{{queued: true}, {approved: true}}, stackQueued},
		{"all approved", []*stackChange{{approved: true}, {approved: true}}, stackReady},
		{"one approved is only waiting", []*stackChange{{approved: true}, {}}, stackWaiting},
		{"agent", []*stackChange{{agent: true}}, stackAgent},
		{"waiting", []*stackChange{{}}, stackWaiting},
		{"empty", nil, stackWaiting},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, stackStatusOf(c.in), c.name)
	}
}

func TestChecksCounts(t *testing.T) {
	pass, fail, pending := checksCounts([]*git_model.CommitStatus{
		{State: api.CommitStatusSuccess}, {State: api.CommitStatusSkipped}, {State: api.CommitStatusPending},
		{State: api.CommitStatusFailure}, {State: api.CommitStatusError},
	})
	assert.Equal(t, 2, pass)
	assert.Equal(t, 1, pending)
	assert.Equal(t, 2, fail)
}

func TestBuildStackRows(t *testing.T) {
	person := &user_model.User{ID: 1, Name: "priya"}
	bot := &user_model.User{ID: 2, Name: "coder-2", Type: user_model.UserTypeBot}
	other := &user_model.User{ID: 3, Name: "diego"}

	// stack A: 11 (bottom) <- 12 <- 13 (tip), 2 landed below; stack B: a lone agent change; stack C: failing, newer
	a1 := testChange(11, 0, "retry", person, nil)
	a2 := testChange(12, 11, "retry-2", bot, func(c *stackChange) { c.add, c.del, c.hasSize = 10, 2, true; c.pass, c.total = 3, 3 })
	a3 := testChange(13, 12, "retry-3", person, func(c *stackChange) { c.needsMe = true; c.add, c.del, c.hasSize = 5, 1, true; c.pass, c.total = 4, 4 })
	b := testChange(21, 0, "bump", bot, nil)
	c := testChange(31, 0, "ledger", other, func(c *stackChange) { c.failing = true; c.fail, c.total = 1, 31; c.pr.Issue.UpdatedUnix = 500 })

	rows := buildStackRows([]*stackChange{a1, a2, a3, b, c}, map[int64]int{11: 2}, "/acme/pay", person)
	if !assert.Len(t, rows, 3) {
		return
	}
	// needs-review first, failing second, agent last
	assert.Equal(t, []string{stackNeedsReview, stackFailing, stackAgent}, []string{rows[0].Status, rows[1].Status, rows[2].Status})

	r := rows[0]
	assert.Equal(t, "retry", r.Name, "the stack is named by its bottom change")
	assert.Equal(t, "title retry-3", r.Title, "the intent is that of the top change")
	assert.Equal(t, 5, r.Changes)
	assert.Equal(t, 2, r.Landed)
	assert.Equal(t, 3, r.Open)
	assert.True(t, r.Mine)
	assert.True(t, r.Agent)
	assert.True(t, r.NeedsMe)
	assert.Equal(t, 15, r.Additions)
	assert.Equal(t, 3, r.Deletions)
	assert.Equal(t, 7, r.ChecksTotal)
	assert.Equal(t, "/acme/pay/pulls/13", r.Link, "the row opens the change that needs the viewer")
	states := make([]string, len(r.Segments))
	for i, s := range r.Segments {
		states[i] = s.State
	}
	assert.Equal(t, []string{"landed", "landed", "warn", "agent", "warn"}, states)
	assert.Empty(t, r.Segments[0].Link, "a landed change has no link")
	assert.Equal(t, "/acme/pay/pulls/12", r.Segments[3].Link)
	assert.Len(t, r.People, 2)

	assert.False(t, rows[1].Mine)
	assert.False(t, rows[1].Agent)
	assert.Equal(t, 1, rows[2].Changes)
	assert.True(t, rows[2].Agent)

	counts := stackFilterCounts(rows)
	assert.Equal(t, stackCounts{Open: 3, Mine: 1, Agents: 2, NeedsMe: 1}, counts)
}

func TestBuildStackRowsCycleEnds(t *testing.T) {
	p := &user_model.User{ID: 1, Name: "p"}
	x := testChange(1, 2, "x", p, nil)
	y := testChange(2, 1, "y", p, nil)
	// a corrupt cycle has no tip: nothing is drawn and nothing loops
	assert.Empty(t, buildStackRows([]*stackChange{x, y}, nil, "/o/r", nil))
}

func TestBuildStackRowsGuestHasNoMine(t *testing.T) {
	p := &user_model.User{ID: 1, Name: "p"}
	rows := buildStackRows([]*stackChange{testChange(1, 0, "x", p, nil)}, nil, "/o/r", nil)
	assert.False(t, rows[0].Mine)
}

func TestRequestIDIsPrintable(t *testing.T) {
	defer test.MockVariableValue(&setting.Log.RequestIDHeaders, []string{"X-Request-Id"})()
	req := httptest.NewRequest("GET", "/", nil)
	assert.Equal(t, "-", requestID(req))
	req.Header.Set("X-Request-Id", "abc<b>"+strings.Repeat("z", 60))
	got := requestID(req)
	assert.Len(t, got, 39, "capped at 40 bytes before control characters are dropped")
	assert.True(t, strings.HasPrefix(got, "abc<b>"))
}
