// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscChecksStateTabsCountAndSideCards(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureCiInsights)
	f.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		st := r.GetStates()[0]
		switch st {
		case ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED:
			if len(r.GetStates()) == 2 { // the list itself: open and acknowledged
				return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{checksGroup(1, "panic", "unit", 7, st)}}, nil
			}
		}
		groups := map[ascv1.FailureGroupState][]*ascv1.FailureGroup{
			ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN:         {checksGroup(1, "panic", "unit", 7, st), checksGroup(2, "timeout", "e2e", 3, st), checksGroup(3, "lint", "unit", 1, st)},
			ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED: {checksGroup(4, "slow", "unit", 1, st)},
			ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED:  {checksGroup(5, "flaky_one", "e2e", 4, st), checksGroup(6, "flaky_two", "e2e", 2, st)},
		}[st]
		return &ascv1.ListFailureGroupsResponse{Groups: groups}, nil
	}
	page := checksPage(t, "", 2)
	require.Equal(t, http.StatusOK, page.status, page.body)

	for key, want := range map[string]string{"open": "3", "acknowledged": "1", "quarantined": "2", "resolved": "0"} {
		at := strings.Index(page.body, `data-testid="asc-checks-count-`+key+`"`)
		require.Positive(t, at, key)
		assert.Contains(t, page.body[at:at+80], ">"+want+"<", "the %s tab counts its groups", key)
	}
	assert.Contains(t, page.body, `data-testid="asc-checks-side"`)
	list := page.body[strings.Index(page.body, `data-testid="asc-quarantined-list"`):]
	assert.Contains(t, list[:600], "tflaky_one")
	assert.Contains(t, list[:600], "tflaky_two")
	assert.Contains(t, page.body, `data-testid="asc-quarantined-manage"`)
	assert.Contains(t, page.body, `class="ds-bar rv-jobbar"`, "each failing job has a bar")
	assert.Contains(t, page.body, `style="width: 100%"`, "the job with the most failures has the full bar")
}

func TestAscChecksCountOfALongListIsOpenEnded(t *testing.T) {
	assert.Equal(t, "100+", ascCountLabel(asctrust.GroupCount{N: 100, More: true}))
	assert.Equal(t, "3", ascCountLabel(asctrust.GroupCount{N: 3}))
}

func TestAscStackRailRowsShowAuthorSizeAndReview(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	bot := &user_model.User{ID: 77, Name: "agent-coder-2", LowerName: "agent-coder-2", Type: user_model.UserTypeBot}
	person := &user_model.User{ID: 78, Name: "priya", LowerName: "priya"}
	html := renderPart(t, "repo/pulls/asc_stack", func(d map[string]any) {
		d["AscStackPanel"] = &pull_service.AscStackPanel{Entries: []pull_service.AscStackEntry{
			{Index: 3, Title: "one", State: "open", Link: "/user2/repo1/pulls/3", Position: 1, Current: true, Poster: bot, Additions: 12, Deletions: 3, HasSize: true, Review: "approved"},
			{Index: 4, Title: "two", State: "open", Link: "/user2/repo1/pulls/4", Position: 2, Poster: person, Review: "changes"},
			{Index: 2, Title: "gone", State: "merged", Link: "/user2/repo1/pulls/2", Position: 0},
		}}
	})
	assert.Equal(t, 2, strings.Count(html, `data-testid="asc-rail-author"`))
	assert.Equal(t, 1, strings.Count(html, `data-testid="agent-mark"`), "only the agent's account carries the mark")
	assert.Contains(t, html, "+12")
	assert.Equal(t, 1, strings.Count(html, `data-testid="asc-rail-size"`), "a row with no size draws none")
	assert.Contains(t, html, "asc_review.rail_approved")
	assert.Contains(t, html, "asc_review.rail_changes")
	assert.Equal(t, 2, strings.Count(html, `data-testid="asc-rail-review"`))

	assert.Equal(t, "changes", ascStackReview(true, true, true))
	assert.Equal(t, "approved", ascStackReview(true, false, true))
	assert.Equal(t, "draft", ascStackReview(true, false, false))
	assert.Empty(t, ascStackReview(false, false, false))
}

