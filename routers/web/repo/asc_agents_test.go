// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// rloChar is a right-to-left override, written as an escape so the source holds none.
var rloChar = string(rune(0x202E))

const (
	testRunsPath    = "/user2/repo1/asc/agents"
	testPlantedKey  = "sk-planted-secret-0123456789"
	testInternalTok = ascRPCTestToken
)

func sampleRun(id uint64, state ascv1.AgentRunState) *ascv1.AgentRun {
	return &ascv1.AgentRun{
		Id:        id,
		Repo:      "user2/repo1",
		Agent:     "coder",
		Title:     "Add retries to the billing client",
		State:     state,
		Target:    "master",
		Model:     "claude-sonnet-5-5",
		CreatedBy: "user2",
		Created:   1_700_000_000,
		Started:   1_700_000_010,
		Limits:    &ascv1.AgentBudgetLimits{MaxChanges: 8, MaxTokens: 200_000, MaxRuntimeSeconds: 1200},
		Usage:     &ascv1.AgentBudgetUsage{Changes: 4, Tokens: 116_000, CostMicroUsd: 1_250_000, RuntimeSeconds: 360},
	}
}

func idStr(n uint64) string { return fmt.Sprint(n) }

func changeID(b byte) []byte {
	out := make([]byte, 16)
	for i := range out {
		out[i] = b + byte(i)
	}
	return out
}

func noCallTokens(t *testing.T) {
	t.Helper()
	n, err := db.GetEngine(db.DefaultContext).Where("name LIKE ?", "asc-hub-call-%").Count(&auth_model.AccessToken{})
	require.NoError(t, err)
	assert.Zero(t, n, "a one-use call token is left behind")
}

func isUserCredential(t *testing.T, c fakeCall) {
	t.Helper()
	bearer, ok := strings.CutPrefix(c.auth, "Bearer ")
	require.True(t, ok, "%s carries no bearer credential", c.method)
	assert.NotEqual(t, testInternalTok, bearer, "%s: a person's action must not use the internal token", c.method)
	assert.Len(t, bearer, 40, "%s: a one-use access token", c.method)
}

func isSystemCredential(t *testing.T, c fakeCall) {
	t.Helper()
	assert.Equal(t, "Bearer "+testInternalTok, c.auth, "%s: reads use the internal token after the hub's own checks", c.method)
}

func withDetail(t *testing.T, c codes.Code, msg, code string, paths ...string) error {
	t.Helper()
	st, err := status.New(c, msg).WithDetails(&ascv1.ErrorDetail{Code: code, Paths: paths})
	require.NoError(t, err)
	return st.Err()
}

// ---------------------------------------------------------------------------------------------- the gate

func TestAscAgentsGate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{})
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: sampleRun(1, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)})
	hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{})
	hub.ok("Workflows.ListDefinitions", &ascv1.ListWorkflowDefinitionsResponse{})

	type route struct {
		name    string
		method  string
		path    string
		handler func(*context.Context)
		params  map[string]string
	}
	id := map[string]string{":id": "1"}
	reads := []route{
		{"runs", "GET", testRunsPath, AscAgents, nil},
		{"run", "GET", testRunsPath + "/runs/1", AscAgentsRun, id},
		{"events", "GET", testRunsPath + "/runs/1/events", AscAgentsEvents, id},
	}
	writes := []route{
		{"new form", "GET", testRunsPath + "/new", AscAgentsNew, nil},
		{"create", "POST", testRunsPath + "/new", AscAgentsCreate, nil},
		{"cancel", "POST", testRunsPath + "/runs/1/cancel", AscAgentsCancel, id},
		{"input", "POST", testRunsPath + "/runs/1/input", AscAgentsInput, id},
	}
	admins := []route{
		{"tokens", "GET", testRunsPath + "/tokens", AscAgentsTokens, nil},
		{"token create", "POST", testRunsPath + "/tokens", AscAgentsTokenCreate, nil},
		{"token revoke", "POST", testRunsPath + "/tokens/at_AAAAAAAAAAAAAAAAAAAA/revoke", AscAgentsTokenRevoke, map[string]string{":id": "at_AAAAAAAAAAAAAAAAAAAA"}},
		{"token budget", "POST", testRunsPath + "/tokens/at_AAAAAAAAAAAAAAAAAAAA/budget", AscAgentsTokenBudget, map[string]string{":id": "at_AAAAAAAAAAAAAAAAAAAA"}},
	}
	all := append(append(append([]route{}, reads...), writes...), admins...)

	for _, r := range all {
		t.Run("signed out: "+r.name, func(t *testing.T) {
			before := hub.total()
			p := runAgents(t, agentsReq{method: r.method, path: r.path, params: r.params}, r.handler)
			assert.Equal(t, http.StatusNotFound, p.status)
			assert.Equal(t, before, hub.total(), "asc is not called for a stranger")
		})
		t.Run("switch off: "+r.name, func(t *testing.T) {
			before := hub.total()
			p := runAgents(t, agentsReq{method: r.method, path: r.path, user: agentsOwner, features: false, params: r.params}, r.handler)
			assert.Equal(t, http.StatusNotFound, p.status, "agent features are off by default: the page does not exist")
			assert.Equal(t, before, hub.total())
		})
	}

	t.Run("asc is off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		assert.Equal(t, http.StatusNotFound, p.status)
	})

	t.Run("a repository that is not in asc", func(t *testing.T) {
		setStorageBackend(t, 1, "git")
		defer setStorageBackend(t, 1, "asc")
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		assert.Equal(t, http.StatusNotFound, p.status)
	})

	t.Run("a reader may look but not act", func(t *testing.T) {
		for _, r := range reads {
			p := runAgents(t, agentsReq{method: r.method, path: r.path, user: agentsReader, features: true, params: r.params}, r.handler)
			assert.Equal(t, http.StatusOK, p.status, r.name)
		}
		for _, r := range writes {
			before := hub.total()
			p := runAgents(t, agentsReq{method: r.method, path: r.path, user: agentsReader, features: true, params: r.params, form: url.Values{"task": {"x"}, "text": {"x"}}}, r.handler)
			assert.Equal(t, http.StatusForbidden, p.status, r.name)
			assert.Equal(t, before, hub.total(), "%s: nothing reaches asc", r.name)
		}
		for _, r := range admins {
			before := hub.total()
			p := runAgents(t, agentsReq{method: r.method, path: r.path, user: agentsReader, features: true, params: r.params}, r.handler)
			assert.Equal(t, http.StatusNotFound, p.status, "%s: tokens are for admins, others do not learn they exist", r.name)
			assert.Equal(t, before, hub.total())
		}
	})

	t.Run("an archived repository takes no new work", func(t *testing.T) {
		_, err := db.GetEngine(db.DefaultContext).ID(1).Cols("is_archived").Update(&repo_model.Repository{IsArchived: true})
		require.NoError(t, err)
		defer func() {
			_, err := db.GetEngine(db.DefaultContext).ID(1).Cols("is_archived").Update(&repo_model.Repository{IsArchived: false})
			require.NoError(t, err)
		}()
		before := hub.total()
		for _, r := range []route{writes[0], writes[1]} {
			p := runAgents(t, agentsReq{method: r.method, path: r.path, user: agentsOwner, features: true, form: url.Values{"task": {"x"}}}, r.handler)
			assert.Equal(t, http.StatusForbidden, p.status, r.name)
		}
		assert.Equal(t, before, hub.total())
		// Looking at it still works.
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		assert.Equal(t, http.StatusOK, p.status)
	})
	noCallTokens(t)
}

