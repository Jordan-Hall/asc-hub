// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	issues_model "forgejo.org/models/issues"
	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/translation"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func initSnapshotLocales() {
	if len(setting.Langs) == 0 || setting.Langs[0] != "en-US" {
		setting.Langs = []string{"en-US"}
		setting.Names = []string{"English"}
	}
	translation.InitLocales(gocontext.Background())
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

func TestAscReviewSnapshotGate(t *testing.T) {
	dir := snapshotDir(t)
	f := gateFake(t)
	p := gatePage(t, "", 2, adminAccess)
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "gate", p.body)

	f.auditList = func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{}, nil
	}
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{}, nil
	}
	p = gatePage(t, "?days=30", 2, adminAccess)
	writeSnapshot(t, dir, "gate-empty", p.body)

	p = gatePage(t, "", 4, perm_model.AccessModeRead)
	writeSnapshot(t, dir, "gate-reader", p.body)

	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return gatePolicySource(false), nil
	}
	p = gatePage(t, "", 2, adminAccess)
	writeSnapshot(t, dir, "gate-invalid", p.body)

	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return &ascv1.GetPolicySourceResponse{}, nil
	}
	p = gatePage(t, "", 2, adminAccess)
	writeSnapshot(t, dir, "gate-absent", p.body)

	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return nil, status.Error(codes.Unavailable, "secret-internal-detail")
	}
	p = gatePage(t, "", 2, adminAccess)
	writeSnapshot(t, dir, "gate-error", p.body)
}

func TestAscReviewSnapshotCompare(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	f := startFakeBadges(t)
	sha := func(b byte) []byte {
		out := make([]byte, 20)
		for i := range out {
			out[i] = b
		}
		return out
	}
	f.compare = func(r *ascv1.CompareRequest) (*ascv1.CompareResponse, error) {
		return &ascv1.CompareResponse{
			MergeBase: sha(0xab), Ahead: 3, Behind: 1,
			AheadEntries: []*ascv1.LogEntry{
				{Git: sha(0x11), Message: []byte("Reject duplicate refund requests that reuse a key\n\nbody"), Author: &ascv1.Signature{Name: []byte("Maya Okafor")}},
				{Git: sha(0x22), Message: []byte("Add idempotency key column"), Author: &ascv1.Signature{Name: []byte("Diego Alvarez")}},
				{Git: sha(0x33), Message: []byte("Define the key type"), Author: &ascv1.Signature{Name: []byte("Diego Alvarez")}},
			},
			BehindEntries: []*ascv1.LogEntry{{Git: sha(0x44), Message: []byte("Bump the toolchain"), Author: &ascv1.Signature{Name: []byte("Priya Raman")}}},
			Files:         []*ascv1.PathChange{{Path: []byte("src/refunds/handler.rs"), Status: "M"}, {Path: []byte("src/refunds/keys.rs"), Status: "A"}, {Path: []byte("tests/refunds.rs"), Status: "M"}, {Path: []byte("docs/old.md"), Status: "D"}},
		}, nil
	}
	get := func(query string) string {
		ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/compare"+query)
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Data["RepoLink"] = ctx.Repo.RepoLink
		withLocale(AscCompare)(ctx)
		return resp.Body.String()
	}
	writeSnapshot(t, dir, "asc-compare", get("?a=main&b=idempotency-keys"))
	f.compare = func(*ascv1.CompareRequest) (*ascv1.CompareResponse, error) {
		return &ascv1.CompareResponse{MergeBase: sha(0xab)}, nil
	}
	writeSnapshot(t, dir, "asc-compare-nothing", get("?a=main&b=main"))
	writeSnapshot(t, dir, "asc-compare-form", get(""))
	writeSnapshot(t, dir, "asc-compare-bad", get("?a=--bad&b=main"))
}

// renderFragment renders one template with the given data and the real messages, and writes it as frag-<name>.html.
func renderFragment(t *testing.T, dir, name, tpl string, data map[string]any) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/pulls/2/files")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Base.Locale = translation.NewLocale("en-US")
	if ctx.TemplateContext != nil {
		ctx.TemplateContext.Locale = ctx.Base.Locale
	}
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 2})
	require.NoError(t, issue.LoadRepo(gocontext.Background()))
	ctx.Data["Issue"] = issue
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	for k, v := range data {
		ctx.Data[k] = v
	}
	ctx.HTML(200, base.TplName(tpl))
	require.Equal(t, 200, resp.Code, resp.Body.String())
	writeSnapshot(t, dir, "frag-"+name, resp.Body.String())
}

