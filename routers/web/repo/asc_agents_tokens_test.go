// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

const (
	testTokenID     = "at_AAAAAAAAAAAAAAAAAAAA"
	testTokenSecret = "ascat_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	testTokensPath  = testRunsPath + "/tokens"
)

func sampleToken(id string, state ascv1.AgentTokenState) *ascv1.AgentToken {
	return &ascv1.AgentToken{
		Id:               id,
		Repo:             "user2/repo1",
		Name:             "my laptop",
		Agent:            "coder",
		Paths:            []string{"src/**", "tests/**"},
		MaxChangesPerRun: 8,
		State:            state,
		CreatedBy:        "user2",
		Created:          1_700_000_000,
		ExpiresAt:        1_700_003_600,
		LastUsed:         1_700_000_500,
		Limits:           &ascv1.AgentBudgetLimits{MaxChanges: 20, MaxTokens: 1_000_000, MaxCostMicroUsd: 5_000_000},
		Usage:            &ascv1.AgentBudgetUsage{Changes: 5, Tokens: 900_000, CostMicroUsd: 1_000_000},
		SecretHint:       "wxyz",
	}
}

func TestAscAgentsTokensPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)

	revoked := sampleToken("at_BBBBBBBBBBBBBBBBBBBB", ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED)
	revoked.Name = "old <b>token</b>"
	revoked.RevokedAt = 1_700_000_900
	revoked.Created = 1_600_000_000
	hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{revoked, sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)}})
	hub.ok("Policy.GetSource", &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Agents: &ascv1.PolicyAgents{DefaultScope: []string{"src/**"}}}})

	p := runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, AscAgentsTokens)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Equal(t, "no-store", p.resp.Header().Get("Cache-Control"))

	calls := hub.callsOf("AgentTokens.ListTokens")
	require.Len(t, calls, 1)
	isUserCredential(t, calls[0])
	assert.Equal(t, "user2/repo1", calls[0].req.(*ascv1.ListAgentTokensRequest).GetRepo().GetName())

	// Active tokens first. The list shows what the token may do and how much it has used, never a secret.
	iActive, iRevoked := strings.Index(p.body, testTokenID), strings.Index(p.body, "at_BBBB")
	assert.True(t, iActive > 0 && iRevoked > iActive, "active first")
	assert.Contains(t, p.body, "my laptop")
	assert.Contains(t, p.body, "asc_review.tokens_ends_in <code>wxyz</code>")
	assert.Contains(t, p.body, "<code>src/**</code>, <code>tests/**</code>")
	assert.Contains(t, p.body, "asc_review.policy_max_changes")
	assert.Contains(t, p.body, `data-state="active"`)
	assert.Contains(t, p.body, `data-state="revoked"`)
	assert.Contains(t, p.body, "old &lt;b&gt;token&lt;/b&gt;")
	assert.Regexp(t, `<progress class="asc-meter-bar is-warn" max="100" value="90" aria-label="asc_review.meter_tokens">`, p.body)
	assert.Contains(t, p.body, "/tokens/"+testTokenID+"/revoke")
	assert.NotContains(t, p.body, "/tokens/at_BBBBBBBBBBBBBBBBBBBB/revoke", "a revoked token cannot be revoked again")
	assert.NotContains(t, p.body, "asc-agent-secret", "no secret outside the answer that made it")
	noCallTokens(t)

	t.Run("a hint that is not a short slug is not shown", func(t *testing.T) {
		tok := sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)
		tok.SecretHint = testTokenSecret
		hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{tok}})
		p := runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, AscAgentsTokens)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, testTokenSecret)
		assert.NotContains(t, p.body, "ends in")
	})

	t.Run("a token of another repository, or without a proper id, spoils the answer", func(t *testing.T) {
		stranger := sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)
		stranger.Repo = "user3/secret"
		stranger.Name = "STRANGER-TOKEN"
		hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE), stranger}})
		p := runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, AscAgentsTokens)
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, "STRANGER-TOKEN")

		bad := sampleToken("at_../x", ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)
		hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{bad}})
		p = runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, AscAgentsTokens)
		assert.Equal(t, http.StatusBadGateway, p.status)
	})

	t.Run("agents are off", func(t *testing.T) {
		hub.fail("AgentTokens.ListTokens", withDetail(t, codes.FailedPrecondition, "off", "AGENTS_DISABLED"))
		p := runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, AscAgentsTokens)
		assert.Equal(t, http.StatusOK, p.status)
		assert.Contains(t, p.body, "Agents are not turned on for this repository")
	})
}