// ---------------------------------------------------------------------------------------------- the list

func TestAscAgentsList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)

	evil := sampleRun(9, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
	evil.Title = "<script>alert(1)</script> fix\x1b[31m it"
	evil.Agent = "coder"
	failed := sampleRun(8, ascv1.AgentRunState_AGENT_RUN_STATE_FAILED)
	failed.FailureCode = "BUDGET_EXCEEDED"
	failed.FailureMessage = "the " + rloChar + "limit was reached"
	hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{evil, failed}, NextPageToken: "tok_A-b=1"})

	p := runAgents(t, agentsReq{path: testRunsPath + "?state=active&agent=coder&mine=1", user: agentsOwner, features: true}, AscAgents)
	require.Equal(t, http.StatusOK, p.status, p.body)

	calls := hub.callsOf("Agents.ListRuns")
	require.Len(t, calls, 1)
	isSystemCredential(t, calls[0])
	req := calls[0].req.(*ascv1.ListAgentRunsRequest)
	assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
	assert.Equal(t, "coder", req.GetAgent())
	assert.Equal(t, "user2", req.GetCreatedBy(), "the signed-in person, from the session")
	assert.ElementsMatch(t, []ascv1.AgentRunState{
		ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
	}, req.GetStates())
	assert.EqualValues(t, 30, req.GetLimit())

	assert.Contains(t, p.body, "&lt;script&gt;alert(1)&lt;/script&gt; fix[31m it", "text is cleaned and escaped")
	assert.NotContains(t, p.body, "<script>alert(1)")
	assert.NotContains(t, p.body, "\x1b")
	assert.Contains(t, p.body, `href="/user2/repo1/asc/agents/runs/9"`)
	assert.Contains(t, p.body, `data-state="running"`)
	assert.Contains(t, p.body, `data-state="failed"`)
	assert.Contains(t, p.body, "the limit was reached")
	assert.NotContains(t, p.body, rloChar)
	assert.Contains(t, p.body, "agent <code>coder</code>")
	assert.Contains(t, p.body, "state=active")
	assert.Contains(t, p.body, "page=tok_A-b%3D1", "the next page keeps the filter")
	assert.Contains(t, p.body, ">New task<")

	t.Run("a reader sees the list but no new task button", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsReader, features: true}, AscAgents)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, ">New task<")
		assert.NotContains(t, p.body, "/tokens")
	})

	t.Run("a run of another repository is never shown", func(t *testing.T) {
		stranger := sampleRun(5, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
		stranger.Repo = "user3/secret"
		stranger.Title = "STRANGER-TITLE"
		hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{sampleRun(1, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING), stranger}})
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, "STRANGER-TITLE")
		assert.NotContains(t, p.body, "Add retries", "nothing of a bad answer is used")
	})

	t.Run("a next-page token that is not opaque is dropped", func(t *testing.T) {
		hub.ok("Agents.ListRuns", &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{sampleRun(1, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)}, NextPageToken: `"><script>`})
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, "Older runs")
	})

	t.Run("agents are off for the repository", func(t *testing.T) {
		hub.fail("Agents.ListRuns", withDetail(t, codes.FailedPrecondition, "agents are off: "+testPlantedKey, "AGENTS_DISABLED"))
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		assert.Equal(t, http.StatusOK, p.status, "a state to explain, not a fault")
		assert.Contains(t, p.body, "Agents are not turned on for this repository")
		assert.Contains(t, p.body, `data-code="AGENTS_DISABLED"`)
		assert.NotContains(t, p.body, testPlantedKey)
	})

	t.Run("a failing asc is a 502 and says nothing of why", func(t *testing.T) {
		hub.fail("Agents.ListRuns", status.Error(codes.Internal, "panic at /srv/asc/agents.rs:12 "+testPlantedKey))
		p := runAgents(t, agentsReq{path: testRunsPath, user: agentsOwner, features: true}, AscAgents)
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, testPlantedKey)
		assert.NotContains(t, p.body, "agents.rs")
	})

	t.Run("bad filters are refused before asc is called", func(t *testing.T) {
		before := hub.total()
		for _, q := range []string{"?state=bogus", "?agent=Not%20Valid", "?page=a%20b", "?agent=" + strings.Repeat("a", 41)} {
			p := runAgents(t, agentsReq{path: testRunsPath + q, user: agentsOwner, features: true}, AscAgents)
			assert.Equal(t, http.StatusBadRequest, p.status, q)
		}
		assert.Equal(t, before, hub.total())
	})
}