func TestAscRestackLine(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := &repo_model.Repository{Name: "repo1", OwnerName: "user2"}
	row := &asctrust.OpRow{Seq: 9, Ref: "refs/heads/feat", ViaKey: "restack", Actor: "alice", Mine: true, At: time.Now().Add(-3 * time.Minute), Undoable: true}

	line := ascRestackLineOf(repo, "main", row, true)
	require.NotNil(t, line)
	assert.Equal(t, "main", line.Base)
	assert.Contains(t, line.UndoURL, "/asc/ops/undo?")
	assert.Contains(t, line.UndoURL, "seq=9")
	assert.Contains(t, line.UndoURL, "ref=refs%2Fheads%2Ffeat")

	assert.Empty(t, ascRestackLineOf(repo, "main", row, false).UndoURL, "no right to write: no Undo")
	notMine := *row
	notMine.Undoable = false
	assert.Empty(t, ascRestackLineOf(repo, "main", &notMine, true).UndoURL, "asc says it cannot be undone")
	push := *row
	push.ViaKey = "push"
	assert.Nil(t, ascRestackLineOf(repo, "main", &push, true), "the newest operation is not a restack")
	assert.Nil(t, ascRestackLineOf(repo, "main", nil, true))

	html := renderPart(t, "repo/pulls/asc_stack", func(d map[string]any) {
		d["AscStackPanel"] = &pull_service.AscStackPanel{Entries: []pull_service.AscStackEntry{{Index: 3, State: "open", Position: 1, Current: true}}}
		d["AscRestackLine"] = line
	})
	assert.Contains(t, html, `data-testid="asc-restacked"`)
	assert.Contains(t, html, "asc_review.restacked_mine")
	assert.Contains(t, html, `data-testid="asc-restacked-undo"`)
}

func TestAscConfirmNamesBranchAndHashesAndFocusesCancel(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	page := func(op string, facts *AscMergeFacts) string {
		return renderPart(t, "repo/asc_confirm", func(d map[string]any) {
			d["Title"] = "Merge now"
			d["AscOp"] = op
			d["AscBack"] = "/user2/repo1/pulls/3"
			d["AscAction"] = "/user2/repo1/pulls/3/asc/merge"
			d["AscCanConfirm"] = true
			d["AscMergeFacts"] = facts
		})
	}
	body := page("merge", &AscMergeFacts{Branch: "main", Current: "aaaaaaaaaaaa", Result: "bbbbbbbbbbbb"})
	assert.Contains(t, body, `data-testid="asc-merge-facts"`)
	assert.Contains(t, body, "<code>main</code>")
	assert.Contains(t, body, "<code>aaaaaaaaaaaa</code>")
	assert.Contains(t, body, "<code>bbbbbbbbbbbb</code>")
	cancel := body[strings.Index(body, `data-testid="asc-confirm-cancel"`)-120:]
	assert.Contains(t, cancel[:200], "autofocus", "Cancel is the default focus of a merge")
	assert.NotContains(t, body[:strings.Index(body, `data-testid="asc-confirm-button"`)], "autofocus")

	queue := page("queue", nil)
	assert.NotContains(t, queue, "asc-merge-facts")
	assert.NotContains(t, queue, "autofocus", "queueing is not destructive")
	assert.Equal(t, "aaaaaaaaaaaa", ascShortChange("aaaaaaaaaaaaffff"))
}

func TestAscMergedMessageUndoLink(t *testing.T) {
	repo := &repo_model.Repository{Name: "repo1", OwnerName: "user2"}
	url := ascUndoURL(repo, &asctrust.OpRow{Seq: 12, Ref: "refs/heads/main"})
	assert.Equal(t, "/user2/repo1/asc/ops/undo?ref=refs%2Fheads%2Fmain&seq=12", url)
}

