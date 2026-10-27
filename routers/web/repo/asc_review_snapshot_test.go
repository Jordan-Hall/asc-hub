// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/require"
)

// Page snapshots for the design review of the agents, workflows, trust and change pages. Set ASC_REVIEW_SNAPSHOT_DIR
// to a directory and run this test: every page is rendered from fixtures through its real handler and written as
// <name>.html, ready to be opened in a browser with the built stylesheet. Without the variable it does nothing.
func snapshotDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("ASC_REVIEW_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set ASC_REVIEW_SNAPSHOT_DIR to write page snapshots")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

// withLocale runs a handler with the real en-US messages instead of the mock locale that only answers the key, so
// the snapshot shows the words people read.
func withLocale(h func(*context.Context)) func(*context.Context) {
	if len(setting.Langs) == 0 || setting.Langs[0] != "en-US" {
		setting.Langs = []string{"en-US"}
		setting.Names = []string{"English"}
	}
	translation.InitLocales(gocontext.Background())
	return func(c *context.Context) {
		c.Base.Locale = translation.NewLocale("en-US")
		if c.TemplateContext != nil {
			c.TemplateContext.Locale = c.Base.Locale
		}
		h(c)
	}
}

func writeSnapshot(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), []byte(body), 0o644))
}

func TestAscReviewSnapshotAgents(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)

	run := func(id uint64, state ascv1.AgentRunState, agent, title string, changes uint32, tokens uint64) *ascv1.AgentRun {
		r := sampleRun(id, state)
		r.Agent = agent
		r.Title = title
		r.Usage = &ascv1.AgentBudgetUsage{Changes: changes, Tokens: tokens, CostMicroUsd: tokens * 10}
		return r
	}
	runs := []*ascv1.AgentRun{
		run(214, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, "coder-2", "Add retries to the billing client", 3, 116_000),
		run(213, ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT, "coder-1", "Update currency tables for 2026 rates", 2, 88_000),
		run(212, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, "reviewer-1", "Review web-app #88 and leave notes", 1, 31_000),
		run(211, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED, "coder-2", "Add rate limit headers", 4, 142_000),
		run(210, ascv1.AgentRunState_AGENT_RUN_STATE_CANCELLED, "coder-1", "Fix flaky test cursor::page_boundary", 2, 40_000),
		run(209, ascv1.AgentRunState_AGENT_RUN_STATE_FAILED, "docs-1", "Rewrite the migration guide", 0, 12_000),
	}
	runs[5].FailureCode = "BUDGET_EXCEEDED"
	runs[5].FailureMessage = "The run stopped: it reached its token limit before it finished."
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: runs, NextPageToken: "tok1"})
	p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, withLocale(AscAgents))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "agents", p.body)

	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{})
	p = runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, withLocale(AscAgents))
	writeSnapshot(t, dir, "agents-empty", p.body)
	p = runAgents(t, agentsReq{path: testRunsPath + "?state=failed&mine=1", user: agentsOwner, features: true}, withLocale(AscAgents))
	writeSnapshot(t, dir, "agents-nomatch", p.body)
	p = runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: false}, withLocale(AscAgents))
	writeSnapshot(t, dir, "agents-off", p.body)

	hub.fail("Agents.ListRuns", errAscAgentsBadAnswer)
	p = runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, withLocale(AscAgents))
	writeSnapshot(t, dir, "agents-error", p.body)
}

func TestAscReviewSnapshotAgentPages(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	policy := &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Agents: &ascv1.PolicyAgents{
		DefaultScope: []string{"src/**", "tests/**"}, DeniedPaths: []string{"infra/**"}, MaxChangesPerRun: 8,
	}}}
	hub.ok("Policy.GetSource", policy)

	// One run, with a log.
	c1, c2, c3 := changeID(10), changeID(40), changeID(70)
	run := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT)
	run.Agent = "coder-2"
	run.Changes = [][]byte{c1, c2, c3}
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: run})
	tool := ev(2, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_CALL, "docs/retries.md")
	tool.Tool = "edit_file"
	chg := ev(3, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE, "")
	chg.Change = c2
	hub.watch = func(*ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
		return []*ascv1.AgentRunEvent{
			ev(1, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "I will start with the client."),
			tool, chg,
			ev(4, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_ERROR, "scope refused infra/limits.tf"),
		}, false
	}
	p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, withLocale(AscAgentsRun))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "agent-run", p.body)

	hub.ok("AiProviders.ListKeys", &ascv1.ListAiKeysResponse{Keys: []*ascv1.AiKey{
		{Id: keyID('A'), Label: "Work key", Provider: ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, DefaultModel: "claude-sonnet-5-5", HasKey: true, State: ascv1.AiKeyState_AI_KEY_STATE_ACTIVE},
		{Id: keyID('C'), Label: "Capped", Provider: ascv1.AiProvider_AI_PROVIDER_GOOGLE, State: ascv1.AiKeyState_AI_KEY_STATE_CAPPED},
	}})
	hub.ok("AiProviders.ListModels", &ascv1.ListAiModelsResponse{Models: []*ascv1.AiModel{{Id: "claude-sonnet-5-5"}}})
	p = runAgents(t, agentsReq{path: testRunsPath + "/new", user: agentsOwner, features: true}, withLocale(AscAgentsNew))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "agent-new", p.body)
	p = runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: url.Values{"task": {""}, "agent": {"Bad Name"}}}, withLocale(AscAgentsCreate))
	writeSnapshot(t, dir, "agent-new-errors", p.body)

	mk := func(id, name, agent string, st ascv1.AgentTokenState) *ascv1.AgentToken {
		tok := sampleToken(id, st)
		tok.Name, tok.Agent = name, agent
		return tok
	}
	hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{
		mk("at_AAAAAAAAAAAAAAAAAAAA", "my laptop", "coder-2", ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE),
		mk("at_CCCCCCCCCCCCCCCCCCCC", "ci runner", "docs-1", ascv1.AgentTokenState_AGENT_TOKEN_STATE_EXHAUSTED),
		mk("at_BBBBBBBBBBBBBBBBBBBB", "old token", "coder-1", ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED),
	}})
	p = runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, withLocale(AscAgentsTokens))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "agent-tokens", p.body)
	hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{})
	p = runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, withLocale(AscAgentsTokens))
	writeSnapshot(t, dir, "agent-tokens-empty", p.body)
	made := sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)
	hub.ok("AgentTokens.CreateToken", &ascv1.CreateAgentTokenResponse{Token: made, Secret: testTokenSecret})
	hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{made}})
	p = runAgents(t, agentsReq{method: "POST", path: testTokensPath, user: agentsOwner, features: true, form: newTokenForm()}, withLocale(AscAgentsTokenCreate))
	writeSnapshot(t, dir, "agent-token-made", p.body)
}