// ---------------------------------------------------------------------------------------------- the task form

func keyID(c byte) string { return "ak_" + strings.Repeat(string(c), 20) }

func TestAscAgentsNewForm(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)

	hub.on("AiProviders.ListKeys", func(m proto.Message) (proto.Message, error) {
		return &ascv1.ListAiKeysResponse{Keys: []*ascv1.AiKey{
			{Id: keyID('A'), Label: "Work <b>key</b>", Provider: ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, DefaultModel: "claude-sonnet-5-5", KeyHint: "…wxyz", HasKey: true, State: ascv1.AiKeyState_AI_KEY_STATE_ACTIVE},
			{Id: keyID('B'), Label: "Spare", Provider: ascv1.AiProvider_AI_PROVIDER_OPENAI, State: ascv1.AiKeyState_AI_KEY_STATE_UNTESTED},
			{Id: "ak_../../etc", Label: "Bad id", State: ascv1.AiKeyState_AI_KEY_STATE_ACTIVE},
			{Id: keyID('C'), Label: "Capped", Provider: ascv1.AiProvider_AI_PROVIDER_GOOGLE, State: ascv1.AiKeyState_AI_KEY_STATE_CAPPED},
		}}, nil
	})
	hub.ok("AiProviders.ListModels", &ascv1.ListAiModelsResponse{Models: []*ascv1.AiModel{{Id: "claude-sonnet-5-5"}, {Id: "claude haiku; rm -rf"}, {Id: "claude-haiku-5-5"}}})
	hub.ok("Policy.GetSource", &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Agents: &ascv1.PolicyAgents{
		DefaultScope: []string{"src/**", "tests/**"}, DeniedPaths: []string{"infra/**"}, MaxChangesPerRun: 8,
	}}})

	p := runAgents(t, agentsReq{path: testRunsPath + "/new", user: agentsOwner, features: true}, AscAgentsNew)
	require.Equal(t, http.StatusOK, p.status, p.body)

	lists := hub.callsOf("AiProviders.ListKeys")
	require.Len(t, lists, 1, "the owner is the person: one owner to list")
	isUserCredential(t, lists[0])
	owner := lists[0].req.(*ascv1.ListAiKeysRequest).GetOwner()
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_USER, owner.GetKind())
	assert.Equal(t, "user2", owner.GetName())
	models := hub.callsOf("AiProviders.ListModels")
	require.Len(t, models, 1, "only a usable key is asked for its models")
	assert.Equal(t, keyID('A'), models[0].req.(*ascv1.ListAiModelsRequest).GetId())
	isUserCredential(t, models[0])
	policy := hub.callsOf("Policy.GetSource")
	require.Len(t, policy, 1)
	isSystemCredential(t, policy[0])

	// The picker: the usable key can be chosen, the others are listed but cannot.
	assert.Contains(t, p.body, `value="`+keyID('A')+`"`)
	assert.Contains(t, p.body, "Work &lt;b&gt;key&lt;/b&gt; (Anthropic, user2)")
	assert.Regexp(t, `value="`+keyID('B')+`"[^>]*disabled`, p.body)
	assert.Contains(t, p.body, "not tested yet")
	assert.Regexp(t, `value="`+keyID('C')+`"[^>]*disabled`, p.body)
	assert.Contains(t, p.body, "monthly cap reached")
	assert.NotContains(t, p.body, "ak_../../etc")
	assert.NotContains(t, p.body, "…wxyz", "the hint of a key is not needed here")
	// Models per key reach the script as data; an invalid model name is dropped.
	assert.Contains(t, p.body, "data-asc-models=")
	assert.Contains(t, p.body, "claude-haiku-5-5")
	assert.NotContains(t, p.body, "rm -rf")
	// The policy panel.
	assert.Contains(t, p.body, "<code>src/**</code>, <code>tests/**</code>")
	assert.Contains(t, p.body, "<code>infra/**</code>")
	assert.Contains(t, p.body, "8 changes per run")
	assert.Contains(t, p.body, "Cannot</span> push or merge to trunk")
	// A fresh op id for the form.
	assert.Regexp(t, `name="op" value="[0-9a-f-]{36}"`, p.body)
	noCallTokens(t)

	t.Run("a person who may not list keys still gets a form", func(t *testing.T) {
		hub.fail("AiProviders.ListKeys", withDetail(t, codes.NotFound, "no such owner", ""))
		p := runAgents(t, agentsReq{path: testRunsPath + "/new", user: agentsOwner, features: true}, AscAgentsNew)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "The repository default")
		assert.Contains(t, p.body, "No AI key of yours or of the repository owner is listed")
	})
}

