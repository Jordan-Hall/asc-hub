// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const testWorkflowsPath = "/user2/repo1/asc/workflows"

func sampleDef(name string) *ascv1.WorkflowDefinition {
	return &ascv1.WorkflowDefinition{
		Name:        name,
		Path:        ".asc/workflows/" + name + ".toml",
		Source:      []byte{0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89},
		Description: "Review every new change",
		Enabled:     true,
		Valid:       true,
		Triggers: []*ascv1.WorkflowTrigger{
			{Kind: ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_CHANGE_OPENED, Branches: []string{"main"}, Paths: []string{"src/**"}},
			{Kind: ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_SCHEDULE, Cron: "0 3 * * *"},
		},
		Inputs: []*ascv1.WorkflowInput{
			{Name: "focus", Description: "what to look at", Required: true},
			{Name: "depth", DefaultValue: "normal"},
			{Name: "bad name!", Required: true},
		},
		Steps: []*ascv1.WorkflowStep{
			{Id: "review", Kind: ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_AGENT, Agent: "reviewer", Prompt: "Review {{change}} with <b>care</b>.\x1b[2J", Paths: []string{"src/**"}, Limits: &ascv1.AgentBudgetLimits{MaxTokens: 100_000}},
			{Id: "approve", Kind: ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_APPROVAL, Needs: []string{"review"}, Approvers: []string{"role:maintainer"}, TimeoutSeconds: 86400},
			{Id: "Bad Step", Kind: ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_CHECKS},
			{Id: "checks", Kind: ascv1.WorkflowStepKind_WORKFLOW_STEP_KIND_CHECKS, Needs: []string{"approve", "no way"}, Checks: []string{"build"}},
		},
		Limits:          &ascv1.AgentBudgetLimits{MaxTokens: 300_000},
		MaxParallelRuns: 0,
	}
}

func sampleWorkflowRun(id uint64, state ascv1.WorkflowRunState) *ascv1.WorkflowRun {
	return &ascv1.WorkflowRun{
		Id:        id,
		Repo:      "user2/repo1",
		Workflow:  "review",
		Source:    []byte{0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89},
		State:     state,
		Trigger:   ascv1.WorkflowTriggerKind_WORKFLOW_TRIGGER_KIND_MANUAL,
		StartedBy: "user2",
		Change:    changeID(10),
		Inputs:    map[string]string{"focus": "error handling", "depth": "deep"},
		Created:   1_700_000_000,
		Steps: []*ascv1.WorkflowStepRun{
			{Id: "review", State: ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_SUCCEEDED, AgentRunId: 7, Started: 1_700_000_010, Finished: 1_700_000_100},
			{Id: "approve", State: ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_WAITING, Started: 1_700_000_100},
			{Id: "checks", State: ascv1.WorkflowStepState_WORKFLOW_STEP_STATE_PENDING},
		},
		Usage: &ascv1.AgentBudgetUsage{Tokens: 42_000, CostMicroUsd: 300_000},
	}
}

