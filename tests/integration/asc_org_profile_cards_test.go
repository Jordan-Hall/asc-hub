// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type orgCardsAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedOrgSettingsServer
	mu    sync.Mutex
	merge *ascv1.MergeDefaults
}

func (*orgCardsAsc) Version(context.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeatureOrgSettings}}, nil
}

func (f *orgCardsAsc) GetOrgSettings(context.Context, *ascv1.GetOrgSettingsRequest) (*ascv1.GetOrgSettingsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &ascv1.GetOrgSettingsResponse{Agents: &ascv1.OrgAgentPolicy{AllowAgents: true}, Merge: f.merge, InstanceAllowsAgents: true}, nil
}

func (*orgCardsAsc) ListAgents(context.Context, *ascv1.ListOrgAgentsRequest) (*ascv1.ListOrgAgentsResponse, error) {
	return &ascv1.ListOrgAgentsResponse{Agents: []*ascv1.OrgAgent{{Name: "release-bot", DisplayName: "Release bot", RunBy: "user2", State: ascv1.OrgAgentState_ORG_AGENT_STATE_PAUSED}}}, nil
}

type orgCardsRuns struct {
	ascv1.UnimplementedAgentsServer
}

func (orgCardsRuns) SummarizeAgents(_ context.Context, req *ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
	resp := &ascv1.SummarizeAgentsResponse{}
	for i, r := range req.GetRepos() {
		if r.GetName() == "org3/repo3" {
			resp.Agents = append(resp.Agents, &ascv1.AgentSummary{Agent: "review", Runs: 4, Active: 1, Places: []*ascv1.AgentPlace{{Repo: uint32(i), Runs: 4, Active: 1}}})
		}
	}
	return resp, nil
}

// The organization page has an Agents card and, for a member, a Merge policy card read from asc; its head counts the
// agents.
func TestTheOrgPageHasTheAgentsAndMergePolicyCards(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef"))()
	f := &orgCardsAsc{merge: &ascv1.MergeDefaults{RequireApprovals: 2, RequireChecks: []string{"build", "lint"}, RequireSigned: true, Strategy: "rebase", Set: true}}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterOrgSettingsServer(srv, f)
	ascv1.RegisterAgentsServer(srv, orgCardsRuns{})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()

	_, err = db.GetEngine(db.DefaultContext).ID(3).Cols("storage_backend", "is_private").Update(&repo_model.Repository{StorageBackend: "asc", IsPrivate: false})
	require.NoError(t, err)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	member := loginUser(t, "user2")
	other := loginUser(t, "user5")
	page := func(s *TestSession) *HTMLDoc {
		return NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/org3"), http.StatusOK).Body)
	}

	t.Run("A member reads the kept agents and the merge defaults", func(t *testing.T) {
		doc := page(member)
		card := doc.Find(`[data-testid="org-agents"]`)
		require.Equal(t, 1, card.Length())
		assert.Equal(t, "Release bot", strings.TrimSpace(card.Find(".wk-org-agent-name").Text()))
		assert.Equal(t, "@org3/release-bot", strings.TrimSpace(card.Find(".wk-org-agent-meta").Text()))
		assert.Equal(t, "Paused", strings.TrimSpace(card.Find(".ds-chip-warn").Text()))
		assert.Equal(t, "1 agent", strings.TrimSpace(doc.Find("[data-asc-org-agents-count]").Text()))
		assert.Equal(t, "/org/org3/members#people-agents", doc.Find("[data-asc-org-agents-count]").AttrOr("href", ""))

		var rules []string
		doc.Find(`[data-testid="org-merge-policy-rules"] li`).Each(func(_ int, s *goquery.Selection) {
			rules = append(rules, strings.TrimSpace(s.Text()))
		})
		assert.Equal(t, []string{"2 approvals required", "2 required checks", "Changes must be signed", "Rebase"}, rules)
		assert.Equal(t, "/org/org3/settings/asc/merge", doc.Find(`[data-testid="org-merge-policy"] a.ds-link`).AttrOr("href", ""))
	})

	t.Run("Merge defaults not set yet", func(t *testing.T) {
		f.mu.Lock()
		f.merge = &ascv1.MergeDefaults{}
		f.mu.Unlock()
		doc := page(member)
		assert.Equal(t, 1, doc.Find(`[data-testid="org-merge-policy-unset"]`).Length())
		assert.Equal(t, 0, doc.Find(`[data-testid="org-merge-policy-rules"]`).Length())
	})

	t.Run("Someone else without agent features: no agents", func(t *testing.T) {
		doc := page(other)
		assert.Equal(t, 0, doc.Find(`[data-testid="org-agents"]`).Length())
		assert.Equal(t, 0, doc.Find("[data-asc-org-agents-count]").Length())
		assert.Equal(t, 0, doc.Find(`[data-testid="org-merge-policy"]`).Length())
	})

	require.NoError(t, ascagentui.SetFeatures(t.Context(), user5, true))
	require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, true))

	t.Run("Someone else with agent features reads the agents that ran", func(t *testing.T) {
		doc := page(other)
		card := doc.Find(`[data-testid="org-agents"]`)
		require.Equal(t, 1, card.Length())
		a := card.Find("a.wk-org-agent-name")
		assert.Equal(t, "@org3/review", strings.TrimSpace(a.Text()))
		assert.Equal(t, "/org3/repo3/asc/agents?agent=review", a.AttrOr("href", ""))
		assert.Equal(t, "4 agent runs", strings.TrimSpace(card.Find(".wk-org-agent-meta").Text()))
		assert.Equal(t, "1 active", strings.TrimSpace(card.Find(".ds-chip-agent").Text()))
		assert.Equal(t, "1 agent", strings.TrimSpace(doc.Find("[data-asc-org-agents-count]").Text()))
		assert.Equal(t, "/org3#wk-org-agents", doc.Find("[data-asc-org-agents-count]").AttrOr("href", ""))
		assert.Equal(t, 0, doc.Find(`[data-testid="org-merge-policy"]`).Length(), "the merge defaults are for members")
	})
}