func newRunForm() url.Values {
	return url.Values{
		"title":  {"Retry policy"},
		"task":   {"Every outbound payment call should use the retry policy.\nDo not touch infra."},
		"agent":  {"coder"},
		"target": {"master"},
		"op":     {uuid.Must(uuid.NewV7()).String()},
	}
}

func TestAscAgentsCreate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	hub.ok("AiProviders.ListKeys", &ascv1.ListAiKeysResponse{})
	created := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED)
	hub.ok("Agents.CreateRun", &ascv1.CreateAgentRunResponse{Run: created})

	t.Run("starts a run as the person, once", func(t *testing.T) {
		form := newRunForm()
		form.Set("key", keyID('A'))
		form.Set("model", "claude-sonnet-5-5")
		form.Set("paths", "src/**\ntests/**,docs/*.md")
		form.Set(ascagentui.BudgetFields.Changes, "8")
		form.Set(ascagentui.BudgetFields.Tokens, "200000")
		form.Set(ascagentui.BudgetFields.CostUSD, "2.50")
		form.Set(ascagentui.BudgetFields.Paths, "40")
		form.Set(ascagentui.BudgetFields.Minutes, "20")
		p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: form}, AscAgentsCreate)
		require.Equal(t, http.StatusSeeOther, p.status, p.body)
		assert.Equal(t, "/user2/repo1/asc/agents/runs/7", p.loc)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "Run 7 started")

		calls := hub.callsOf("Agents.CreateRun")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.Equal(t, form.Get("op"), calls[0].opID, "a double submit is one run: asc answers a retried op id with the first answer")
		req := calls[0].req.(*ascv1.CreateAgentRunRequest)
		assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
		assert.Equal(t, "coder", req.GetAgent())
		assert.Equal(t, "Retry policy", req.GetTitle())
		assert.Equal(t, "Every outbound payment call should use the retry policy.\nDo not touch infra.", req.GetTask())
		assert.Equal(t, "master", req.GetTarget())
		assert.Equal(t, keyID('A'), req.GetAiKeyId())
		assert.Equal(t, "claude-sonnet-5-5", req.GetModel())
		assert.Equal(t, []string{"src/**", "tests/**", "docs/*.md"}, req.GetPaths())
		assert.EqualValues(t, 8, req.GetLimits().GetMaxChanges())
		assert.EqualValues(t, 200_000, req.GetLimits().GetMaxTokens())
		assert.EqualValues(t, 2_500_000, req.GetLimits().GetMaxCostMicroUsd())
		assert.EqualValues(t, 40, req.GetLimits().GetMaxPaths())
		assert.EqualValues(t, 1200, req.GetLimits().GetMaxRuntimeSeconds())
		assert.Empty(t, req.GetBase(), "the tip of the target, chosen by asc")
		noCallTokens(t)
	})

	t.Run("defaults: the coder agent, the default branch, the policy's limits", func(t *testing.T) {
		form := url.Values{"task": {"Do the thing"}}
		p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: form}, AscAgentsCreate)
		require.Equal(t, http.StatusSeeOther, p.status, p.body)
		calls := hub.callsOf("Agents.CreateRun")
		req := calls[len(calls)-1].req.(*ascv1.CreateAgentRunRequest)
		assert.Equal(t, "coder", req.GetAgent())
		assert.Equal(t, "master", req.GetTarget())
		assert.Zero(t, req.GetLimits().GetMaxChanges()+uint32(req.GetLimits().GetMaxTokens()))
		_, err := uuid.Parse(calls[len(calls)-1].opID)
		assert.NoError(t, err, "an op id is made when the form has none")
	})

	invalid := map[string]struct {
		mutate func(url.Values)
		want   string
	}{
		"no task":        {func(v url.Values) { v.Set("task", "  ") }, "Describe the task"},
		"task with nul":  {func(v url.Values) { v.Set("task", "a\x00b") }, "Describe the task"},
		"task too long":  {func(v url.Values) { v.Set("task", strings.Repeat("x", ascagentui.MaxTaskBytes+1)) }, "Describe the task"},
		"title with eol": {func(v url.Values) { v.Set("title", "a\nb") }, "title is at most 200"},
		"bad agent":      {func(v url.Values) { v.Set("agent", "Coder One") }, "agent name is 1 to 40"},
		"no such branch": {func(v url.Values) { v.Set("target", "no-such-branch") }, "Choose a branch"},
		"bad key":        {func(v url.Values) { v.Set("key", "ak_x") }, "Choose a key"},
		"bad model":      {func(v url.Values) { v.Set("model", "m; rm -rf /") }, "not a model name"},
		"escaping path":  {func(v url.Values) { v.Set("paths", "src/../../etc") }, "relative path patterns"},
		"absolute path":  {func(v url.Values) { v.Set("paths", "/etc/passwd") }, "relative path patterns"},
		"bad changes":    {func(v url.Values) { v.Set(ascagentui.BudgetFields.Changes, "-1") }, "whole number"},
		"bad cost":       {func(v url.Values) { v.Set(ascagentui.BudgetFields.CostUSD, "two dollars") }, "dollars such as 2.50"},
		"huge tokens":    {func(v url.Values) { v.Set(ascagentui.BudgetFields.Tokens, "99999999999999999999") }, "whole number"},
	}
	for name, tc := range invalid {
		t.Run("refused: "+name, func(t *testing.T) {
			before := len(hub.callsOf("Agents.CreateRun"))
			form := newRunForm()
			tc.mutate(form)
			p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: form}, AscAgentsCreate)
			assert.Equal(t, http.StatusBadRequest, p.status, p.body)
			assert.Contains(t, p.body, tc.want)
			assert.Contains(t, p.body, "Some fields need another look")
			assert.Len(t, hub.callsOf("Agents.CreateRun"), before, "nothing reaches asc")
			noCallTokens(t)
		})
	}

	t.Run("what the person typed is shown again, escaped", func(t *testing.T) {
		form := newRunForm()
		form.Set("task", "<img src=x onerror=alert(1)>")
		form.Set("agent", "Bad Agent")
		p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: form}, AscAgentsCreate)
		assert.Equal(t, http.StatusBadRequest, p.status)
		assert.Contains(t, p.body, "&lt;img src=x onerror=alert(1)&gt;")
		assert.NotContains(t, p.body, "<img src=x")
	})

	for code, tc := range map[string]struct {
		grpc codes.Code
		http int
		text string
	}{
		"AI_KEY_UNTESTED":    {codes.FailedPrecondition, http.StatusConflict, "Test that AI key first"},
		"AI_CAP_REACHED":     {codes.ResourceExhausted, http.StatusTooManyRequests, "monthly cap"},
		"AGENT_SCOPE_DENIED": {codes.PermissionDenied, http.StatusForbidden, "does not let an agent write those paths"},
		"BUDGET_EXCEEDED":    {codes.ResourceExhausted, http.StatusTooManyRequests, "budget is reached"},
		"AGENTS_DISABLED":    {codes.FailedPrecondition, http.StatusConflict, "Agents are not turned on"},
	} {
		t.Run("asc says "+code, func(t *testing.T) {
			hub.fail("Agents.CreateRun", withDetail(t, tc.grpc, "provider said: "+testPlantedKey, code, "infra/limits.tf"))
			p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: newRunForm()}, AscAgentsCreate)
			assert.Equal(t, tc.http, p.status, p.body)
			assert.Contains(t, p.body, tc.text)
			assert.NotContains(t, p.body, testPlantedKey, "asc's own text is never shown")
			assert.Contains(t, p.body, "Every outbound payment call", "the task is kept in the form")
			noCallTokens(t)
		})
	}

	t.Run("a run of another repository in the answer is refused", func(t *testing.T) {
		stranger := sampleRun(3, ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED)
		stranger.Repo = "user3/secret"
		hub.ok("Agents.CreateRun", &ascv1.CreateAgentRunResponse{Run: stranger})
		p := runAgents(t, agentsReq{method: "POST", path: testRunsPath + "/new", user: agentsOwner, features: true, form: newRunForm()}, AscAgentsCreate)
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.Empty(t, p.loc)
	})
}

