// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

func TestAscAgentSamplePath(t *testing.T) {
	for glob, want := range map[string]string{
		"src/**": "src/file", "**/*.md": "dir/file.md", "*.go": "file.go", "docs/a?.txt": "docs/aa.txt", "README.md": "README.md",
		"a/[bc]/x": "", "{a,b}/x": "", "../x": "", "": "",
	} {
		assert.Equal(t, want, ascAgentSamplePath(glob), glob)
	}
}

func TestAscAgentsMayDoEndpointJudgesTheAgentAndTheScope(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.on("Policy.Evaluate", func(m proto.Message) (proto.Message, error) {
		r := m.(*ascv1.EvaluatePolicyRequest)
		if r.GetAction() == ascv1.PolicyAction_POLICY_ACTION_RUN {
			return &ascv1.EvaluatePolicyResponse{RunAllowed: false, RunReason: "at most 3 changes per run\x00"}, nil
		}
		var ds []*ascv1.PolicyDecision
		for _, p := range r.GetPaths() {
			ds = append(ds, &ascv1.PolicyDecision{Path: p, Allowed: string(p) != ".github/file", Reason: "denied path"})
		}
		return &ascv1.EvaluatePolicyResponse{Decisions: ds}, nil
	})
	path := testRunsPath + "/may-do"
	form := url.Values{"agent": {"coder-2"}, "paths": {"src/**\n.github/**"}, "max_changes": {"9"}, "max_paths": {"40"}}
	p := runAgents(t, agentsReq{path: path, user: agentsOwner, features: true, form: form}, AscAgentsMayDo)
	require.Equal(t, http.StatusOK, p.status, p.body)
	var got ascAgentMayDo
	require.NoError(t, json.Unmarshal([]byte(p.body), &got))
	assert.True(t, got.Known)
	assert.False(t, got.RunAllowed)
	assert.Equal(t, "at most 3 changes per run", got.RunReason, "asc's text is cleaned")
	require.Len(t, got.Writes, 2)
	assert.True(t, got.Writes[0].Allowed)
	assert.False(t, got.Writes[1].Allowed)
	assert.Equal(t, ".github/**", got.Writes[1].Glob)

	calls := hub.callsOf("Policy.Evaluate")
	require.Len(t, calls, 2)
	w := calls[0].req.(*ascv1.EvaluatePolicyRequest)
	assert.Equal(t, ascv1.PolicyAction_POLICY_ACTION_WRITE, w.GetAction())
	assert.Equal(t, "agent", w.GetPrincipal().GetKind())
	assert.Equal(t, "coder-2", w.GetPrincipal().GetName())
	assert.Equal(t, [][]byte{[]byte("src/file"), []byte(".github/file")}, w.GetPaths())
	r := calls[1].req.(*ascv1.EvaluatePolicyRequest)
	assert.EqualValues(t, 9, r.GetRunChanges())
	assert.EqualValues(t, 40, r.GetRunPaths())

	// bad input is refused before asc is asked; a reader may not ask at all; asc failing leaves the card unknown
	n := len(hub.callsOf("Policy.Evaluate"))
	for _, f := range []url.Values{{"agent": {"../x"}}, {"agent": {"coder-2"}, "max_changes": {"-1"}}} {
		assert.Equal(t, http.StatusBadRequest, runAgents(t, agentsReq{path: path, user: agentsOwner, features: true, form: f}, AscAgentsMayDo).status)
	}
	assert.Equal(t, http.StatusForbidden, runAgents(t, agentsReq{path: path, user: agentsReader, features: true, form: url.Values{"agent": {"coder-2"}}}, AscAgentsMayDo).status)
	assert.Len(t, hub.callsOf("Policy.Evaluate"), n)
	hub.fail("Policy.Evaluate", status.Error(codes.Unavailable, "down"))
	p = runAgents(t, agentsReq{path: path, user: agentsOwner, features: true, form: form}, AscAgentsMayDo)
	require.Equal(t, http.StatusOK, p.status)
	assert.Contains(t, p.body, `"known":false`)
}

func TestAscAgentsNewFormShowsWhatTheRunMayDo(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Policy.GetSource", &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Agents: &ascv1.PolicyAgents{DefaultScope: []string{"src/**"}}}})
	hub.on("Policy.Evaluate", func(m proto.Message) (proto.Message, error) {
		r := m.(*ascv1.EvaluatePolicyRequest)
		if r.GetAction() == ascv1.PolicyAction_POLICY_ACTION_RUN {
			return &ascv1.EvaluatePolicyResponse{RunAllowed: true}, nil
		}
		return &ascv1.EvaluatePolicyResponse{Decisions: []*ascv1.PolicyDecision{{Path: r.GetPaths()[0], Allowed: true}}}, nil
	})
	p := runAgents(t, agentsReq{path: testRunsPath + "/new", user: agentsOwner, features: true}, AscAgentsNew)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-testid="asc-agent-maydo"`)
	assert.Contains(t, p.body, "<code>src/**</code>", "with no scope typed the default scope of the policy is judged")
	assert.Contains(t, p.body, `data-asc-maydo-input`)

	hub.fail("Policy.Evaluate", status.Error(codes.Unavailable, "down"))
	p = runAgents(t, agentsReq{path: testRunsPath + "/new", user: agentsOwner, features: true}, AscAgentsNew)
	require.Equal(t, http.StatusOK, p.status)
	assert.Contains(t, p.body, "data-md-unknown>", "asc silent: the card says so and the form still works")
	assert.NotContains(t, p.body, "data-md-unknown hidden")
}
