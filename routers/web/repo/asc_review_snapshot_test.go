// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/asctrust"
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

func TestAscReviewSnapshotWorkflows(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Policy.GetSource", &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Agents: &ascv1.PolicyAgents{DefaultScope: []string{"src/**"}}}})
	broken := &ascv1.WorkflowDefinition{Name: "hotfix-gate", Valid: false, Description: "Extra approvals for changes on release branches", Error: "Step gate names approver role:releaser, which does not exist."}
	off := sampleDef("nightly-fuzz")
	off.Enabled = false
	hub.ok("Workflows.ListDefinitions", &ascv1.ListWorkflowDefinitionsResponse{Definitions: []*ascv1.WorkflowDefinition{sampleDef("dependency-update"), sampleDef("changed-src-review"), off, broken}})
	runs := []*ascv1.WorkflowRun{
		sampleWorkflowRun(1042, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING),
		sampleWorkflowRun(1041, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_SUCCEEDED),
		sampleWorkflowRun(1040, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL),
		sampleWorkflowRun(1039, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_FAILED),
	}
	hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{Runs: runs, NextPageToken: "tok1"})
	p := runAgents(t, agentsReq{path: testWorkflowsPath, user: agentsOwner, features: true}, withLocale(AscWorkflows))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "workflows", p.body)
	p = runAgents(t, agentsReq{path: testWorkflowsPath + "/runs", user: agentsOwner, features: true}, withLocale(AscWorkflowRuns))
	writeSnapshot(t, dir, "workflow-runs", p.body)

	hub.ok("Workflows.ListDefinitions", &ascv1.ListWorkflowDefinitionsResponse{})
	hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{})
	p = runAgents(t, agentsReq{path: testWorkflowsPath, user: agentsOwner, features: true}, withLocale(AscWorkflows))
	writeSnapshot(t, dir, "workflows-empty", p.body)

	hub.ok("Workflows.GetDefinition", &ascv1.GetWorkflowDefinitionResponse{Definition: sampleDef("review"), Text: []byte("description = \"Review every new change\"\n[[step]]\nid = \"review\"\n")})
	hub.ok("Workflows.ListRuns", &ascv1.ListWorkflowRunsResponse{Runs: runs[:2]})
	p = runAgents(t, agentsReq{path: testWorkflowsPath + "/review", user: agentsOwner, features: true, params: map[string]string{":name": "review"}}, withLocale(AscWorkflowDef))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "workflow-def", p.body)

	hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_WAITING_APPROVAL)})
	p = runAgents(t, agentsReq{path: testWorkflowsPath + "/runs/3", user: agentsOwner, features: true, params: map[string]string{":id": "3"}}, withLocale(AscWorkflowRun))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "workflow-run", p.body)
}

func TestAscReviewSnapshotTrust(t *testing.T) {
	dir := snapshotDir(t)

	// Queue with two entries, a quarantine and a finished one.
	f := newQRFixture(t)
	ft := startFakeTrust(t, asctrust.FeatureCiInsights, asctrust.FeatureStacks, asctrust.FeatureAudit)
	ft.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{
			quarantinedGroup(4, "webhook::refund_webhook_timeout", "Flaky since the gateway sandbox moved regions. Owner: Priya Raman."), quarantinedGroup(5, "ledger::tests::reconcile_batch", ""),
		}}, nil
	}
	qrRequest(t, AscQueueEnqueue, 2, f.top, "user2/repo1/pulls/5/asc/enqueue", nil)
	p := runTrust(t, trustReq{path: "/user2/repo1/asc/queue", user: 2, access: perm_model.AccessModeAdmin}, withLocale(AscQueuePage))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "queue", p.body)

	ft.listRefLog = func(r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{
			opsEntry(9, "refs/heads/retry-with-backoff", "user2", 1, 2, true, ""),
			opsEntry(8, "refs/heads/main", "someone", 3, 4, false, "not your operation"),
			opsEntry(7, "refs/heads/main", "user2", 5, 6, false, "outside the undo window of a protected ref"),
			opsEntry(6, "refs/heads/odd", "other-name", 5, 6, false, "the ref moved since this operation"),
			opsEntry(5, "refs/heads/new", "user2", 0, 6, false, "something the hub does not know"),
		}, NextPageToken: "5"}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/ops", user: 2, access: perm_model.AccessModeWrite}, withLocale(AscOps))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "ops", p.body)
	ft.listRefLog = func(r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
		return &ascv1.ListRefLogResponse{}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/ops?mine=1", user: 2, access: perm_model.AccessModeWrite}, withLocale(AscOps))
	writeSnapshot(t, dir, "ops-empty", p.body)
	previewServer(ft, opsEntry(9, "refs/heads/feature", "user2", 1, 2, true, ""))
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/ops/undo?ref=refs/heads/feature&seq=9", user: 2, access: perm_model.AccessModeWrite}, withLocale(AscOpsUndoPreview))
	writeSnapshot(t, dir, "ops-undo", p.body)

	open := ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN
	ft.listGroups = func(r *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{
			checksGroup(1, "panic", "unit", 7, open), checksGroup(2, "timeout", "e2e", 20, open), checksGroup(3, "lint", "unit", 2, open),
		}, NextPageToken: "n_3"}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/checks", user: 2, access: perm_model.AccessModeRead}, withLocale(AscChecks))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "checks", p.body)
	ft.listFlaky = func(r *ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
		return &ascv1.ListFlakyTestsResponse{Tests: []*ascv1.FlakyTest{{TestName: "retry::zero", JobName: "unit", FlakeRate: 0.125, Runs: 40, GroupId: 9, LastFlip: 1_700_000_000}, {TestName: "net::dial", FlakeRate: 0.05, Runs: 20}}}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/checks?view=flaky", user: 2, access: perm_model.AccessModeRead}, withLocale(AscChecks))
	writeSnapshot(t, dir, "checks-flaky", p.body)

	ft.auditList = func(r *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{
			auditEvent(9, "repo.ref.update", ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS),
			auditEvent(8, "policy.proposed", ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED),
			auditEvent(7, "storage.target.set", ascv1.AuditOutcome_AUDIT_OUTCOME_FAILED),
		}, NextPageToken: "7"}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit", user: 2, access: adminAccess}, withLocale(AscAudit))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "audit", p.body)
}
