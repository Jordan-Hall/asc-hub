// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/url"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascbabysit"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscNewChangeRailBelowCountsTheChangesUnderTheOneOpened(t *testing.T) {
	var none *AscNewChangeRail
	assert.Zero(t, none.Below())
	r := &AscNewChangeRail{Stack: []AscRailChange{{Current: true}, {}, {}}, StackMore: 2}
	assert.Equal(t, 4, r.Below(), "the listed changes under it and the ones not listed")
	r.Stack = []AscRailChange{{}, {Current: true}}
	r.StackMore = 0
	assert.Zero(t, r.Below(), "the bottom of the stack")
	r.Stack = []AscRailChange{{}, {}}
	assert.Zero(t, r.Below(), "a stack without the change")
}

func TestAscOptAgentsKeepTheAgentsThatMayWrite(t *testing.T) {
	assert.Equal(t, []string{"coder-2", "fixer"}, ascOptAgentsOf([]ascagentui.Roster{
		{Name: "coder-2", Scope: "src/**"}, {Name: "reader", Scope: "read-only"}, {Name: "fixer", Scope: "tests/**"},
	}))
	assert.Equal(t, []string{ascAgentsDefaultAgent}, ascOptAgentsOf([]ascagentui.Roster{{Name: "reader", Scope: "read-only"}}), "with none, the default agent")
	var many []ascagentui.Roster
	for i := 0; i < ascOptAgentsMax+3; i++ {
		many = append(many, ascagentui.Roster{Name: "a" + strings.Repeat("b", i), Scope: "**"})
	}
	assert.Len(t, ascOptAgentsOf(many), ascOptAgentsMax)
}

func TestPrepareAscNewChangeOptionsOffersWhatThePersonMayDo(t *testing.T) {
	hub := startFakeHub(t)
	hub.ok("Agents.ListAgents", &ascv1.ListRepoAgentsResponse{Agents: []*ascv1.RepoAgentInfo{
		{Agent: "coder-2", Paths: []string{"src/**"}}, {Agent: "reader"},
	}})
	env := railCtx(t, true)
	env.ctx.Data["AscNewChangeRail"] = &AscNewChangeRail{Stack: []AscRailChange{{Current: true}, {}}}
	env.ctx.Data["HeadRepo"] = env.ctx.Repo.Repository
	prepareAscNewChangeOptions(env.ctx)
	opts, ok := env.ctx.Data["AscNewChangeOptions"].(*AscNewChangeOptions)
	require.True(t, ok)
	assert.Equal(t, &AscNewChangeOptions{DraftPrefix: "WIP:", Below: 1, CanLand: true, Agents: []string{"coder-2"}, Runs: ascbabysit.MaxRuns}, opts)

	env = railCtx(t, true)
	env.ctx.Data["HeadRepo"] = &repo_model.Repository{ID: 11}
	prepareAscNewChangeOptions(env.ctx)
	opts = env.ctx.Data["AscNewChangeOptions"].(*AscNewChangeOptions)
	assert.Nil(t, opts.Agents, "an agent cannot push to the branch of a fork")
	assert.Len(t, hub.callsOf("Agents.ListAgents"), 1)

	env = railCtx(t, false)
	prepareAscNewChangeOptions(env.ctx)
	assert.Nil(t, env.ctx.Data["AscNewChangeOptions"], "nobody is signed in")

	defer test.MockVariableValue(&setting.Repository.PullRequest.WorkInProgressPrefixes, nil)()
	ctx, _ := contexttest.MockContext(t, "user2/repo1/compare/master...branch2")
	contexttest.LoadUser(t, ctx, 4)
	ctx.IsSigned = true
	contexttest.LoadRepo(t, ctx, 1)
	prepareAscNewChangeOptions(ctx)
	assert.Nil(t, ctx.Data["AscNewChangeOptions"], "no row to offer: the card is left out")
}

func optCtx(t *testing.T, user int64, form url.Values) *context.Context {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/compare/master...branch2")
	contexttest.LoadUser(t, ctx, user)
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadGitRepo(t, ctx)
	t.Cleanup(func() { ctx.Repo.GitRepo.Close() })
	ctx.Req.PostForm = form
	return ctx
}