func TestAscReviewSnapshotChangeParts(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	initSnapshotLocales()
	panel := &pull_service.AscStackPanel{
		Entries: []pull_service.AscStackEntry{
			{Index: 11, Title: "Add the retry policy type", State: "merged", Link: "/user2/repo1/pulls/11", Position: 1},
			{Index: 12, Title: "Backoff with jitter", State: "open", Link: "/user2/repo1/pulls/12", Position: 2},
			{Index: 13, Title: "Wire the retry policy into the client", State: "open", Link: "/user2/repo1/pulls/13", Position: 3, Current: true},
			{Index: 14, Title: "Docs and retry metrics", State: "open", Link: "/user2/repo1/pulls/14", Position: 4},
		},
		NeedsRestack: true, MergeBlocked: true,
	}
	panel.Parent = &panel.Entries[1]
	actions := &AscStackActions{CanQueue: true, CanMerge: true, CanRestack: true, CanRevert: false, ConfirmURL: "/user2/repo1/pulls/13/asc/confirm", QueueURL: "/user2/repo1/asc/queue"}
	renderFragment(t, dir, "stack", "repo/pulls/asc_stack", map[string]any{"AscStackPanel": panel, "AscActions": actions, "AscQueueStates": map[int64]string{14: "queued"}})
	panel2 := *panel
	panel2.NeedsRestack, panel2.MergeBlocked = false, false
	renderFragment(t, dir, "stack-plain", "repo/pulls/asc_stack", map[string]any{"AscStackPanel": &panel2, "AscActions": &AscStackActions{CanUnqueue: true, CanRevert: true, ConfirmURL: "/x", QueueURL: "/q"}})

	versions := &pull_service.AscVersionList{Versions: []pull_service.AscVersion{
		{N: 1, At: 1_700_000_000, Actor: "coder-2"}, {N: 2, At: 1_700_100_000, Actor: "ASC, automatic"},
		{N: 3, At: 1_700_200_000, Actor: "coder-2"}, {N: 4, At: 1_700_300_000, Actor: "coder-2"},
	}}
	sigs := map[uint32]asctrust.Badge{
		1: {Label: "Verified", Tone: "ok", Title: "Signed by key mo_41c9"}, 2: {Label: "Unsigned", Tone: "ro", Title: "Made by the server"},
		3: {Label: "Verified", Tone: "ok", Title: "Signed"}, 4: {Label: "Unverified", Tone: "wn", Title: "The key is not known"},
	}
	vdata := map[string]any{"AscVersions": versions, "AscVersionFrom": uint32(3), "AscVersionTo": uint32(4), "AscHeadVersion": uint32(4), "AscCanRestore": true, "AscSignatures": sigs, "AscSinceVersion": uint32(2)}
	renderFragment(t, dir, "versions", "repo/pulls/asc_versions", vdata)

	renderFragment(t, dir, "badges", "repo/pulls/asc_badges", map[string]any{"AscBadges": &AscBadges{
		Merge:    &AscMergeBadge{State: AscMergeConflicts, Conflicts: []string{"docs/retries.md", "src/policy.rs"}, MoreConflicts: 3},
		Overlaps: []AscOverlapBadge{{Change: "mmmmmmmm", PullIndex: 12, PullLink: "/user2/repo1/pulls/12", ReviewState: "open", Approvals: 1, Paths: []string{"src/payments/client.rs", "src/payments/refund.rs"}, MorePaths: 2}},
	}})
	renderFragment(t, dir, "panel", "repo/pulls/asc_panel", map[string]any{"AscStackPanel": panel})
}

// renderPage renders a whole page template of the repository with the given data and the real messages, and writes
// it as <name>.html.
func renderPage(t *testing.T, dir, name, tpl string, data map[string]any) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/pulls/13/asc/confirm")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Base.Locale = translation.NewLocale("en-US")
	if ctx.TemplateContext != nil {
		ctx.TemplateContext.Locale = ctx.Base.Locale
	}
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	for k, v := range data {
		ctx.Data[k] = v
	}
	ctx.HTML(200, base.TplName(tpl))
	require.Equal(t, 200, resp.Code, resp.Body.String())
	writeSnapshot(t, dir, name, resp.Body.String())
}