func TestAscWorkflowsList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	broken := &ascv1.WorkflowDefinition{Name: "broken", Valid: false, Error: "12: unknown key \"colour\"\x1b[31m"}
	hub.ok("Workflows.ListDefinitions", &ascv1.ListWorkflowDefinitionsResponse{Definitions: []*ascv1.WorkflowDefinition{
		sampleDef("review"), broken, sampleDef("runs"), sampleDef("Bad Name"),
	}})
	hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{Runs: []*ascv1.WorkflowRun{sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING)}})

	for _, tc := range []struct {
		name     string
		user     int64
		features bool
	}{
		{"owner with agent features", agentsOwner, true},
		{"owner without agent features", agentsOwner, false},
		{"reader", agentsReader, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := runAgents(t, agentsReq{path: testWorkflowsPath, user: tc.user, features: tc.features}, AscWorkflows)
			require.Equal(t, http.StatusOK, p.status, p.body)
			assert.Contains(t, p.body, `href="/user2/repo1/asc/workflows/review"`)
			assert.Contains(t, p.body, "asc_review.wf_on")
			assert.Contains(t, p.body, "asc_review.wf_not_valid")
			assert.Contains(t, p.body, "12: unknown key &#34;colour&#34;[31m")
			assert.Contains(t, p.body, "asc_review.wf_skipped_one")
			assert.NotContains(t, p.body, `href="/user2/repo1/asc/workflows/runs"`+`>runs`, "runs is the history page")
			assert.Contains(t, p.body, "A change was opened")
			assert.Contains(t, p.body, "asc_review.wf_run_n")
			assert.Equal(t, tc.features, strings.Contains(p.body, `href="/user2/repo1/asc/agents"`), "the agent tabs follow the switch")
			if !tc.features {
				assert.Contains(t, p.body, `data-testid="asc-agents-off"`)
			}
			assert.NotContains(t, p.body, "\x1b")
		})
	}

	calls := hub.callsOf("Workflows.ListDefinitions")
	require.NotEmpty(t, calls)
	isSystemCredential(t, calls[0])

	t.Run("signed out", func(t *testing.T) {
		before := hub.total()
		p := runAgents(t, agentsReq{path: testWorkflowsPath}, AscWorkflows)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.Equal(t, before, hub.total())
	})

	t.Run("none", func(t *testing.T) {
		hub.ok("Workflows.ListDefinitions", &ascv1.ListWorkflowDefinitionsResponse{})
		p := runAgents(t, agentsReq{path: testWorkflowsPath, user: agentsOwner, features: true}, AscWorkflows)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "asc_review.wf_none")
	})

	t.Run("the server has no workflows", func(t *testing.T) {
		hub.fail("Workflows.ListDefinitions", status.Error(codes.Unimplemented, "no"))
		p := runAgents(t, agentsReq{path: testWorkflowsPath, user: agentsOwner, features: true}, AscWorkflows)
		assert.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "does not offer this feature")
	})

	t.Run("workflows are off", func(t *testing.T) {
		hub.fail("Workflows.ListDefinitions", withDetail(t, codes.FailedPrecondition, "off "+testPlantedKey, "WORKFLOW_DISABLED"))
		p := runAgents(t, agentsReq{path: testWorkflowsPath, user: agentsOwner, features: true}, AscWorkflows)
		assert.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "This workflow is switched off")
		assert.NotContains(t, p.body, testPlantedKey)
	})
}

func TestAscWorkflowDefinitionPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Workflows.GetDefinition", &ascv1.GetWorkflowDefinitionResponse{Definition: sampleDef("review"), Text: []byte("description = \"<b>x</b>\"\n[[step]]\nid = \"review\"\x1b[31m\n")})
	hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{Runs: []*ascv1.WorkflowRun{sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_SUCCEEDED)}})
	params := map[string]string{":name": "review"}

	p := runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsOwner, features: true, params: params}, AscWorkflowDef)
	require.Equal(t, http.StatusOK, p.status, p.body)
	get := hub.callsOf("Workflows.GetDefinition")
	require.Len(t, get, 1)
	assert.Equal(t, "review", get[0].req.(*ascv1.GetWorkflowDefinitionRequest).GetName())
	isSystemCredential(t, get[0])

	assert.Contains(t, p.body, "Review every new change")
	assert.Contains(t, p.body, "definition version <code>abcdef012345</code>")
	assert.Contains(t, p.body, "at most 1 run at once")
	// Steps: valid ones only, with their relations.
	assert.Contains(t, p.body, "<code>review</code>")
	assert.NotContains(t, p.body, "Bad Step")
	assert.Contains(t, p.body, "after <code>review</code>")
	assert.NotContains(t, p.body, "no way", "a step id that is not a step id is dropped")
	assert.Contains(t, p.body, "approved by role:maintainer")
	assert.Contains(t, p.body, "times out after 24 h")
	// An agent step's prompt is shown as text, with the switch on.
	assert.Contains(t, p.body, "Review {{change}} with &lt;b&gt;care&lt;/b&gt;.[2J")
	assert.NotContains(t, p.body, "<b>care")
	// The triggers, the inputs (only valid names), the file as text.
	assert.Contains(t, p.body, "A change was opened")
	assert.Contains(t, p.body, "<code>0 3 * * *</code> UTC")
	assert.Contains(t, p.body, `name="in_focus"`)
	assert.Contains(t, p.body, `name="in_depth" type="text" maxlength="4096" value="normal"`, "the default is filled in")
	assert.NotContains(t, p.body, "bad name!")
	assert.Contains(t, p.body, "description = &#34;&lt;b&gt;x&lt;/b&gt;&#34;")
	assert.NotContains(t, p.body, "\x1b")
	// Budget of the agent steps, the latest runs, the on/off button, the trigger form.
	assert.Contains(t, p.body, "300k")
	assert.Contains(t, p.body, "asc_review.wf_run_n")
	assert.Contains(t, p.body, "/workflows/review/enable")
	assert.Contains(t, p.body, "Switch off")
	assert.Contains(t, p.body, `action="/user2/repo1/asc/workflows/review/trigger"`)

	t.Run("without agent features the agent parts are gone", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsOwner, features: false, params: params}, AscWorkflowDef)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "Review every new change")
		assert.NotContains(t, p.body, "Review {{change}}", "a prompt is a task for an agent")
		assert.NotContains(t, p.body, "/trigger")
		assert.NotContains(t, p.body, "/enable")
		assert.Contains(t, p.body, `data-testid="asc-agents-off"`)
	})

	t.Run("a reader reads, and cannot start or switch", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsReader, features: true, params: params}, AscWorkflowDef)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, "/trigger")
		assert.NotContains(t, p.body, "/enable")
	})

	t.Run("an invalid definition cannot be started or switched on", func(t *testing.T) {
		bad := sampleDef("review")
		bad.Valid, bad.Error, bad.Enabled = false, "3: missing id", false
		hub.ok("Workflows.GetDefinition", &ascv1.GetWorkflowDefinitionResponse{Definition: bad})
		p := runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsOwner, features: true, params: params}, AscWorkflowDef)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "3: missing id")
		assert.NotContains(t, p.body, "/trigger")
		assert.NotContains(t, p.body, "/enable")
	})

	t.Run("names", func(t *testing.T) {
		before := hub.total()
		for _, bad := range []string{"runs", "Review", "../x", "a b", ""} {
			p := runAgents(t, agentsReq{path: testWorkflowsPath + "/x", user: agentsOwner, features: true, params: map[string]string{":name": bad}}, AscWorkflowDef)
			assert.Equal(t, http.StatusNotFound, p.status, bad)
		}
		assert.Equal(t, before, hub.total())
	})

	t.Run("the answer must be the definition asked for", func(t *testing.T) {
		hub.ok("Workflows.GetDefinition", &ascv1.GetWorkflowDefinitionResponse{Definition: sampleDef("other")})
		p := runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsOwner, features: true, params: params}, AscWorkflowDef)
		assert.Equal(t, http.StatusBadGateway, p.status)
	})

	t.Run("not found", func(t *testing.T) {
		hub.fail("Workflows.GetDefinition", status.Error(codes.NotFound, "no file "+testPlantedKey))
		p := runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsOwner, features: true, params: params}, AscWorkflowDef)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.NotContains(t, p.body, testPlantedKey)
	})
}

