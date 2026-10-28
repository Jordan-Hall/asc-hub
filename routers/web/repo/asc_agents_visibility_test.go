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

// What a person typed or an agent wrote on a run is private to the run's creator and to the repository administrators. The
// others who may read the code see the run's metadata and a neutral label (asc_agents_common.go, ascAgentsMaySeeRun).

const (
	secretTask    = "rotate the key sk-live-0123456789 in infra/prod.tf"
	secretRequest = "rm -rf /srv/prod-secrets"
	secretPlan    = "step: dump the production database"
	secretCP      = "checkpoint: copied the prod dump"
	secretFailure = "failed while reading the secret vault"
)

// visibilityRun is a run of user2 (the owner, who also administers repo1) or, with by set, of another person.
func visibilityRun(by string) *ascv1.AgentRun {
	r := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_FAILED)
	r.CreatedBy = by
	r.Task = secretTask
	r.FailureCode = "BUDGET_EXCEEDED"
	r.FailureMessage = secretFailure
	return r
}

func visibilityHub(t *testing.T, by string) *fakeHub {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	run := visibilityRun(by)
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{run}})
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: run})
	hub.ok("Agents.GetRunLog", &ascv1.GetAgentRunLogResponse{Jsonl: []byte("{\"text\":\"" + secretTask + "\"}\n")})
	hub.ok("Agents.ListRequests", &ascv1.ListAgentRequestsResponse{Requests: []*ascv1.AgentRequest{
		{Id: 5, RunId: 7, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION, State: ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING, Summary: secretRequest, Agent: "coder"},
		{Id: 6, RunId: 7, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN, State: ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING, Summary: "plan", Agent: "coder",
			Plan: []*ascv1.AgentPlanStep{{Text: secretPlan}}},
	}})
	hub.ok("Agents.ListCheckpoints", &ascv1.ListAgentCheckpointsResponse{Checkpoints: []*ascv1.AgentCheckpoint{{Id: 1, RunId: 7, Seq: 1, Label: secretCP}}})
	hub.watch = func(*ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
		return []*ascv1.AgentRunEvent{{RunId: 7, Seq: 1, Kind: ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, Text: secretTask}}, false
	}
	return hub
}

func assertNoSecrets(t *testing.T, body string) {
	t.Helper()
	for _, s := range []string{secretTask, secretRequest, secretPlan, secretCP, secretFailure, "sk-live-0123456789"} {
		assert.NotContains(t, body, s)
	}
}

func TestAscAgentsPrivateTextOnTheList(t *testing.T) {
	visibilityHub(t, "user2")

	t.Run("an administrator who is the creator sees the text", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "rotate the key")
		assert.Contains(t, p.body, secretRequest)
	})
	t.Run("another reader sees metadata and a label, and search cannot reach the text", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsReader, features: true}, AscAgents)
		require.Equal(t, http.StatusOK, p.status)
		assertNoSecrets(t, p.body)
		assert.Contains(t, p.body, "asc_review.agents_task_hidden")
		assert.Contains(t, p.body, "Add retries to the billing client", "the run itself still lists")
		assert.Contains(t, p.body, "asc_review.agents_request_hidden")
		assert.NotContains(t, p.body, "asc_review.agents_allow_once", "no decision buttons for text the person may not read")

		p = runAgents(t, agentsReq{path: testRunsPath + "?q=sk-live", user: agentsReader, features: true, form: url.Values{"q": {"sk-live"}}}, AscAgents)
		assertNoSecrets(t, p.body)
		assert.NotContains(t, p.body, "Add retries to the billing client", "the hidden text is not matched")
	})
	t.Run("anonymous gets nothing", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath}, AscAgents)
		assert.Equal(t, http.StatusNotFound, p.status)
		assertNoSecrets(t, p.body)
	})
}

