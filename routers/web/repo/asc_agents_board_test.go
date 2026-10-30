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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func pendingPermission(id, run uint64) *ascv1.AgentRequest {
	return &ascv1.AgentRequest{
		Id: id, RunId: run, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION,
		State: ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING, Summary: "asks to write", Detail: "infra/limits.tf", Agent: "coder-1", RequestedAt: 1_700_000_000,
	}
}

func TestAscAgentsListShowsTheBoard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	run := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
	run.Progress = &ascv1.AgentRunProgress{StepsDone: 3, StepsTotal: 4}
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{run}})
	hub.ok("Agents.ListAgents", &ascv1.ListRepoAgentsResponse{
		Running: 3, Landed_30D: 18, Changes_30D: 58,
		Days: []*ascv1.AgentActivityDay{{Day: "2026-10-01", Started: 1}, {Day: "2026-10-02", Started: 3, Landed: 2}, {Day: "2026-10-03", Started: 2, Landed: 1}},
		Agents: []*ascv1.RepoAgentInfo{{Agent: "coder-2", Runs: 24, Active: 1, Landed_30D: 11, Paths: []string{"src/**", "tests/**"}}},
	})
	hub.ok("Agents.GetSpend", &ascv1.GetAgentSpendResponse{
		Month: "2026-10", CostMicroUsd: 142_000_000, MonthlyCapMicroUsd: 400_000_000,
		Days: []*ascv1.SpendDay{{Day: "2026-10-01", CostMicroUsd: 5_000_000}, {Day: "2026-10-02", CostMicroUsd: 9_000_000}},
	})
	hub.ok("Agents.ListRequests", &ascv1.ListAgentRequestsResponse{Requests: []*ascv1.AgentRequest{pendingPermission(5, 7), {
		Id: 6, RunId: 8, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN, State: ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING, Summary: "wants approval", Agent: "docs-1",
	}}})

	p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
	require.Equal(t, http.StatusOK, p.status, p.body)
	for _, want := range []string{"asc_review.agents_row_steps", "$142.00", "asc_review.agents_stat_cap", "asc_review.agents_stat_landed_pct", "asc_review.agents_allow_once", "asc_review.agents_review_plan", "coder-2", "src/**, tests/**"} {
		assert.Contains(t, p.body, want)
	}
	// the pending requests are read with the internal credential after the gate, never as a person
	calls := hub.callsOf("Agents.ListRequests")
	require.NotEmpty(t, calls)
	for _, c := range calls {
		isSystemCredential(t, c)
		assert.Equal(t, "user2/repo1", c.req.(*ascv1.ListAgentRequestsRequest).GetRepo().GetName())
	}
	assert.Contains(t, p.body, `action="/user2/repo1/asc/agents/requests/5/decide"`)
	assert.Equal(t, 2, strings.Count(p.body, `class="rv-spark`), "the running and landed cards have a line from the days asc reports")
}

func TestAscAgentsListDegradesWhenTheBoardCallsFail(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)}})
	// ListAgents, GetSpend and ListRequests are not implemented: the page still draws, and says so
	p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, "Add retries to the billing client")
	assert.Contains(t, p.body, "asc_review.agents_partial")
}

func TestAscAgentsListSearchFiltersThePage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	a, b := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING), sampleRun(8, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
	b.Title = "Fix flaky test"
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{a, b}})
	p := runAgents(t, agentsReq{path: testRunsPath + "?q=flaky", user: agentsOwner, features: true}, AscAgents)
	require.Equal(t, http.StatusOK, p.status)
	assert.Contains(t, p.body, "Fix flaky test")
	assert.NotContains(t, p.body, "Add retries to the billing client")
}

func TestAscAgentsDecide(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	allowed := pendingPermission(5, 7)
	allowed.State = ascv1.AgentRequestState_AGENT_REQUEST_STATE_ALLOWED
	hub.ok("Agents.DecideRequest", &ascv1.DecideAgentRequestResponse{Request: allowed, Run: sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)})
	id := map[string]string{":id": "5"}
	path := testRunsPath + "/requests/5/decide"

	t.Run("allow once as the person", func(t *testing.T) {
		p := runAgents(t, agentsReq{method: "POST", path: path, user: agentsOwner, features: true, params: id, form: url.Values{"decision": {"allow"}, "run": {"7"}, "reason": {"fine"}}}, AscAgentsDecide)
		require.Equal(t, http.StatusSeeOther, p.status, p.body)
		assert.Equal(t, testRunsPath+"/runs/7", p.loc)
		calls := hub.callsOf("Agents.DecideRequest")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.NotEmpty(t, calls[0].opID)
		req := calls[0].req.(*ascv1.DecideAgentRequestRequest)
		assert.EqualValues(t, 5, req.GetRequestId())
		assert.Equal(t, ascv1.AgentDecision_AGENT_DECISION_ALLOW_ONCE, req.GetDecision())
		assert.Equal(t, "fine", req.GetReason())
		noCallTokens(t)
	})

	t.Run("a script gets JSON", func(t *testing.T) {
		p := runAgents(t, agentsReq{method: "POST", path: path, user: agentsOwner, features: true, params: id, header: map[string]string{"X-Asc-Fetch": "1"}, form: url.Values{"decision": {"allow"}}}, AscAgentsDecide)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, `"ok":true`)
		assert.Contains(t, p.body, `"state":"allowed"`)
	})

	t.Run("unknown words and long reasons never reach asc", func(t *testing.T) {
		before := len(hub.callsOf("Agents.DecideRequest"))
		for _, f := range []url.Values{{"decision": {"delete"}}, {"decision": {"allow"}, "reason": {strings.Repeat("x", 513)}}, {"decision": {"allow"}, "reason": {"a\nb"}}, {}} {
			p := runAgents(t, agentsReq{method: "POST", path: path, user: agentsOwner, features: true, params: id, form: f}, AscAgentsDecide)
			assert.Equal(t, http.StatusSeeOther, p.status)
			assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
		}
		assert.Len(t, hub.callsOf("Agents.DecideRequest"), before)
	})

	t.Run("a reader cannot decide", func(t *testing.T) {
		before := len(hub.callsOf("Agents.DecideRequest"))
		p := runAgents(t, agentsReq{method: "POST", path: path, user: agentsReader, features: true, params: id, form: url.Values{"decision": {"allow"}}}, AscAgentsDecide)
		assert.Equal(t, http.StatusForbidden, p.status)
		p = runAgents(t, agentsReq{method: "POST", path: path, user: 0, params: id, form: url.Values{"decision": {"allow"}}}, AscAgentsDecide)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.Len(t, hub.callsOf("Agents.DecideRequest"), before)
	})

	t.Run("asc refuses: only a fixed sentence is shown", func(t *testing.T) {
		hub.fail("Agents.DecideRequest", withDetail(t, codes.PermissionDenied, "user is "+testPlantedKey, "PERMISSION_DENIED"))
		p := runAgents(t, agentsReq{method: "POST", path: path, user: agentsOwner, features: true, params: id, form: url.Values{"decision": {"allow"}}}, AscAgentsDecide)
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.NotContains(t, p.ctx.Flash.ErrorMsg, testPlantedKey)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
	})
}