// ---------------------------------------------------------------------------------------------- the run page

func ev(seq uint64, kind ascv1.AgentRunEventKind, text string) *ascv1.AgentRunEvent {
	return &ascv1.AgentRunEvent{RunId: 7, Seq: seq, Kind: kind, TimeMs: 1_700_000_000_000 + int64(seq)*1000, Text: text}
}

func TestAscAgentsRunPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)

	// Two of the three changes were submitted for review: one landed, one is waiting.
	c1, c2, c3 := changeID(10), changeID(40), changeID(70)
	merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	open := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, merged, ascagentui.ChangeText(c1), 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, open, ascagentui.ChangeText(c2), merged.ID))

	run := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
	run.Changes = [][]byte{c1, c2, c3}
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: run})
	tool := ev(2, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_CALL, "docs/retries.md")
	tool.Tool = "edit_file"
	chg := ev(3, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE, "")
	chg.Change = c2
	hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
		return []*ascv1.AgentRunEvent{
			ev(1, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "I will start with <b>the client</b>.\x1b[2J"+rloChar),
			tool, chg,
			ev(4, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_ERROR, "scope refused infra/limits.tf"),
		}, false
	}
	hub.ok("Policy.GetSource", &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Agents: &ascv1.PolicyAgents{DefaultScope: []string{"src/**"}}}})

	p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
	require.Equal(t, http.StatusOK, p.status, p.body)

	get := hub.callsOf("Agents.GetRun")
	require.Len(t, get, 1)
	isSystemCredential(t, get[0])
	assert.EqualValues(t, 7, get[0].req.(*ascv1.GetAgentRunRequest).GetId())
	assert.Equal(t, "user2/repo1", get[0].req.(*ascv1.GetAgentRunRequest).GetRepo().GetName())

	// The state, the log and the budget.
	assert.Contains(t, p.body, `data-state="running"`)
	assert.Contains(t, p.body, `data-last-seq="4"`)
	assert.Contains(t, p.body, `data-active="true"`)
	assert.Contains(t, p.body, `data-poll-url="/user2/repo1/asc/agents/runs/7/events"`)
	assert.Contains(t, p.body, "I will start with &lt;b&gt;the client&lt;/b&gt;.[2J")
	assert.NotContains(t, p.body, "<b>the client")
	assert.NotContains(t, p.body, "\x1b")
	assert.NotContains(t, p.body, rloChar)
	assert.Contains(t, p.body, `<code class="asc-log-tool">edit_file</code>`)
	assert.Contains(t, p.body, "scope refused infra/limits.tf")
	assert.Contains(t, p.body, `asc-log-row is-quiet" data-seq="1"`, "the agent's own words are quiet until asked for")
	assert.Regexp(t, `<progress class="asc-meter-bar" max="100" value="58" aria-label="Tokens">`, p.body)
	assert.Contains(t, p.body, "116k of 200k")
	assert.Contains(t, p.body, "$1.25 (no limit)")
	// The stack: bottom first, with where to review each change.
	assert.Contains(t, p.body, `href="/user2/repo1/pulls/2"`)
	assert.Contains(t, p.body, "Landed")
	assert.Contains(t, p.body, `href="/user2/repo1/pulls/3"`)
	assert.Contains(t, p.body, "Waiting for review")
	assert.Contains(t, p.body, "not submitted for review yet")
	assert.Contains(t, p.body, `class="asc-log-change" href="/user2/repo1/pulls/3">change `+ascagentui.ShortChange(ascagentui.ChangeText(c2)))
	// The person may stop and steer; the policy panel is there.
	assert.Contains(t, p.body, "/runs/7/cancel")
	assert.Contains(t, p.body, "/runs/7/input")
	assert.Contains(t, p.body, "<code>src/**</code>")
	assert.NotContains(t, p.body, "tw-hidden\" role=\"alert\" data-run-failure>.", "no failure text without a failure")

	t.Run("a reader sees the run but cannot stop or steer it", func(t *testing.T) {
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsReader, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "I will start with")
		assert.NotContains(t, p.body, "/runs/7/cancel")
		assert.NotContains(t, p.body, "/runs/7/input")
	})

	t.Run("a finished run is not followed", func(t *testing.T) {
		done := proto.Clone(run).(*ascv1.AgentRun)
		done.State = ascv1.AgentRunState_AGENT_RUN_STATE_FAILED
		done.FailureMessage = "the model <i>refused</i>"
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: done})
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, `data-active="false"`)
		assert.Contains(t, p.body, `data-done="true"`)
		assert.Contains(t, p.body, "the model &lt;i&gt;refused&lt;/i&gt;")
		assert.NotContains(t, p.body, "/runs/7/cancel")
	})

	t.Run("only the newest events are drawn", func(t *testing.T) {
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: run})
		hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
			var evs []*ascv1.AgentRunEvent
			for i := uint64(1); i <= 250; i++ {
				evs = append(evs, ev(i, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_RESULT, fmt.Sprintf("line-%d", i)))
			}
			return evs, false
		}
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
		require.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "50 earlier events are not shown")
		assert.Contains(t, p.body, ">line-250<")
		assert.NotContains(t, p.body, ">line-50<")
		assert.Contains(t, p.body, `data-last-seq="250"`)
	})

	t.Run("a bad answer is used for nothing", func(t *testing.T) {
		other := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
		other.Repo = "user3/secret"
		other.Title = "STRANGER-TITLE"
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: other})
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, "STRANGER-TITLE")

		wrongID := sampleRun(8, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: wrongID})
		p = runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
		assert.Equal(t, http.StatusBadGateway, p.status, "asked for run 7, answered with run 8")
	})

	t.Run("run ids are digits", func(t *testing.T) {
		before := hub.total()
		for _, bad := range []string{"abc", "0", "007", "-1", "1e3", "99999999999", "7 ", ""} {
			p := runAgents(t, agentsReq{path: testRunsPath + "/runs/x", user: agentsOwner, features: true, params: map[string]string{":id": bad}}, AscAgentsRun)
			assert.Equal(t, http.StatusNotFound, p.status, "id %q", bad)
		}
		assert.Equal(t, before, hub.total())
	})

	t.Run("an unknown run is a 404 with no word from asc", func(t *testing.T) {
		hub.fail("Agents.GetRun", status.Error(codes.NotFound, "no run 7 in secret/other "+testPlantedKey))
		p := runAgents(t, agentsReq{path: testRunsPath + "/runs/7", user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsRun)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.NotContains(t, p.body, testPlantedKey)
		assert.NotContains(t, p.body, "secret/other")
	})
}