func TestAscWorkflowTrigger(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Workflows.GetDefinition", &ascv1.GetWorkflowDefinitionResponse{Definition: sampleDef("review")})
	hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{})
	started := sampleWorkflowRun(11, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_QUEUED)
	hub.ok("Workflows.Trigger", &ascv1.TriggerWorkflowResponse{Run: started})
	params := map[string]string{":name": "review"}
	post := func(form url.Values) agentsPage {
		return runAgents(t, agentsReq{method: "POST", path: testWorkflowsPath + "/review/trigger", user: agentsOwner, features: true, params: params, form: form}, AscWorkflowTrigger)
	}

	t.Run("starts a run with the inputs the definition names", func(t *testing.T) {
		change := ascagentui.ChangeText(changeID(10))
		p := post(url.Values{
			"in_focus":     {"  error handling "},
			"in_depth":     {""},
			"in_evil":      {"not an input of this workflow"},
			"in_bad name!": {"x"},
			"change":       {change},
			"op":           {"01890000-0000-7000-8000-000000000002"},
		})
		require.Equal(t, http.StatusSeeOther, p.status, p.body)
		assert.Equal(t, "/user2/repo1/asc/workflows/runs/11", p.loc)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "run 11 started")
		calls := hub.callsOf("Workflows.Trigger")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.Equal(t, "01890000-0000-7000-8000-000000000002", calls[0].opID)
		req := calls[0].req.(*ascv1.TriggerWorkflowRequest)
		assert.Equal(t, "review", req.GetName())
		assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
		assert.Equal(t, map[string]string{"focus": "error handling"}, req.GetInputs(), "unknown names are not forwarded, an empty input is left to its default")
		assert.Equal(t, changeID(10), req.GetChange())
		noCallTokens(t)
	})

	t.Run("a required input without a default is needed", func(t *testing.T) {
		before := len(hub.callsOf("Workflows.Trigger"))
		p := post(url.Values{"in_depth": {"deep"}})
		assert.Equal(t, http.StatusBadRequest, p.status)
		assert.Contains(t, p.body, "This input is required")
		assert.Contains(t, p.body, `value="deep"`, "what was typed is kept")
		assert.Len(t, hub.callsOf("Workflows.Trigger"), before)
	})

	t.Run("values and the change id are checked", func(t *testing.T) {
		before := len(hub.callsOf("Workflows.Trigger"))
		for name, form := range map[string]url.Values{
			"nul":          {"in_focus": {"a\x00b"}},
			"too long":     {"in_focus": {strings.Repeat("x", ascagentui.MaxInputValue+1)}},
			"bad change":   {"in_focus": {"ok"}, "change": {"not-a-change"}},
			"commit as id": {"in_focus": {"ok"}, "change": {strings.Repeat("a", 32)}},
		} {
			p := post(form)
			assert.Equal(t, http.StatusBadRequest, p.status, name)
		}
		assert.Len(t, hub.callsOf("Workflows.Trigger"), before)
	})

	t.Run("asc refuses", func(t *testing.T) {
		for code, text := range map[string]string{
			"WORKFLOW_DISABLED": "This workflow is switched off",
			"AGENTS_DISABLED":   "Agents are not turned on",
			"BUDGET_EXCEEDED":   "budget is reached",
			"WORKFLOW_INVALID":  "definition is not valid",
		} {
			hub.fail("Workflows.Trigger", withDetail(t, codes.FailedPrecondition, "x "+testPlantedKey, code, "7: bad"))
			p := post(url.Values{"in_focus": {"x"}})
			assert.Contains(t, p.body, text, code)
			assert.NotContains(t, p.body, testPlantedKey)
			noCallTokens(t)
		}
	})

	t.Run("the answer must be a run of this workflow and repository", func(t *testing.T) {
		other := sampleWorkflowRun(12, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_QUEUED)
		other.Workflow = "nightly"
		hub.ok("Workflows.Trigger", &ascv1.TriggerWorkflowResponse{Run: other})
		p := post(url.Values{"in_focus": {"x"}})
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
		assert.Equal(t, "/user2/repo1/asc/workflows/review", p.loc, "back to the definition, not to the run")
	})

	t.Run("a reader, and a person without the switch, cannot", func(t *testing.T) {
		before := hub.total()
		p := runAgents(t, agentsReq{method: "POST", path: testWorkflowsPath + "/review/trigger", user: agentsReader, features: true, params: params, form: url.Values{"in_focus": {"x"}}}, AscWorkflowTrigger)
		assert.Equal(t, http.StatusForbidden, p.status)
		p = runAgents(t, agentsReq{method: "POST", path: testWorkflowsPath + "/review/trigger", user: agentsOwner, features: false, params: params, form: url.Values{"in_focus": {"x"}}}, AscWorkflowTrigger)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.Equal(t, before, hub.total())
	})
}