func newTokenForm() url.Values {
	return url.Values{
		"name":                {"my laptop"},
		"agent":               {"coder"},
		"paths":               {"src/**\ntests/**"},
		"ttl":                 {"86400"},
		"max_changes_per_run": {"8"},
		"op":                  {"01890000-0000-7000-8000-000000000001"},
	}
}

func TestAscAgentsTokenCreate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	made := sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)
	hub.ok("AgentTokens.CreateToken", &ascv1.CreateAgentTokenResponse{Token: made, Secret: testTokenSecret})
	hub.ok("AgentTokens.ListTokens", &ascv1.ListAgentTokensResponse{Tokens: []*ascv1.AgentToken{made}})

	post := func(form url.Values) agentsPage {
		return runAgents(t, agentsReq{method: "POST", path: testTokensPath, user: agentsOwner, features: true, form: form}, AscAgentsTokenCreate)
	}

	t.Run("makes a token and shows its secret once", func(t *testing.T) {
		form := newTokenForm()
		form.Set(ascagentui.BudgetFields.Changes, "20")
		form.Set(ascagentui.BudgetFields.Tokens, "1000000")
		form.Set(ascagentui.BudgetFields.CostUSD, "5")
		p := post(form)
		require.Equal(t, http.StatusOK, p.status, p.body)
		assert.Equal(t, "no-store", p.resp.Header().Get("Cache-Control"))
		assert.Empty(t, p.loc, "no redirect: the secret never goes through a URL or a flash message")
		assert.Equal(t, 1, strings.Count(p.body, testTokenSecret), "once, in a read-only field")
		assert.Contains(t, p.body, "asc_review.tokens_secret_help")
		assert.Contains(t, p.body, `data-testid="asc-agent-secret"`)
		assert.Empty(t, p.ctx.Flash.SuccessMsg)
		assert.Empty(t, p.ctx.Flash.InfoMsg)

		calls := hub.callsOf("AgentTokens.CreateToken")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		assert.Equal(t, "01890000-0000-7000-8000-000000000001", calls[0].opID)
		req := calls[0].req.(*ascv1.CreateAgentTokenRequest)
		assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
		assert.Equal(t, "my laptop", req.GetName())
		assert.Equal(t, "coder", req.GetAgent())
		assert.Equal(t, []string{"src/**", "tests/**"}, req.GetPaths())
		assert.EqualValues(t, 86400, req.GetTtlSeconds())
		assert.EqualValues(t, 8, req.GetMaxChangesPerRun())
		assert.EqualValues(t, 20, req.GetLimits().GetMaxChanges())
		assert.EqualValues(t, 1_000_000, req.GetLimits().GetMaxTokens())
		assert.EqualValues(t, 5_000_000, req.GetLimits().GetMaxCostMicroUsd())
		noCallTokens(t)

		// The next view of the page has no secret.
		again := runAgents(t, agentsReq{path: testTokensPath, user: agentsOwner, features: true}, AscAgentsTokens)
		assert.NotContains(t, again.body, testTokenSecret)
	})

	invalid := map[string]struct {
		mutate func(url.Values)
		want   string
	}{
		"no name":          {func(v url.Values) { v.Set("name", " ") }, "Name the token"},
		"long name":        {func(v url.Values) { v.Set("name", strings.Repeat("n", 65)) }, "Name the token"},
		"name with eol":    {func(v url.Values) { v.Set("name", "a\nb") }, "Name the token"},
		"bad agent":        {func(v url.Values) { v.Set("agent", "Agent One") }, "agent name is 1 to 40"},
		"escaping path":    {func(v url.Values) { v.Set("paths", "../../x") }, "relative path patterns"},
		"ttl not offered":  {func(v url.Values) { v.Set("ttl", "31536000") }, "how long the token lasts"},
		"ttl not a number": {func(v url.Values) { v.Set("ttl", "forever") }, "how long the token lasts"},
		"ttl empty":        {func(v url.Values) { v.Del("ttl") }, "how long the token lasts"},
		"bad changes":      {func(v url.Values) { v.Set("max_changes_per_run", "ten") }, "whole number"},
		"too many changes": {func(v url.Values) { v.Set("max_changes_per_run", "10001") }, "whole number"},
		"bad cost":         {func(v url.Values) { v.Set(ascagentui.BudgetFields.CostUSD, "-5") }, "dollars such as 2.50"},
	}
	for name, tc := range invalid {
		t.Run("refused: "+name, func(t *testing.T) {
			before := len(hub.callsOf("AgentTokens.CreateToken"))
			form := newTokenForm()
			tc.mutate(form)
			p := post(form)
			assert.Equal(t, http.StatusBadRequest, p.status, p.body)
			assert.Contains(t, p.body, tc.want)
			assert.NotContains(t, p.body, testTokenSecret)
			assert.Len(t, hub.callsOf("AgentTokens.CreateToken"), before)
		})
	}

	t.Run("the offered lifetimes are the only ones", func(t *testing.T) {
		for _, ttl := range ascAgentTokenTTLs {
			assert.LessOrEqual(t, ttl.Seconds, uint32(90*86400), "asc accepts at most 90 days")
			assert.NotZero(t, ttl.Seconds, "a token always expires")
		}
	})

	t.Run("asc refuses the scope", func(t *testing.T) {
		hub.fail("AgentTokens.CreateToken", withDetail(t, codes.PermissionDenied, "scope: "+testPlantedKey, "AGENT_SCOPE_DENIED", "infra/**"))
		p := post(newTokenForm())
		assert.Equal(t, http.StatusForbidden, p.status)
		assert.Contains(t, p.body, "does not let an agent write those paths")
		assert.Contains(t, p.body, "infra/**")
		assert.NotContains(t, p.body, testPlantedKey)
		assert.Contains(t, p.body, `value="my laptop"`, "the form is kept")
	})

	t.Run("a secret that is not a secret is not shown, and not logged into the page", func(t *testing.T) {
		for _, bad := range []string{"", "ascat_short", "ascat_" + strings.Repeat("<", 43), "tok_" + strings.Repeat("a", 43)} {
			hub.ok("AgentTokens.CreateToken", &ascv1.CreateAgentTokenResponse{Token: made, Secret: bad})
			p := post(newTokenForm())
			assert.Equal(t, http.StatusBadGateway, p.status, bad)
			assert.Contains(t, p.body, "its answer was not usable")
			if bad != "" {
				assert.NotContains(t, p.body, bad)
			}
		}
	})

	t.Run("a token of another repository", func(t *testing.T) {
		stranger := sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)
		stranger.Repo = "user3/secret"
		hub.ok("AgentTokens.CreateToken", &ascv1.CreateAgentTokenResponse{Token: stranger, Secret: testTokenSecret})
		p := post(newTokenForm())
		assert.Equal(t, http.StatusBadGateway, p.status)
		assert.NotContains(t, p.body, testTokenSecret)
	})
}