func TestAscVersionHistory(t *testing.T) {
	job := func(name string, st ascv1.CiStatus) *ascv1.CiJob { return &ascv1.CiJob{Name: name, Status: st} }
	runs := []*ascv1.CiRun{
		{Version: 4, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_FAILURE), job("lint", ascv1.CiStatus_CI_STATUS_FAILURE), job("e2e", ascv1.CiStatus_CI_STATUS_SUCCESS)}},
		{Version: 4, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_SUCCESS)}}, // older run of unit: ignored
		{Version: 3, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_SUCCESS), job("lint", ascv1.CiStatus_CI_STATUS_SUCCESS)}},
		{Version: 2, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_RUNNING), job("x", ascv1.CiStatus_CI_STATUS_CANCELLED)}},
		{Version: 0, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_FAILURE)}},
	}
	got := ascVersionHistoryOf(runs)
	assert.Equal(t, []AscVersionHistoryRow{
		{Version: 4, Failing: 2, Passing: 1},
		{Version: 3, Passing: 2},
		{Version: 2, Running: 1},
	}, got)
	assert.Empty(t, ascVersionHistoryOf(nil))

	html := renderPart(t, "repo/pulls/asc_checks", func(d map[string]any) {
		d["Title"] = "Checks"
		d["AscPullChecks"] = &ascPullChecksPage{
			PullURL: "/user2/repo1/pulls/3", ChecksURL: "/user2/repo1/asc/checks", History: got,
			Checks: asctrust.BuildPullChecks(asctrust.PullChecksIn{Head: "abcdef1234", Now: time.Now(), Statuses: []asctrust.StatusIn{{Context: "unit", State: "success"}}}),
		}
	})
	assert.Contains(t, html, `data-testid="asc-version-history"`)
	assert.Contains(t, html, "asc_trust.pull.history_failing")
	assert.Contains(t, html, "asc_trust.pull.history_passing")
	assert.Contains(t, html, "/user2/repo1/pulls/3/files?asc_to=4")
}

func TestAscPanelChecksCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	panel := func(set func(d map[string]any)) string {
		return renderPart(t, "repo/pulls/asc_panel", func(d map[string]any) {
			d["AscStackPanel"] = &pull_service.AscStackPanel{}
			d["AscVersionChecks"] = map[uint32][]AscVersionCheck{4: {{Name: "unit", Tone: "ok"}, {Name: "lint", Tone: "err"}, {Name: "e2e", Tone: "warn"}}}
			d["AscHeadVersion"] = uint32(4)
			set(d)
		})
	}
	body := panel(func(map[string]any) {})
	assert.Contains(t, body, `data-testid="asc-checks-card"`)
	assert.Contains(t, body, "asc_review.checks_on")
	assert.Contains(t, body, "rv-pill-err")
	assert.Contains(t, body, `data-testid="asc-checks-card-open"`)
	assert.Contains(t, body, "/pulls/3/asc/checks")
	assert.NotContains(t, panel(func(d map[string]any) { d["AscHeadVersion"] = uint32(9) }), "asc-checks-card", "no chips for that version")
	assert.NotContains(t, panel(func(d map[string]any) { delete(d, "AscVersionChecks") }), "asc-checks-card")
}