func TestAscWorkflowEnable(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.on("Workflows.SetEnabled", func(m proto.Message) (proto.Message, error) {
		d := sampleDef("review")
		d.Enabled = m.(*ascv1.SetWorkflowEnabledRequest).GetEnabled()
		return &ascv1.SetWorkflowEnabledResponse{Definition: d}, nil
	})
	params := map[string]string{":name": "review"}
	do := func(v string) agentsPage {
		return runAgents(t, agentsReq{method: "POST", path: testWorkflowsPath + "/review/enable", user: agentsOwner, features: true, params: params, form: url.Values{"enabled": {v}}}, AscWorkflowEnable)
	}

	p := do("0")
	require.Equal(t, http.StatusSeeOther, p.status)
	assert.Equal(t, "/user2/repo1/asc/workflows/review", p.loc)
	assert.Contains(t, p.ctx.Flash.SuccessMsg, "off")
	p = do("1")
	require.Equal(t, http.StatusSeeOther, p.status)
	assert.Contains(t, p.ctx.Flash.SuccessMsg, "on")
	calls := hub.callsOf("Workflows.SetEnabled")
	require.Len(t, calls, 2)
	isUserCredential(t, calls[0])
	assert.False(t, calls[0].req.(*ascv1.SetWorkflowEnabledRequest).GetEnabled())
	assert.True(t, calls[1].req.(*ascv1.SetWorkflowEnabledRequest).GetEnabled())
	noCallTokens(t)

	before := hub.total()
	for _, bad := range []string{"", "yes", "true", "2"} {
		assert.Equal(t, http.StatusBadRequest, do(bad).status, bad)
	}
	assert.Equal(t, before, hub.total())

	t.Run("only a maintainer, which asc decides", func(t *testing.T) {
		hub.fail("Workflows.SetEnabled", status.Error(codes.PermissionDenied, "not a maintainer "+testPlantedKey))
		p := do("1")
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, "You may not do that.", p.ctx.Flash.ErrorMsg)
	})
}

func TestAscWorkflowRunHistory(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	target := changeID(10)
	mk := func(id uint64, change []byte) *ascv1.WorkflowRun {
		r := sampleWorkflowRun(id, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_SUCCEEDED)
		r.Change = change
		return r
	}
	hub.on("Workflows.ListRuns", func(m proto.Message) (proto.Message, error) {
		req := m.(*ascv1.ListWorkflowRunsRequest)
		switch req.GetPageToken() {
		case "":
			return &ascv1.ListWorkflowRunsResponse{Runs: []*ascv1.WorkflowRun{mk(30, target), mk(29, changeID(50))}, NextPageToken: "p2"}, nil
		case "p2":
			return &ascv1.ListWorkflowRunsResponse{Runs: []*ascv1.WorkflowRun{mk(28, nil), mk(27, target)}}, nil
		}
		return nil, status.Error(codes.InvalidArgument, "page")
	})
	get := func(q string) agentsPage {
		return runAgents(t, agentsReq{path: testWorkflowsPath + "/runs" + q, user: agentsReader, features: false}, AscWorkflowRuns)
	}

	t.Run("plain history is paged by asc", func(t *testing.T) {
		p := get("?workflow=review&state=succeeded")
		require.Equal(t, http.StatusOK, p.status, p.body)
		req := hub.callsOf("Workflows.ListRuns")[0].req.(*ascv1.ListWorkflowRunsRequest)
		assert.Equal(t, "review", req.GetName())
		assert.Equal(t, []ascv1.WorkflowRunState{ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_SUCCEEDED}, req.GetStates())
		assert.EqualValues(t, 30, req.GetLimit())
		assert.Contains(t, p.body, "review · run 30")
		assert.Contains(t, p.body, "review · run 29")
		assert.Contains(t, p.body, "definition <code>abcdef012345</code>", "each row names the definition version it ran")
		assert.Contains(t, p.body, "page=p2")
		assert.Contains(t, p.body, "workflow=review")
	})

	t.Run("per change: the runs about one change, across pages", func(t *testing.T) {
		text := ascagentui.ChangeText(target)
		p := get("?change=" + text)
		require.Equal(t, http.StatusOK, p.status, p.body)
		assert.Contains(t, p.body, "review · run 30")
		assert.Contains(t, p.body, "review · run 27")
		assert.NotContains(t, p.body, "review · run 29")
		assert.NotContains(t, p.body, "review · run 28")
		assert.Contains(t, p.body, "4 runs were looked at")
		assert.NotContains(t, p.body, "may exist")
	})

	t.Run("a scan that stops says so", func(t *testing.T) {
		hub.on("Workflows.ListRuns", func(m proto.Message) (proto.Message, error) {
			return &ascv1.ListWorkflowRunsResponse{Runs: []*ascv1.WorkflowRun{mk(5, changeID(50))}, NextPageToken: "more"}, nil
		})
		p := get("?change=" + ascagentui.ChangeText(target))
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "Only the latest 5 runs were looked at; older ones may exist")
		assert.Contains(t, p.body, "No runs match")
		assert.Len(t, hub.callsOf("Workflows.ListRuns"), 3+ascWorkflowsScanPages)
	})

	t.Run("filters are checked first", func(t *testing.T) {
		before := hub.total()
		for _, q := range []string{"?state=nope", "?workflow=Bad%20Name", "?workflow=runs", "?change=short", "?change=" + strings.Repeat("a", 32), "?page=a%20b"} {
			assert.Equal(t, http.StatusBadRequest, get(q).status, q)
		}
		assert.Equal(t, before, hub.total())
	})

	t.Run("a run of another repository is a bad answer", func(t *testing.T) {
		other := mk(1, target)
		other.Repo = "user3/secret"
		other.Workflow = "STRANGER"
		hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{Runs: []*ascv1.WorkflowRun{other}})
		p := get("")
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, "STRANGER")
	})
}

func TestAscWorkflowRunPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	waiting := sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL)
	waiting.Steps[0].Comment = "all <i>good</i>"
	waiting.Steps[1].Error = "timed out\x1b[0m"
	hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: waiting})
	hub.on("Agents.GetRun", func(m proto.Message) (proto.Message, error) {
		return &ascv1.GetAgentRunResponse{Run: sampleRun(m.(*ascv1.GetAgentRunRequest).GetId(), ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)}, nil
	})
	hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
		var evs []*ascv1.AgentRunEvent
		for i := uint64(1); i <= 30; i++ {
			e := ev(i, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_RESULT, "log-line-"+idStr(i))
			e.RunId = r.GetId()
			evs = append(evs, e)
		}
		return evs, false
	}
	params := map[string]string{":id": "3"}
	page := func(user int64, features bool) agentsPage {
		return runAgents(t, agentsReq{path: testWorkflowsPath + "/runs/3", user: user, features: features, params: params}, AscWorkflowRun)
	}

	p := page(agentsOwner, true)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-poll-url="/user2/repo1/asc/workflows/runs/3/state"`)
	assert.Contains(t, p.body, `data-active="true"`)
	assert.Contains(t, p.body, `data-state="waiting"`)
	assert.Contains(t, p.body, "definition version <code>abcdef012345</code>")
	assert.Contains(t, p.body, "error handling")
	assert.Contains(t, p.body, "“all &lt;i&gt;good&lt;/i&gt;”")
	assert.Contains(t, p.body, "timed out[0m")
	assert.NotContains(t, p.body, "\x1b")
	assert.Contains(t, p.body, "/workflows/runs?change="+ascagentui.ChangeText(changeID(10)), "all runs for this change")
	// Logs of the agent step: the newest 25 of 30, with a note.
	assert.Contains(t, p.body, "log-line-30")
	assert.NotContains(t, p.body, "log-line-5<")
	assert.Contains(t, p.body, "5 older not shown")
	assert.Contains(t, p.body, `href="/user2/repo1/asc/agents/runs/7"`)
	// The approval form, the stop button.
	assert.Contains(t, p.body, `data-testid="asc-workflow-approval"`)
	assert.Contains(t, p.body, `name="decision" value="approve"`)
	assert.Contains(t, p.body, `name="decision" value="reject"`)
	assert.Contains(t, p.body, "/runs/3/cancel")
	assert.NotContains(t, p.body, "/runs/3/rerun", "a run that is going on is not run again")
	assert.Contains(t, p.body, "42k")
	// The agent run behind the log was checked to be this repository's.
	assert.NotEmpty(t, hub.callsOf("Agents.GetRun"))

	t.Run("without the switch: no logs, no agent links, no forms", func(t *testing.T) {
		p := page(agentsOwner, false)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, "log-line-30")
		assert.NotContains(t, p.body, `href="/user2/repo1/asc/agents/runs/7"`)
		assert.Contains(t, p.body, "agent run 7")
		assert.NotContains(t, p.body, "asc-workflow-approval")
		assert.NotContains(t, p.body, "/runs/3/cancel")
	})

	t.Run("a reader sees the steps, without forms", func(t *testing.T) {
		p := page(agentsReader, true)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "approve")
		assert.NotContains(t, p.body, "asc-workflow-approval")
		assert.NotContains(t, p.body, "/runs/3/cancel")
	})

	t.Run("a finished run can be run again", func(t *testing.T) {
		done := sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_FAILED)
		done.Error = "step review failed"
		hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: done})
		p := page(agentsOwner, true)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, `data-active="false"`)
		assert.Contains(t, p.body, "/runs/3/rerun")
		assert.NotContains(t, p.body, "/runs/3/cancel")
		assert.Contains(t, p.body, "step review failed")
	})

	t.Run("a log of an agent run of another repository is not shown", func(t *testing.T) {
		hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: waiting})
		hub.on("Agents.GetRun", func(m proto.Message) (proto.Message, error) {
			r := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
			r.Repo = "user3/secret"
			return &ascv1.GetAgentRunResponse{Run: r}, nil
		})
		p := page(agentsOwner, true)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, "log-line-30")
	})

	t.Run("a run of another repository is a bad answer", func(t *testing.T) {
		other := sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING)
		other.Repo = "user3/secret"
		hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: other})
		assert.Equal(t, http.StatusBadGateway, page(agentsOwner, true).status)
		hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: sampleWorkflowRun(4, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING)})
		assert.Equal(t, http.StatusBadGateway, page(agentsOwner, true).status, "asked for run 3, answered with run 4")
	})
}

func TestAscWorkflowRunState(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	run := sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING)
	run.Steps[2].Error = "bad <b>x</b>"
	hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: run})
	p := runAgents(t, agentsReq{path: testWorkflowsPath + "/runs/3/state", user: agentsReader, features: false, params: map[string]string{":id": "3"}}, AscWorkflowRunState)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Equal(t, "no-store", p.resp.Header().Get("Cache-Control"))
	assert.Contains(t, p.body, `"state":"running"`)
	assert.Contains(t, p.body, `"active":true`)
	assert.Contains(t, p.body, `"id":"approve","state":"waiting"`)
	assert.NotContains(t, p.body, "<b>x</b>", "JSON text is escaped")

	assert.Equal(t, http.StatusNotFound, runAgents(t, agentsReq{path: testWorkflowsPath + "/runs/x/state", user: agentsReader, params: map[string]string{":id": "x"}}, AscWorkflowRunState).status)
	assert.Equal(t, http.StatusNotFound, runAgents(t, agentsReq{path: testWorkflowsPath + "/runs/3/state", params: map[string]string{":id": "3"}}, AscWorkflowRunState).status, "signed out")
}

func TestAscWorkflowRunActions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	run := sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL)
	hub.ok("Workflows.CancelRun", &ascv1.CancelWorkflowRunResponse{Run: run})
	hub.ok("Workflows.DecideApproval", &ascv1.DecideWorkflowApprovalResponse{Run: run})
	hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: run})
	hub.ok("Workflows.Trigger", &ascv1.TriggerWorkflowResponse{Run: sampleWorkflowRun(9, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_QUEUED)})
	params := map[string]string{":id": "3"}
	back := "/user2/repo1/asc/workflows/runs/3"
	act := func(h func(*context.Context), suffix string, form url.Values) agentsPage {
		return runAgents(t, agentsReq{method: "POST", path: back + suffix, user: agentsOwner, features: true, params: params, form: form}, h)
	}

	t.Run("cancel", func(t *testing.T) {
		p := act(AscWorkflowRunCancel, "/cancel", url.Values{"reason": {"not needed"}})
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, back, p.loc)
		calls := hub.callsOf("Workflows.CancelRun")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.Equal(t, "not needed", calls[0].req.(*ascv1.CancelWorkflowRunRequest).GetReason())
		noCallTokens(t)
		p = act(AscWorkflowRunCancel, "/cancel", url.Values{"reason": {"a\nb"}})
		assert.Contains(t, p.ctx.Flash.ErrorMsg, "reason")
		assert.Len(t, hub.callsOf("Workflows.CancelRun"), 1)
	})

	t.Run("decide", func(t *testing.T) {
		p := act(AscWorkflowRunDecide, "/decide", url.Values{"step": {"approve"}, "decision": {"approve"}, "comment": {"looks fine"}})
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "Approved")
		p = act(AscWorkflowRunDecide, "/decide", url.Values{"step": {"approve"}, "decision": {"reject"}})
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "Rejected")
		calls := hub.callsOf("Workflows.DecideApproval")
		require.Len(t, calls, 2)
		isUserCredential(t, calls[0])
		req := calls[0].req.(*ascv1.DecideWorkflowApprovalRequest)
		assert.EqualValues(t, 3, req.GetId())
		assert.Equal(t, "approve", req.GetStep())
		assert.True(t, req.GetApprove())
		assert.Equal(t, "looks fine", req.GetComment())
		assert.False(t, calls[1].req.(*ascv1.DecideWorkflowApprovalRequest).GetApprove())
		noCallTokens(t)
	})

	t.Run("decide: only a decision of two, on a real step id", func(t *testing.T) {
		before := len(hub.callsOf("Workflows.DecideApproval"))
		for name, form := range map[string]url.Values{
			"no decision":  {"step": {"approve"}},
			"odd decision": {"step": {"approve"}, "decision": {"maybe"}},
			"bad step":     {"step": {"Not A Step"}, "decision": {"approve"}},
			"no step":      {"decision": {"approve"}},
		} {
			assert.Equal(t, http.StatusBadRequest, act(AscWorkflowRunDecide, "/decide", form).status, name)
		}
		long := act(AscWorkflowRunDecide, "/decide", url.Values{"step": {"approve"}, "decision": {"approve"}, "comment": {strings.Repeat("c", 1025)}})
		assert.Contains(t, long.ctx.Flash.ErrorMsg, "comment")
		assert.Len(t, hub.callsOf("Workflows.DecideApproval"), before)
	})

	t.Run("an approval that asc refuses: not the named approver", func(t *testing.T) {
		hub.fail("Workflows.DecideApproval", status.Error(codes.PermissionDenied, "you are the starter "+testPlantedKey))
		p := act(AscWorkflowRunDecide, "/decide", url.Values{"step": {"approve"}, "decision": {"approve"}})
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, "You may not do that.", p.ctx.Flash.ErrorMsg)
		assert.Empty(t, p.ctx.Flash.SuccessMsg)
	})

	t.Run("run again", func(t *testing.T) {
		p := act(AscWorkflowRunRerun, "/rerun", nil)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, "/user2/repo1/asc/workflows/runs/9", p.loc)
		calls := hub.callsOf("Workflows.Trigger")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		req := calls[0].req.(*ascv1.TriggerWorkflowRequest)
		assert.Equal(t, "review", req.GetName())
		assert.Equal(t, map[string]string{"focus": "error handling", "depth": "deep"}, req.GetInputs())
		assert.Equal(t, changeID(10), req.GetChange())
		noCallTokens(t)
	})

	t.Run("run again does not forward inputs that are not inputs", func(t *testing.T) {
		odd := sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_FAILED)
		odd.Inputs = map[string]string{"focus": "ok", "bad name!": "x", "big": strings.Repeat("v", ascagentui.MaxInputValue+1)}
		hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: odd})
		act(AscWorkflowRunRerun, "/rerun", nil)
		calls := hub.callsOf("Workflows.Trigger")
		assert.Equal(t, map[string]string{"focus": "ok"}, calls[len(calls)-1].req.(*ascv1.TriggerWorkflowRequest).GetInputs())
	})

	t.Run("a reader, signed out, or without the switch, cannot act", func(t *testing.T) {
		before := hub.total()
		for _, h := range []struct {
			f      func(*context.Context)
			suffix string
		}{{AscWorkflowRunCancel, "/cancel"}, {AscWorkflowRunDecide, "/decide"}, {AscWorkflowRunRerun, "/rerun"}} {
			form := url.Values{"step": {"approve"}, "decision": {"approve"}}
			p := runAgents(t, agentsReq{method: "POST", path: back + h.suffix, user: agentsReader, features: true, params: params, form: form}, h.f)
			assert.Equal(t, http.StatusForbidden, p.status, h.suffix)
			p = runAgents(t, agentsReq{method: "POST", path: back + h.suffix, user: agentsOwner, features: false, params: params, form: form}, h.f)
			assert.Equal(t, http.StatusNotFound, p.status, h.suffix)
			p = runAgents(t, agentsReq{method: "POST", path: back + h.suffix, params: params, form: form}, h.f)
			assert.Equal(t, http.StatusNotFound, p.status, h.suffix)
		}
		assert.Equal(t, before, hub.total())
	})
}
