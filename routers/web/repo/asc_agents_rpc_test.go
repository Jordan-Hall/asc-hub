// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The agents and workflows services are never on the read-only RPC proxy: they hold tokens and prompts, and every
// call that changes something has to run as the signed-in person. The pages in asc_agents*.go and asc_workflows.go
// serve them, after the hub's own permission checks. Listing them in the denied set of the proxy's classification
// test keeps that decision visible: moving one to the read allowlist is a deliberate change.
var ascAgentsServedByTheHub = []string{
	"asc.v1.Agents/CreateRun", "asc.v1.Agents/ListRuns", "asc.v1.Agents/GetRun", "asc.v1.Agents/CancelRun",
	"asc.v1.Agents/SendInput", "asc.v1.Agents/WatchRun",
	"asc.v1.AgentTokens/CreateToken", "asc.v1.AgentTokens/ListTokens", "asc.v1.AgentTokens/GetToken",
	"asc.v1.AgentTokens/RevokeToken", "asc.v1.AgentTokens/SetBudget", "asc.v1.AgentTokens/ReportUsage",
	"asc.v1.Workflows/ListDefinitions", "asc.v1.Workflows/GetDefinition", "asc.v1.Workflows/Validate",
	"asc.v1.Workflows/SetEnabled", "asc.v1.Workflows/Trigger", "asc.v1.Workflows/ListRuns", "asc.v1.Workflows/GetRun",
	"asc.v1.Workflows/CancelRun", "asc.v1.Workflows/DecideApproval", "asc.v1.Workflows/WatchRun",
}

func init() {
	for _, m := range ascAgentsServedByTheHub {
		ascRPCTestDenied[m] = "served by the hub's agents and workflows pages, as the signed-in person"
	}
}

func TestAscAgentsServicesAreNotOnTheReadProxy(t *testing.T) {
	for _, m := range ascAgentsServedByTheHub {
		assert.False(t, ascRPCReadMethods[m], "%s must not be served to a browser through the proxy", m)
	}
}
