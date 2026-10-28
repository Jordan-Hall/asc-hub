// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscAgentsCreatePassesPlanFirstAndNamesLoweredLimits(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.CreateRun", &ascv1.CreateAgentRunResponse{Run: sampleRun(9, ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED), Lowered: []string{"cost"}})
	form := url.Values{"task": {"Add retries"}, "agent": {"coder-2"}, "target": {"master"}, "plan_first": {"1"}}
	p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: form}, AscAgentsCreate)
	require.Equal(t, http.StatusSeeOther, p.status, p.body)
	calls := hub.callsOf("Agents.CreateRun")
	require.Len(t, calls, 1)
	isUserCredential(t, calls[0])
	assert.True(t, calls[0].req.(*ascv1.CreateAgentRunRequest).GetPlanFirst())
	assert.Contains(t, p.ctx.Flash.WarningMsg, "cost", "a lowered limit is said, never silent")
}

func TestAscAgentsNewFormExtras(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.ListAgents", &ascv1.ListRepoAgentsResponse{Agents: []*ascv1.RepoAgentInfo{{Agent: "coder-2", Paths: []string{"src/**"}}}})
	hub.ok("Agents.GetEstimate", &ascv1.GetAgentEstimateResponse{Samples: 14, Seconds: 600, CostMicroUsd: 1_600_000})
	hub.ok("Agents.RecentTasks", &ascv1.RecentAgentTasksResponse{Tasks: []*ascv1.AgentTaskRef{{Task: "Add rate limit headers", Title: "Add rate limit headers", Agent: "coder-2", RunId: 4}, {Task: "no run id", RunId: 0}}})

	p := runAgents(t, agentsReq{path: testRunsPath + "/new", user: agentsOwner, features: true}, AscAgentsNew)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, "$1.60")
	assert.Contains(t, p.body, "10 min")
	assert.Contains(t, p.body, `data-asc-reuse="Add rate limit headers"`)
	assert.NotContains(t, p.body, "no run id")
	assert.Contains(t, p.body, `<option value="coder-2">src/**</option>`)
	// the recent tasks are the person's: read as the person, so asc applies its own rule on whose tasks they are
	calls := hub.callsOf("Agents.RecentTasks")
	require.Len(t, calls, 1)
	isUserCredential(t, calls[0])
	noCallTokens(t)
}

func TestAscAgentsEstimateEndpoint(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.GetEstimate", &ascv1.GetAgentEstimateResponse{Samples: 3, Seconds: 90, CostMicroUsd: 500_000})
	path := testRunsPath + "/estimate"

	p := runAgents(t, agentsReq{path: path + "?agent=coder-2&bytes=120", user: agentsOwner, features: true, form: url.Values{"agent": {"coder-2"}, "bytes": {"120"}}}, AscAgentsEstimate)
	require.Equal(t, http.StatusOK, p.status)
	assert.Contains(t, p.body, `"known":true`)
	assert.Contains(t, p.body, `"cost":"$0.50"`)
	req := hub.callsOf("Agents.GetEstimate")[0].req.(*ascv1.GetAgentEstimateRequest)
	assert.EqualValues(t, 120, req.GetTaskBytes())

	before := len(hub.callsOf("Agents.GetEstimate"))
	for _, f := range []url.Values{{"agent": {"../x"}}, {"agent": {"coder-2"}, "bytes": {"-1"}}} {
		p = runAgents(t, agentsReq{path: path, user: agentsOwner, features: true, form: f}, AscAgentsEstimate)
		assert.Equal(t, http.StatusBadRequest, p.status)
	}
	assert.Equal(t, http.StatusForbidden, runAgents(t, agentsReq{path: path, user: agentsReader, features: true, form: url.Values{"agent": {"coder-2"}}}, AscAgentsEstimate).status)
	assert.Len(t, hub.callsOf("Agents.GetEstimate"), before)
}