func TestAscAgentsTokenRevokeAndBudget(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	revoked := sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED)
	hub.ok("AgentTokens.RevokeToken", &ascv1.RevokeAgentTokenResponse{Token: revoked})
	hub.ok("AgentTokens.SetBudget", &ascv1.SetAgentTokenBudgetResponse{Token: sampleToken(testTokenID, ascv1.AgentTokenState_AGENT_TOKEN_STATE_ACTIVE)})
	id := map[string]string{":id": testTokenID}

	t.Run("revoke", func(t *testing.T) {
		p := runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/" + testTokenID + "/revoke", user: agentsOwner, features: true, params: id, form: url.Values{"reason": {"laptop lost"}}}, AscAgentsTokenRevoke)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, testTokensPath, p.loc)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "revoked")
		calls := hub.callsOf("AgentTokens.RevokeToken")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		req := calls[0].req.(*ascv1.RevokeAgentTokenRequest)
		assert.Equal(t, testTokenID, req.GetId())
		assert.Equal(t, "laptop lost", req.GetReason())
		noCallTokens(t)
	})

	t.Run("budget", func(t *testing.T) {
		form := url.Values{ascagentui.BudgetFields.Changes: {"12"}, ascagentui.BudgetFields.Tokens: {"500000"}, ascagentui.BudgetFields.CostUSD: {"1.5"}}
		p := runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/" + testTokenID + "/budget", user: agentsOwner, features: true, params: id, form: form}, AscAgentsTokenBudget)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "budget")
		calls := hub.callsOf("AgentTokens.SetBudget")
		require.Len(t, calls, 1)
		isUserCredential(t, calls[0])
		req := calls[0].req.(*ascv1.SetAgentTokenBudgetRequest)
		assert.Equal(t, testTokenID, req.GetId())
		assert.EqualValues(t, 12, req.GetLimits().GetMaxChanges())
		assert.EqualValues(t, 500_000, req.GetLimits().GetMaxTokens())
		assert.EqualValues(t, 1_500_000, req.GetLimits().GetMaxCostMicroUsd())
		noCallTokens(t)
	})

	t.Run("a bad budget does not reach asc", func(t *testing.T) {
		before := len(hub.callsOf("AgentTokens.SetBudget"))
		p := runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/" + testTokenID + "/budget", user: agentsOwner, features: true, params: id, form: url.Values{ascagentui.BudgetFields.Changes: {"-3"}}}, AscAgentsTokenBudget)
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.Contains(t, p.ctx.Flash.ErrorMsg, "not a valid number")
		assert.Len(t, hub.callsOf("AgentTokens.SetBudget"), before)
	})

	t.Run("token ids have a shape", func(t *testing.T) {
		before := hub.total()
		for _, bad := range []string{"x", "at_short", "at_" + strings.Repeat("A", 21), "../x", "at_AAAAAAAAAAAAAAAAAAA\n"} {
			bp := map[string]string{":id": bad}
			assert.Equal(t, http.StatusNotFound, runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/x/revoke", user: agentsOwner, features: true, params: bp}, AscAgentsTokenRevoke).status, bad)
			assert.Equal(t, http.StatusNotFound, runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/x/budget", user: agentsOwner, features: true, params: bp}, AscAgentsTokenBudget).status, bad)
		}
		assert.Equal(t, before, hub.total())
	})

	t.Run("the answer must be the token asked for", func(t *testing.T) {
		hub.ok("AgentTokens.RevokeToken", &ascv1.RevokeAgentTokenResponse{Token: sampleToken("at_CCCCCCCCCCCCCCCCCCCC", ascv1.AgentTokenState_AGENT_TOKEN_STATE_REVOKED)})
		p := runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/x/revoke", user: agentsOwner, features: true, params: id}, AscAgentsTokenRevoke)
		assert.Equal(t, http.StatusSeeOther, p.status)
		assert.NotEmpty(t, p.ctx.Flash.ErrorMsg)
		assert.Empty(t, p.ctx.Flash.SuccessMsg)
	})

	t.Run("an admin of another repository is not an admin here", func(t *testing.T) {
		before := hub.total()
		p := runAgents(t, agentsReq{method: "POST", path: testTokensPath + "/x/revoke", user: agentsReader, features: true, params: id}, AscAgentsTokenRevoke)
		assert.Equal(t, http.StatusNotFound, p.status)
		assert.Equal(t, before, hub.total())
	})
}
