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

func TestAscAgentsSpendPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.GetSpend", &ascv1.GetAgentSpendResponse{
		Month: "2026-10", CostMicroUsd: 58_000_000, Tokens: 1_200_000, MonthlyCapMicroUsd: 120_000_000,
		Days:   []*ascv1.SpendDay{{Day: "2026-10-01", CostMicroUsd: 3_000_000}, {Day: "2026-10-02", CostMicroUsd: 6_000_000}},
		Agents: []*ascv1.AgentSpend{{Agent: "coder-2", CostMicroUsd: 40_000_000, MonthlyCapMicroUsd: 80_000_000}},
	})
	p := runAgents(t, agentsReq{path: testRunsPath + "/spend", user: agentsOwner, features: true}, AscAgentsSpend)
	require.Equal(t, http.StatusOK, p.status, p.body)
	for _, want := range []string{"$58.00", "$120.00", "2026-10-02", "coder-2", "$40.00", "(50%)", `action="/user2/repo1/asc/agents/spend/budget"`} {
		assert.Contains(t, p.body, want)
	}
	for _, c := range hub.callsOf("Agents.GetSpend") {
		isSystemCredential(t, c)
	}

	t.Run("a reader sees the spend but no cap form", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath + "/spend", user: agentsReader, features: true}, AscAgentsSpend)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, "spend/budget")
	})
}

func TestAscAgentsSpendBudget(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.SetMonthlyBudget", &ascv1.SetMonthlyBudgetResponse{})
	path := testRunsPath + "/spend/budget"

	p := runAgents(t, agentsReq{method: "POST", path: path, user: agentsOwner, features: true, form: url.Values{"agent": {"coder-2"}, "cap_usd": {"120.5"}}}, AscAgentsSpendBudget)
	require.Equal(t, http.StatusSeeOther, p.status)
	calls := hub.callsOf("Agents.SetMonthlyBudget")
	require.Len(t, calls, 1)
	isUserCredential(t, calls[0])
	req := calls[0].req.(*ascv1.SetMonthlyBudgetRequest)
	assert.Equal(t, "coder-2", req.GetAgent())
	assert.EqualValues(t, 120_500_000, req.GetCapMicroUsd())

	before := len(hub.callsOf("Agents.SetMonthlyBudget"))
	for _, f := range []url.Values{{"cap_usd": {"-1"}}, {"cap_usd": {"1e9"}}, {"agent": {"../x"}, "cap_usd": {"5"}}} {
		p = runAgents(t, agentsReq{method: "POST", path: path, user: agentsOwner, features: true, form: f}, AscAgentsSpendBudget)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
	}
	// only a repository administrator sets a cap; a reader gets nothing
	p = runAgents(t, agentsReq{method: "POST", path: path, user: agentsReader, features: true, form: url.Values{"cap_usd": {"5"}}}, AscAgentsSpendBudget)
	assert.Equal(t, http.StatusNotFound, p.status)
	assert.Len(t, hub.callsOf("Agents.SetMonthlyBudget"), before)
}