// ---------------------------------------------------------------------------------------------- the feed

func decodeFeed(t *testing.T, body string) ascAgentFeedJSON {
	t.Helper()
	var f ascAgentFeedJSON
	require.NoError(t, json.Unmarshal([]byte(body), &f), body)
	return f
}

func TestAscAgentsEvents(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	running := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: running})
	all := []*ascv1.AgentRunEvent{
		ev(1, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "one"),
		ev(2, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_CALL, "two <b>"),
		ev(3, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_RESULT, "three\x1b[31m"),
	}
	hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) { return all, true }
	feed := func(query string) agentsPage {
		return runAgents(t, agentsReq{path: testRunsPath + "/runs/7/events" + query, user: agentsOwner, features: true, params: map[string]string{":id": "7"}}, AscAgentsEvents)
	}

	p := feed("?after=1")
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Equal(t, "no-store", p.resp.Header().Get("Cache-Control"))
	assert.Contains(t, p.resp.Header().Get("Content-Type"), "application/json")
	f := decodeFeed(t, p.body)
	require.Len(t, f.Events, 2, "only what is after the last sequence number the page has")
	assert.EqualValues(t, 2, f.Events[0].Seq)
	assert.Equal(t, "two <b>", f.Events[0].Text, "JSON carries the text; the script assigns it as text")
	assert.Equal(t, "three[31m", f.Events[1].Text, "control characters are gone")
	assert.EqualValues(t, 3, f.Last)
	assert.Equal(t, "running", f.State)
	assert.True(t, f.Active)
	assert.False(t, f.Done, "the stream is still open")
	assert.Equal(t, "Running", f.Label)
	require.NotEmpty(t, f.Meters)
	watch := hub.callsOf("Agents.WatchRun")
	require.NotEmpty(t, watch)
	assert.EqualValues(t, 1, watch[0].req.(*ascv1.WatchAgentRunRequest).GetAfterSeq())
	isSystemCredential(t, watch[0])

	t.Run("a quiet wait returns no events and keeps the cursor", func(t *testing.T) {
		p := feed("?after=3")
		require.Equal(t, http.StatusOK, p.status, p.body)
		f := decodeFeed(t, p.body)
		assert.Empty(t, f.Events)
		assert.EqualValues(t, 3, f.Last)
		assert.False(t, f.Done)
	})

	t.Run("a finished run whose stream ended is done", func(t *testing.T) {
		done := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: done})
		hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) { return all, false }
		f := decodeFeed(t, feed("?after=0").body)
		assert.True(t, f.Done)
		assert.False(t, f.Active)
		assert.Len(t, f.Events, 3)
		assert.Equal(t, "Done", f.Label)
	})

	t.Run("a finished run that still has events is not done yet", func(t *testing.T) {
		hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) { return all, true }
		f := decodeFeed(t, feed("?after=0").body)
		assert.False(t, f.Done, "asc has not closed the stream")
	})

	t.Run("the cursor must be a number", func(t *testing.T) {
		before := hub.total()
		for _, bad := range []string{"?after=abc", "?after=-1", "?after=1.5", "?after=99999999999999999999", "?after=1%00"} {
			p := feed(bad)
			assert.Equal(t, http.StatusBadRequest, p.status, bad)
		}
		assert.Equal(t, before, hub.total())
	})

	t.Run("events that are not this run's, or that go backwards, are a bad answer", func(t *testing.T) {
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: running})
		foreign := ev(5, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "FOREIGN-RUN")
		foreign.RunId = 99
		hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
			return []*ascv1.AgentRunEvent{ev(4, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "ok"), foreign}, false
		}
		p := feed("?after=3")
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, "FOREIGN-RUN")

		hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
			return []*ascv1.AgentRunEvent{ev(6, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "six"), ev(5, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE, "five")}, false
		}
		assert.Equal(t, http.StatusBadGateway, feed("?after=3").status, "sequence numbers must rise")
	})

	t.Run("a run of another repository", func(t *testing.T) {
		other := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)
		other.Repo = "user3/secret"
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: other})
		before := len(hub.callsOf("Agents.WatchRun"))
		assert.Equal(t, http.StatusBadGateway, feed("?after=0").status)
		assert.Len(t, hub.callsOf("Agents.WatchRun"), before, "its events are not even asked for")
	})

	t.Run("a change event links to its review page", func(t *testing.T) {
		c := changeID(40)
		open := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
		require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, open, ascagentui.ChangeText(c), 0))
		hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: running})
		chg := ev(1, ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE, "")
		chg.Change = c
		hub.watch = func(r *ascv1.WatchAgentRunRequest) ([]*ascv1.AgentRunEvent, bool) {
			return []*ascv1.AgentRunEvent{chg}, false
		}
		f := decodeFeed(t, feed("?after=0").body)
		require.Len(t, f.Events, 1)
		assert.Equal(t, "/user2/repo1/pulls/3", f.Events[0].ChangeURL)
		assert.Equal(t, ascagentui.ShortChange(ascagentui.ChangeText(c)), f.Events[0].Change)
	})
}