func TestAscAgentsPrivateTextForTheCreatorWhoIsNotAnAdmin(t *testing.T) {
	visibilityHub(t, "user4") // user4 only reads repo1
	p := runAgents(t, agentsReq{path: testRunsPath, user: agentsReader, features: true}, AscAgents)
	require.Equal(t, http.StatusOK, p.status)
	assert.Contains(t, p.body, "rotate the key", "the person who started the run sees its task")
	assert.Contains(t, p.body, secretFailure)
}

func TestAscAgentsPrivateTextOnTheRunPage(t *testing.T) {
	visibilityHub(t, "user2")
	id := map[string]string{":id": "7"}
	path := testRunsPath + "/runs/7"

	p := runAgents(t, agentsReq{path: path, user: agentsOwner, features: true, params: id}, AscAgentsRun)
	require.Equal(t, http.StatusOK, p.status, p.body)
	for _, want := range []string{secretTask, secretRequest, secretPlan, secretCP} {
		assert.Contains(t, p.body, want, "the creator and administrators see %q", want)
	}

	p = runAgents(t, agentsReq{path: path, user: agentsReader, features: true, params: id}, AscAgentsRun)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assertNoSecrets(t, p.body)
	assert.Contains(t, p.body, "asc_review.agents_task_hidden")
	assert.Contains(t, p.body, "asc_review.agents_log_hidden")
	assert.Contains(t, p.body, `data-active="false"`, "a hidden viewer does not follow the live log")
	assert.NotContains(t, p.body, "asc_review.run_cp_rollback")
	assert.NotContains(t, p.body, "/runs/7/log")

	p = runAgents(t, agentsReq{path: path, params: id}, AscAgentsRun)
	assert.Equal(t, http.StatusNotFound, p.status)
	assertNoSecrets(t, p.body)
}

func TestAscAgentsPrivateLogAndPoll(t *testing.T) {
	hub := visibilityHub(t, "user2")
	id := map[string]string{":id": "7"}

	t.Run("the log download", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7/log", user: agentsOwner, features: true, params: id}, AscAgentsLog)
		assert.Equal(t, http.StatusOK, p.status)
		before := len(hub.callsOf("Agents.GetRunLog"))
		p = runAgents(t, agentsReq{path: testRunsPath + "/runs/7/log", user: agentsReader, features: true, params: id}, AscAgentsLog)
		assert.Equal(t, http.StatusForbidden, p.status)
		assertNoSecrets(t, p.body)
		p = runAgents(t, agentsReq{path: testRunsPath + "/runs/7/log", params: id}, AscAgentsLog)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.Len(t, hub.callsOf("Agents.GetRunLog"), before, "asc is not asked for a log that is not shown")
	})

	t.Run("the live feed", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7/events?after=0", user: agentsOwner, features: true, params: id}, AscAgentsEvents)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "sk-live")
		p = runAgents(t, agentsReq{path: testRunsPath + "/runs/7/events?after=0", user: agentsReader, features: true, params: id}, AscAgentsEvents)
		require.Equal(t, http.StatusOK, p.status)
		assertNoSecrets(t, p.body)
		assert.Contains(t, p.body, `"state"`, "the state of the run is not private")
	})
}

func TestAscAgentsPrivateTextAfterADecision(t *testing.T) {
	hub := visibilityHub(t, "user2")
	hub.ok("Agents.DecideRequest", &ascv1.DecideAgentRequestResponse{
		Request: &ascv1.AgentRequest{Id: 5, RunId: 7, Kind: ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION, State: ascv1.AgentRequestState_AGENT_REQUEST_STATE_ALLOWED, Summary: secretRequest},
		Run:     visibilityRun("user2"),
	})
	p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/requests/5/decide", user: agentsOwner, features: true,
		params: map[string]string{":id": "5"}, header: map[string]string{"X-Asc-Fetch": "1"}, form: url.Values{"decision": {"allow"}}}, AscAgentsDecide)
	require.Equal(t, http.StatusOK, p.status)
	assertNoSecrets(t, p.body)
}
