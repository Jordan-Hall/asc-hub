// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscAgentsRunControls(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	id := map[string]string{":id": "7"}
	back := testRunsPath + "/runs/7"
	running := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
	hub.ok("Agents.PauseRun", &ascv1.PauseAgentRunResponse{Run: running})
	hub.ok("Agents.SetRunLimits", &ascv1.SetAgentRunLimitsResponse{Run: running, Lowered: []string{"cost"}})
	next := sampleRun(8, ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED)
	hub.ok("Agents.ContinueRun", &ascv1.ContinueAgentRunResponse{Run: next})
	hub.ok("Agents.RollbackRun", &ascv1.RollbackAgentRunResponse{Run: running, Checkpoint: &ascv1.AgentCheckpoint{Id: 3, RunId: 7}})
	post := func(h func(c *context.Context), form url.Values, user int64) agentsPage {
		return runAgents(t, agentsReq{method: "POST", path: back, user: user, features: true, params: id, form: form}, h)
	}

	t.Run("pause then resume, as the person", func(t *testing.T) {
		p := post(AscAgentsPause, url.Values{"paused": {"1"}, "reason": {"lunch"}}, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, back, p.loc)
		calls := hub.callsOf("Agents.PauseRun")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		req := calls[0].req.(*ascv1.PauseAgentRunRequest)
		assert.True(t, req.GetPaused())
		assert.Equal(t, "lunch", req.GetReason())
		post(AscAgentsPause, url.Values{"paused": {"0"}}, agentsOwner)
		assert.False(t, hub.callsOf("Agents.PauseRun")[1].req.(*ascv1.PauseAgentRunRequest).GetPaused())
		noCallTokens(t)
	})

	t.Run("limits go to asc as typed and a lowered limit is named", func(t *testing.T) {
		p := post(AscAgentsLimits, url.Values{"max_tokens": {"300000"}, "max_cost_usd": {"5.5"}, "max_minutes": {"30"}}, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "cost")
		req := hub.callsOf("Agents.SetRunLimits")[0].req.(*ascv1.SetAgentRunLimitsRequest)
		assert.EqualValues(t, 300000, req.GetLimits().GetMaxTokens())
		assert.EqualValues(t, 5_500_000, req.GetLimits().GetMaxCostMicroUsd())
		assert.EqualValues(t, 1800, req.GetLimits().GetMaxRuntimeSeconds())
	})

	t.Run("a limit that is not a number never reaches asc", func(t *testing.T) {
		before := len(hub.callsOf("Agents.SetRunLimits"))
		p := post(AscAgentsLimits, url.Values{"max_tokens": {"-5"}}, agentsOwner)
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
		assert.Len(t, hub.callsOf("Agents.SetRunLimits"), before)
	})

	t.Run("continue opens the new run", func(t *testing.T) {
		p := post(AscAgentsContinue, url.Values{"max_tokens": {"400000"}}, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, testRunsPath+"/runs/8", p.loc)
		isUserCredential(t, hub.callsOf("Agents.ContinueRun")[0])
	})

	t.Run("rollback needs a checkpoint and is made as the person", func(t *testing.T) {
		p := post(AscAgentsRollback, url.Values{}, agentsOwner)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
		assert.Empty(t, hub.callsOf("Agents.RollbackRun"))
		p = post(AscAgentsRollback, url.Values{"checkpoint": {"3"}, "reason": {"wrong turn"}}, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		calls := hub.callsOf("Agents.RollbackRun")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		req := calls[0].req.(*ascv1.RollbackAgentRunRequest)
		assert.EqualValues(t, 3, req.GetCheckpointId())
		assert.EqualValues(t, 7, req.GetRunId())
	})

	t.Run("a checkpoint of another run is refused", func(t *testing.T) {
		hub.ok("Agents.RollbackRun", &ascv1.RollbackAgentRunResponse{Run: running, Checkpoint: &ascv1.AgentCheckpoint{Id: 3, RunId: 99}})
		p := post(AscAgentsRollback, url.Values{"checkpoint": {"3"}}, agentsOwner)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
	})

	t.Run("a reader and a stranger change nothing", func(t *testing.T) {
		total := hub.total()
		for _, h := range []func(c *context.Context){AscAgentsPause, AscAgentsLimits, AscAgentsContinue, AscAgentsRollback} {
			assert.Equal(t, http.StatusForbidden, post(h, url.Values{"checkpoint": {"3"}}, agentsReader).status)
			assert.Equal(t, http.StatusNotFound, post(h, url.Values{"checkpoint": {"3"}}, 0).status)
		}
		assert.Equal(t, total, hub.total())
	})
}

func TestAscAgentsLogDownload(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	id := map[string]string{":id": "7"}
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)})
	hub.ok("Agents.GetRunLog", &ascv1.GetAgentRunLogResponse{Jsonl: []byte("{\"seq\":1}\n"), Truncated: true})

	p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7/log", user: agentsOwner, features: true, params: id}, AscAgentsLog)
	require.Equal(t, http.StatusOK, p.status)
	assert.Equal(t, "{\"seq\":1}\n", p.body)
	assert.Contains(t, p.resp.Header().Get("Content-Disposition"), "attachment")
	assert.Equal(t, "nosniff", p.resp.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "1", p.resp.Header().Get("X-Asc-Truncated"))
	for _, c := range hub.callsOf("Agents.GetRunLog") {
		isSystemCredential(t, c)
	}

	t.Run("the run must belong to this repository", func(t *testing.T) {
		other := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
		other.Repo = "someone/else"
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: other})
		before := len(hub.callsOf("Agents.GetRunLog"))
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7/log", user: agentsOwner, features: true, params: id}, AscAgentsLog)
		assert.NotEqual(t, http.StatusOK, p.status)
		assert.Len(t, hub.callsOf("Agents.GetRunLog"), before)
	})
}