// ---------------------------------------------------------------------------------------------- stop and steer

func TestAscAgentsCancelAndInput(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	stopped := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_CANCELLED)
	hub.ok("Agents.CancelRun", &ascv1.CancelAgentRunResponse{Run: stopped})
	hub.ok("Agents.SendInput", &ascv1.SendAgentInputResponse{Run: sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)})
	id := map[string]string{":id": "7"}
	back := "/user2/repo1/asc/agents/runs/7"

	t.Run("stop", func(t *testing.T) {
		p := runAgents(t, agentsReq{method: "POST", path: back + "/cancel", user: agentsOwner, features: true, params: id, form: url.Values{"reason": {"wrong branch"}}}, AscAgentsCancel)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, back, p.loc)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "stopped")
		calls := hub.callsOf("Agents.CancelRun")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.NotEmpty(t, calls[0].opID)
		req := calls[0].req.(*ascv1.CancelAgentRunRequest)
		assert.EqualValues(t, 7, req.GetId())
		assert.Equal(t, "wrong branch", req.GetReason())
		assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
		noCallTokens(t)
	})

	t.Run("the reason is one short line", func(t *testing.T) {
		before := len(hub.callsOf("Agents.CancelRun"))
		for _, bad := range []string{"a\nb", strings.Repeat("x", 257)} {
			p := runAgents(t, agentsReq{method: "POST", path: back + "/cancel", user: agentsOwner, features: true, params: id, form: url.Values{"reason": {bad}}}, AscAgentsCancel)
			assert.Equal(t, http.StatusSeeOther, p.status)
			assert.Contains(t, p.ctx.Flash.ErrorMsg, "reason")
		}
		assert.Len(t, hub.callsOf("Agents.CancelRun"), before)
	})

	t.Run("a run that already ended", func(t *testing.T) {
		hub.fail("Agents.CancelRun", withDetail(t, codes.FailedPrecondition, "run 7 is "+testPlantedKey, "RUN_NOT_ACTIVE"))
		p := runAgents(t, agentsReq{method: "POST", path: back + "/cancel", user: agentsOwner, features: true, params: id}, AscAgentsCancel)
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, "That run has already finished.", p.ctx.Flash.ErrorMsg)
		assert.NotContains(t, p.ctx.Flash.ErrorMsg, testPlantedKey)
		noCallTokens(t)
	})

	t.Run("steer", func(t *testing.T) {
		p := runAgents(t, agentsReq{method: "POST", path: back + "/input", user: agentsOwner, features: true, params: id, form: url.Values{"text": {"  also add a metric for retry exhaustion  "}}}, AscAgentsInput)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, back, p.loc)
		calls := hub.callsOf("Agents.SendInput")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.Equal(t, "also add a metric for retry exhaustion", calls[0].req.(*ascv1.SendAgentInputRequest).GetText())
		noCallTokens(t)
	})

	t.Run("a message is text of 1 to 16 KiB", func(t *testing.T) {
		before := len(hub.callsOf("Agents.SendInput"))
		for _, bad := range []string{"", "   ", "a\x00b", strings.Repeat("x", ascagentui.MaxInputBytes+1)} {
			p := runAgents(t, agentsReq{method: "POST", path: back + "/input", user: agentsOwner, features: true, params: id, form: url.Values{"text": {bad}}}, AscAgentsInput)
			assert.Equal(t, http.StatusSeeOther, p.status)
			assert.Contains(t, p.ctx.Flash.ErrorMsg, "message")
		}
		assert.Len(t, hub.callsOf("Agents.SendInput"), before)
	})

	t.Run("the answer must be this run", func(t *testing.T) {
		hub.ok("Agents.SendInput", &ascv1.SendAgentInputResponse{Run: sampleRun(8, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING)})
		p := runAgents(t, agentsReq{method: "POST", path: back + "/input", user: agentsOwner, features: true, params: id, form: url.Values{"text": {"hi"}}}, AscAgentsInput)
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
		assert.Empty(t, p.ctx.Flash.SuccessMsg)
	})

	t.Run("run ids are digits", func(t *testing.T) {
		before := hub.total()
		for _, bad := range []string{"x", "0", "07"} {
			assert.Equal(t, http.StatusNotFound, runAgents(t, agentsReq{method: "POST", path: back, user: agentsOwner, features: true, params: map[string]string{":id": bad}}, AscAgentsCancel).status)
			assert.Equal(t, http.StatusNotFound, runAgents(t, agentsReq{method: "POST", path: back, user: agentsOwner, features: true, params: map[string]string{":id": bad}}, AscAgentsInput).status)
		}
		assert.Equal(t, before, hub.total())
	})
}