func TestAscAskAgentToFix(t *testing.T) {
	task := ascAskTask(3, 4, "assertion failed", "thread 'x' panicked")
	assert.Contains(t, task, "change #3 (version 4)")
	assert.Contains(t, task, "Failure: assertion failed")
	assert.Contains(t, task, "Log excerpt:\nthread 'x' panicked")
	assert.NotContains(t, ascAskTask(3, 0, "c", ""), "Log excerpt")
	assert.NotContains(t, ascAskTask(3, 0, "c", ""), "version")

	title := ascAskTitle("a  very\nlong " + strings.Repeat("x", 400))
	assert.LessOrEqual(t, len(title), ascagentui.MaxTitleBytes)
	assert.True(t, strings.HasPrefix(title, "Fix: a very long "))

	require.NoError(t, unittest.PrepareTestDatabase())
	checks := asctrust.BuildPullChecks(asctrust.PullChecksIn{
		Version: 4, Head: "abcdef1234", Now: time.Now(),
		Statuses:    []asctrust.StatusIn{{Context: "unit", State: "failure"}},
		Causes:      []asctrust.CauseIn{{Group: asctrust.GroupRow{ID: 5, Title: "assertion failed", StateKey: "open"}, Occurrences: []asctrust.OccurrenceRow{{RunID: 7, Message: "boom"}}}},
		CausesKnown: true,
	})
	page := func(asks map[uint64]string) string {
		return renderPart(t, "repo/pulls/asc_checks", func(d map[string]any) {
			d["Title"] = "Checks"
			d["AscPullChecks"] = &ascPullChecksPage{PullURL: "/user2/repo1/pulls/3", ChecksURL: "/user2/repo1/asc/checks", Checks: checks, Asks: asks}
		})
	}
	with := page(map[uint64]string{5: "/user2/repo1/asc/agents/new?title=Fix"})
	assert.Contains(t, with, `data-testid="asc-ask-agent"`)
	assert.Contains(t, with, `href="/user2/repo1/asc/agents/new?title=Fix"`)
	assert.NotContains(t, page(nil), "asc-ask-agent")
}

func TestAscAgentFormIsFilledFromTheAddress(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := mockCtxFor(t, "/user2/repo1/asc/agents/new?title=Fix%3A+panic&task=Find+it&agent=coder-2&target=branch2")
	form := ascAgentForm{Agent: "default", Target: "master"}
	ascAgentsPrefill(ctx, &form)
	assert.Equal(t, "Fix: panic", form.Title)
	assert.Equal(t, "Find it", form.Task)
	assert.Equal(t, "coder-2", form.Agent)
	assert.Equal(t, "branch2", form.Target)

	ctx, _ = mockCtxFor(t, "/user2/repo1/asc/agents/new?agent=Bad%20Name&target=no-such-branch&title=two%0Alines")
	form = ascAgentForm{Agent: "default", Target: "master"}
	ascAgentsPrefill(ctx, &form)
	assert.Equal(t, "default", form.Agent, "a bad agent name is dropped")
	assert.Equal(t, "master", form.Target, "a branch that does not exist is dropped")
	assert.Empty(t, form.Title, "a title on two lines is dropped")
}

func TestAscRunTokenCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	row := ascAgentTokenRow{
		ID: "at_0123456789abcdefghij", Paths: []string{"src/**"}, MaxChanges: 4, State: ascagentui.TokenState(ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE),
		Expires: time.Now().Add(time.Hour),
	}
	body := renderPart(t, "repo/asc_agents_run_token", func(d map[string]any) {
		d["AscAgentRunToken"] = row
		d["AscAgentsBase"] = "/user2/repo1/asc/agents"
	})
	assert.Contains(t, body, `data-testid="asc-agent-run-token"`)
	assert.Contains(t, body, "at_0123456789abcdefghij")
	assert.Contains(t, body, "<code>src/**</code>")
	assert.Contains(t, body, "/asc/agents/tokens")
	assert.NotContains(t, renderPart(t, "repo/asc_agents_run_token", func(map[string]any) {}), "asc-agent-run-token", "a viewer who is not an administrator gets no card")
}

func TestJobBarShare(t *testing.T) {
	rows := asctrust.JobStats([]asctrust.GroupRow{
		{JobName: "e2e", Occurrences: 20}, {JobName: "unit", Occurrences: 5}, {JobName: "lint", Occurrences: 0},
	})
	require.Len(t, rows, 3)
	assert.Equal(t, uint32(100), rows[0].Share)
	assert.Equal(t, uint32(25), rows[1].Share)
	assert.Equal(t, uint32(0), rows[2].Share)
}

// mockCtxFor is a request context for repository 1 (user2/repo1) at path.
func mockCtxFor(t *testing.T, path string) (*context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, path)
	contexttest.LoadRepo(t, ctx, 1)
	return ctx, resp
}
