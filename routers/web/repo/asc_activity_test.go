// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strings"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscActivityMergesSourcesNewestFirst(t *testing.T) {
	in := ascActivityInput{
		Versions: []pull_service.AscVersion{
			{N: 1, At: 100, Actor: "coder-2"},
			{N: 2, At: 300, Actor: "coder-2"},
			{N: 3, At: 500, Actor: "coder-2"},
		},
		Comments: []AscActivityEvent{{Kind: ascActComment, At: 200, Actor: "alice", Link: "#issuecomment-1"}},
		Verdicts: []AscActivityEvent{{Kind: ascActApproved, At: 250, Actor: "bob"}, {Kind: ascActChanges, At: 150, Actor: "carol"}},
		Approvals: []AscApproval{
			{Reviewer: "bob", Version: 1, Applies: true, Carried: true},
			{Reviewer: "dave", Version: 3, Applies: true},
			{Reviewer: "erin", Version: 1, ChangesRequested: true, Carried: true},
		},
		Ops: []asctrust.OpRow{
			{ViaKey: "restack", Actor: "coder-2", At: time.Unix(400, 0)},
			{ViaKey: "push", Actor: "coder-2", At: time.Unix(450, 0)},
			{ViaKey: "agent", Actor: "coder-2", At: time.Unix(460, 0)},
			{ViaKey: "undo", Actor: "alice", At: time.Unix(410, 0)},
		},
	}
	a := ascActivityOf(in)
	var got []string
	for _, e := range a.Events {
		got = append(got, e.Kind)
	}
	// 3 versions, restack, undo, 2 verdicts, 1 carried (bob; dave applies on his own version, erin asked for changes), 1 comment.
	assert.Equal(t, []string{
		ascActVersion,  // 500 v3
		ascActUndo,     // 410
		ascActRestack,  // 400
		ascActVersion,  // 300 v2
		ascActCarried,  // 300 (bob's approval, carried when v2 was pushed)
		ascActApproved, // 250
		ascActComment,  // 200
		ascActChanges,  // 150
		ascActVersion,  // 100 v1
	}, got)
	assert.Equal(t, 9, a.Total, "pushes and agent moves of the ref are the versions already listed")
	assert.Equal(t, ascActivityShown, a.Shown)
	assert.Equal(t, 3, a.Hidden())
	for _, e := range a.Events {
		if e.Kind == ascActCarried {
			assert.Equal(t, uint32(3), e.Version, "carried to the newest version")
			assert.Equal(t, "bob", e.Actor)
		}
	}
}

func TestAscActivityIsCappedAndSmallListsAreOpen(t *testing.T) {
	var vs []pull_service.AscVersion
	for i := 1; i <= ascActivityMax+20; i++ {
		vs = append(vs, pull_service.AscVersion{N: uint32(i), At: int64(i), Actor: "x"})
	}
	a := ascActivityOf(ascActivityInput{Versions: vs})
	assert.Equal(t, ascActivityMax, a.Total)
	assert.Equal(t, uint32(ascActivityMax+20), a.Events[0].Version, "the newest events are kept")

	small := ascActivityOf(ascActivityInput{Versions: vs[:2]})
	assert.Equal(t, 2, small.Shown)
	assert.Zero(t, small.Hidden())
	assert.Zero(t, ascActivityOf(ascActivityInput{}).Total)
}

func TestAscActivityLocalReadsCommentsAndVerdicts(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "/user2/repo1/pulls/3")
	contexttest.LoadRepo(t, ctx, 1)

	three := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	_, verdicts, err := ascActivityLocal(ctx, three)
	require.NoError(t, err)
	kinds := map[string]int{}
	for _, v := range verdicts {
		kinds[v.Kind]++
		assert.NotEmpty(t, v.Actor)
	}
	assert.Positive(t, kinds[ascActApproved], "an approving review")
	assert.Positive(t, kinds[ascActChanges], "a review that asks for changes")

	two := &issues_model.PullRequest{IssueID: 1} // only the issue is read; the fixtures have plain comments on issue 1
	comments, _, err := ascActivityLocal(ctx, two)
	require.NoError(t, err)
	require.NotEmpty(t, comments)
	for _, c := range comments {
		assert.Equal(t, ascActComment, c.Kind)
		assert.True(t, strings.HasPrefix(c.Link, "#issuecomment-"), c.Link)
	}
}

func TestAscActivityTemplate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	a := ascActivityOf(ascActivityInput{
		Versions: []pull_service.AscVersion{{N: 1, At: 100, Actor: "coder-2"}, {N: 2, At: 200, Actor: "coder-2"}},
		Comments: []AscActivityEvent{{Kind: ascActComment, At: 150, Actor: "alice", Link: "#issuecomment-9"}},
	})
	out := renderPart(t, "repo/pulls/asc_activity", func(d map[string]any) { d["AscActivity"] = a })
	assert.Contains(t, out, `data-testid="asc-activity"`)
	assert.Contains(t, out, `data-total="3"`)
	assert.Contains(t, out, `href="#issuecomment-9"`)
	assert.Equal(t, 3, strings.Count(out, `data-kind=`))
	assert.NotContains(t, out, "asc-activity-more", "three events are all open")

	var vs []pull_service.AscVersion
	for i := 1; i <= 9; i++ {
		vs = append(vs, pull_service.AscVersion{N: uint32(i), At: int64(i), Actor: "x"})
	}
	out = renderPart(t, "repo/pulls/asc_activity", func(d map[string]any) { d["AscActivity"] = ascActivityOf(ascActivityInput{Versions: vs}) })
	assert.Contains(t, out, `data-testid="asc-activity-more"`)
	assert.Contains(t, out, "asc_review.activity_show_all")
	assert.Equal(t, 9, strings.Count(out, `data-kind=`), "every event is in the page; the rest sit behind the disclosure")

	assert.Empty(t, strings.TrimSpace(renderPart(t, "repo/pulls/asc_activity", func(map[string]any) {})))
}
