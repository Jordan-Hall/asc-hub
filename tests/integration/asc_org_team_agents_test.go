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
	"forgejo.org/models/organization"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascteams"
	"forgejo.org/services/asctrust"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type teamAgentsAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedOrgSettingsServer
	ascv1.UnimplementedPathRulesServer

	mu    sync.Mutex
	teams map[string][]string
}

func (*teamAgentsAsc) Version(context.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{ascteams.Feature, asctrust.FeatureOrgSettings}}, nil
}

func (*teamAgentsAsc) ListAgents(context.Context, *ascv1.ListOrgAgentsRequest) (*ascv1.ListOrgAgentsResponse, error) {
	return &ascv1.ListOrgAgentsResponse{Agents: []*ascv1.OrgAgent{
		{Name: "docs-bot", DisplayName: "Docs bot", Paths: []string{"docs/**"}},
		{Name: "fixer"},
	}}, nil
}

func (f *teamAgentsAsc) ListTeams(context.Context, *ascv1.ListTeamsRequest) (*ascv1.ListTeamsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ascv1.ListTeamsResponse{}
	for name, members := range f.teams {
		out.Teams = append(out.Teams, &ascv1.Team{Name: name, Members: members})
	}
	return out, nil
}

func (f *teamAgentsAsc) SetTeamMembers(_ context.Context, req *ascv1.SetTeamMembersRequest) (*ascv1.SetTeamMembersResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.teams[req.GetTeam()] = req.GetMembers()
	return &ascv1.SetTeamMembersResponse{}, nil
}

// An owner adds the organization's agents to a team on the Teams page and removes them; members see them with their
// path scope, and asc receives them as team members.
func TestTheTeamPageManagesAgentMembers(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := &teamAgentsAsc{teams: map[string][]string{}}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterOrgSettingsServer(srv, f)
	ascv1.RegisterPathRulesServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()

	owner := loginUser(t, "user2")
	card := func(s *TestSession) *goquery.Selection {
		resp := s.MakeRequest(t, NewRequest(t, "GET", "/org/org3/teams?team=team1"), http.StatusOK)
		return NewHTMLParser(t, resp.Body).Find(`[data-testid="org-team-agents"]`)
	}

	got := card(owner)
	require.Equal(t, 1, got.Length())
	assert.Equal(t, 2, got.Find(`select[name="agent"] option`).Length(), "both agents can be added")
	assert.Equal(t, 0, got.Find(`li[data-agent]`).Length())

	req := NewRequestWithValues(t, "POST", "/org/org3/teams/team1/action/agent_add", map[string]string{"agent": "docs-bot"})
	resp := owner.MakeRequest(t, req, http.StatusSeeOther)
	assert.Equal(t, "/org/org3/teams?team=team1#team-detail", test.RedirectURL(resp))
	ascteams.Wait()
	names, err := organization.TeamAgents(db.DefaultContext, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"docs-bot"}, names)

	req = NewRequestWithValues(t, "POST", "/org/org3/teams/team1/action/agent_add", map[string]string{"agent": "stranger"})
	owner.MakeRequest(t, req, http.StatusSeeOther)
	names, err = organization.TeamAgents(db.DefaultContext, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"docs-bot"}, names, "not an agent of the organization")

	got = card(owner)
	row := got.Find(`li[data-agent="docs-bot"]`)
	require.Equal(t, 1, row.Length())
	assert.Contains(t, row.Find(".wk-team-row-main").Text(), "Docs bot")
	assert.Equal(t, "docs/**", strings.TrimSpace(row.Find(".wk-team-glob").Text()))
	assert.Equal(t, 1, got.Find(`select[name="agent"] option`).Length())

	// A member who is not an owner sees the agents but cannot add or remove them.
	member := loginUser(t, "user4")
	got = card(member)
	require.Equal(t, 1, got.Length())
	assert.Equal(t, 1, got.Find(`li[data-agent="docs-bot"]`).Length())
	assert.Equal(t, 0, got.Find("form").Length())
	req = NewRequestWithValues(t, "POST", "/org/org3/teams/team1/action/agent_remove", map[string]string{"agent": "docs-bot"})
	member.MakeRequest(t, req, http.StatusNotFound)

	req = NewRequestWithValues(t, "POST", "/org/org3/teams/team1/action/agent_remove", map[string]string{"agent": "docs-bot"})
	owner.MakeRequest(t, req, http.StatusSeeOther)
	ascteams.Wait()
	names, err = organization.TeamAgents(db.DefaultContext, 2)
	require.NoError(t, err)
	assert.Empty(t, names)
}