func TestAscReviewSnapshotConfirm(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	initSnapshotLocales()
	rows := []AscConfirmRow{
		{Index: 11, Title: "Add the retry policy type", Link: "/user2/repo1/pulls/11", Status: "ready"},
		{Index: 12, Title: "Backoff with jitter", Link: "/user2/repo1/pulls/12", Status: "ready", Detail: "Approved, restacked"},
		{Index: 13, Title: "Wire the retry policy into the client", Link: "/user2/repo1/pulls/13", Status: "not_ready", Detail: "Needs your review"},
	}
	data := map[string]any{"Title": "Merge the stack", "AscOp": "merge", "AscRows": rows, "AscCanConfirm": true, "AscAction": "/user2/repo1/pulls/13/asc/merge", "AscBack": "/user2/repo1/pulls/13", "AscPreviewNote": "Both changes are approved and signed."}
	renderPage(t, dir, "confirm", "repo/asc_confirm", data)
	data["AscCanConfirm"] = false
	data["AscFailure"] = "The trunk moved while you decided. Reload and check the dry run again."
	data["AscNeedsContentMerge"], data["AscNoContentMerge"] = true, true
	renderPage(t, dir, "confirm-blocked", "repo/asc_confirm", data)
	data["AscOp"], data["AscFailure"] = "revert", ""
	data["AscNeedsContentMerge"], data["AscNoContentMerge"] = false, false
	data["AscCanConfirm"] = true
	renderPage(t, dir, "confirm-revert", "repo/asc_confirm", data)
}

func TestAscReviewSnapshotDeps(t *testing.T) {
	dir := snapshotDir(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	finding := func(id uint64, sev ascv1.DepSeverity, state ascv1.DepFindingState, name, ver, eco, id2, sum string) *ascv1.DepFinding {
		d := depsFinding(id, sev, state)
		d.Name, d.Version, d.Ecosystem, d.Advisory.Id, d.Advisory.Summary = name, ver, eco, id2, sum
		d.Direct = id != 1
		d.Path = []byte("Cargo.lock")
		return d
	}
	f.depFindings = func(r *ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{
			Findings: []*ascv1.DepFinding{
				finding(1, ascv1.DepSeverity_DEP_SEVERITY_HIGH, ascv1.DepFindingState_DEP_FINDING_STATE_OPEN, "tokio", "1.38.0", "crates.io", "RUSTSEC-2025-0044", "Unbounded memory growth when a connection is closed during a read"),
				finding(2, ascv1.DepSeverity_DEP_SEVERITY_MODERATE, ascv1.DepFindingState_DEP_FINDING_STATE_OPEN, "vite", "5.2.8", "npm", "GHSA-q7x4-2m8r-vh5c", "Dev server serves files outside the project root on Windows"),
			},
			CountsByState: map[string]uint32{"open": 2, "dismissed": 1}, LastScan: 1_700_000_000, LastLookup: ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_PARTIAL,
		}, nil
	}
	p := runTrust(t, trustReq{path: "/user2/repo1/asc/deps", user: 2, access: perm_model.AccessModeAdmin}, withLocale(AscDeps))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "deps", p.body)
	f.depFindings = func(r *ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/deps", user: 2, access: perm_model.AccessModeAdmin}, withLocale(AscDeps))
	writeSnapshot(t, dir, "deps-never", p.body)
	f.depUpdates = func(r *ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error) {
		return &ascv1.ListDepUpdatesResponse{
			Updates: []*ascv1.DepUpdate{
				{Ecosystem: "crates.io", Name: "tokio", Current: "1.38.0", Latest: "1.41.1", Target: "1.41.1", Kind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, Fixes: []string{"RUSTSEC-2025-0044"}},
				{Ecosystem: "npm", Name: "vite", Current: "5.2.8", Latest: "5.2.14", Target: "5.2.14", Kind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_PATCH, Fixes: []string{"GHSA-q7x4-2m8r-vh5c"}},
				{Ecosystem: "npm", Name: "react", Current: "18.3.0", Latest: "19.0.0", Target: "19.0.0", Kind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MAJOR},
			},
			Packages: 214, UnknownPackages: 2,
		}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/deps?view=updates", user: 2, access: perm_model.AccessModeAdmin}, withLocale(AscDeps))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "deps-updates", p.body)
	f.depConfig = func(*ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error) {
		return &ascv1.GetDepsConfigResponse{
			Source: "file", Text: "schedule = \"0 6 * * 1\"\n", Supported: []string{"cargo", "npm", "pip", "go", "maven"},
			Config: &ascv1.DepsConfig{Schedule: "0 6 * * 1", SeverityThreshold: ascv1.DepSeverity_DEP_SEVERITY_MODERATE, MaxKind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, MaxProposals: 5, Grouping: "none",
				Ignore: []*ascv1.DepIgnore{{Name: "derivative", Reason: "Accepted risk until Dec 1"}}},
		}, nil
	}
	p = runTrust(t, trustReq{path: "/user2/repo1/asc/deps?view=config", user: 2, access: perm_model.AccessModeAdmin}, withLocale(AscDeps))
	require.Equal(t, 200, p.status, p.body)
	writeSnapshot(t, dir, "deps-config", p.body)
}