func TestAscNewChangeTitlePutsTheDraftPrefixInFront(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := optCtx(t, 2, url.Values{ascOptDraftField: {"1"}})
	title, ok := ascNewChangeTitle(ctx, "Refund rounding")
	assert.True(t, ok)
	assert.Equal(t, "WIP: Refund rounding", title)
	title, _ = ascNewChangeTitle(ctx, "[WIP] Refund rounding")
	assert.Equal(t, "[WIP] Refund rounding", title, "a title that is a draft already")

	_, ok = ascNewChangeTitle(ctx, strings.Repeat("é", 252))
	assert.False(t, ok, "the prefix does not fit")

	ctx = optCtx(t, 2, url.Values{})
	title, ok = ascNewChangeTitle(ctx, "Refund rounding")
	assert.True(t, ok)
	assert.Equal(t, "Refund rounding", title)
}

func TestAscApplyNewChangeOptionsWatchesTheChecksAndSchedulesTheMerge(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer automerge.UseAscQueueBacked(func(*repo_model.Repository) (bool, error) { return true, nil })()
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})

	ctx := optCtx(t, 2, url.Values{ascOptLandField: {"1"}, ascOptBabysitField: {"1"}, ascOptAgentField: {" coder-2 "}})
	ascApplyNewChangeOptions(ctx, issue, pr)
	assert.Empty(t, ctx.Flash.WarningMsg)
	w := unittest.AssertExistsAndLoadBean(t, &ascbabysit.AscBabysit{PullID: 2})
	assert.Equal(t, "coder-2", w.Agent)
	assert.EqualValues(t, 2, w.DoerID)
	// A change that is not stacked lands by the scheduled merge of the hub.
	has, sched, err := pull_model.GetScheduledMergeByPullID(ctx, 2)
	require.NoError(t, err)
	require.True(t, has)
	assert.EqualValues(t, 2, sched.DoerID)

	ctx = optCtx(t, 2, url.Values{ascOptBabysitField: {"1"}, ascOptAgentField: {"no agent!"}})
	ascApplyNewChangeOptions(ctx, issue, pr)
	assert.Equal(t, "asc_rp.opt_babysit_not", ctx.Flash.WarningMsg)

	ctx = optCtx(t, 4, url.Values{ascOptLandField: {"1"}})
	require.NoError(t, pull_model.DeleteScheduledAutoMerge(ctx, 2))
	ascApplyNewChangeOptions(ctx, issue, pr)
	assert.Equal(t, "asc_rp.opt_land_not", ctx.Flash.WarningMsg, "a person who may not merge")
	has, _, _ = pull_model.GetScheduledMergeByPullID(ctx, 2)
	assert.False(t, has)

	draft := *issue
	draft.Title = "WIP: " + issue.Title
	ctx = optCtx(t, 2, url.Values{ascOptLandField: {"1"}})
	ascApplyNewChangeOptions(ctx, &draft, pr)
	assert.Equal(t, "asc_rp.opt_land_draft", ctx.Flash.WarningMsg)
	has, _, _ = pull_model.GetScheduledMergeByPullID(ctx, 2)
	assert.False(t, has, "a draft is not set to land")
}

func TestAscNewChangeOptionsTemplate(t *testing.T) {
	opts := &AscNewChangeOptions{DraftPrefix: "WIP:", Below: 3, CanLand: true, Agents: []string{"coder-2"}, Runs: 3}
	out := renderPart(t, "repo/issue/asc_newchange_options", func(d map[string]any) { d["AscNewChangeOptions"] = opts })
	for _, want := range []string{
		`data-testid="asc-newchange-options"`, "asc_rp.opt_title", `name="asc_draft"`, `data-testid="asc-opt-stack"`,
		"asc_rp.opt_stack_sub_", `name="asc_land"`, `role="switch" name="asc_babysit"`,
		`<input type="hidden" name="asc_babysit_agent" value="coder-2">`, "asc_rp.opt_babysit<",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "<select")

	opts = &AscNewChangeOptions{Agents: []string{"coder-2", "fixer"}, Runs: 3}
	out = renderPart(t, "repo/issue/asc_newchange_options", func(d map[string]any) { d["AscNewChangeOptions"] = opts })
	assert.Contains(t, out, `<option value="fixer">fixer</option>`)
	assert.Contains(t, out, "asc_rp.opt_babysit_any")
	for _, gone := range []string{`name="asc_draft"`, `asc-opt-stack`, `name="asc_land"`} {
		assert.NotContains(t, out, gone)
	}

	assert.Empty(t, strings.TrimSpace(renderPart(t, "repo/issue/asc_newchange_options", func(map[string]any) {})))
}
