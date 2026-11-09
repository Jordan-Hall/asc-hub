// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
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

type teamOwnerAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedPolicyServer
}

func (teamOwnerAsc) Version(context.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeaturePolicy}}, nil
}

func (teamOwnerAsc) GetSource(_ context.Context, req *ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
	switch req.GetRepo().GetName() {
	case "org3/repo21":
		return &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{
			Roles: []*ascv1.PolicyRole{{Name: "maintainer", Members: []string{"team:team1"}}},
			Paths: []*ascv1.PolicyPathRule{{Glob: "payments/**", Write: []string{"team:team1"}}, {Glob: "docs/**", Write: []string{"*"}}},
		}}, nil
	case "org3/repo3":
		return &ascv1.GetPolicySourceResponse{Present: true, Parsed: &ascv1.GetPolicyResponse{Error: "line 2: bad"}}, nil
	}
	return &ascv1.GetPolicySourceResponse{}, nil
}

// The team page says which repository policies name the selected team, and what they give it.
func TestTheTeamPageShowsWhatPoliciesGiveTheTeam(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascteams.ForgetPolicies()
	defer ascteams.ForgetPolicies()
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, teamOwnerAsc{})
	ascv1.RegisterPolicyServer(srv, teamOwnerAsc{})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()
	_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET storage_backend = 'asc' WHERE id IN (3, 32)")
	require.NoError(t, err)
	_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET default_branch = 'trunk' WHERE id = 32")
	require.NoError(t, err)

	owner := loginUser(t, "user2")
	page := func(team string) *goquery.Selection {
		resp := owner.MakeRequest(t, NewRequest(t, "GET", "/org/org3/teams?team="+team), http.StatusOK)
		return NewHTMLParser(t, resp.Body).Find(`[data-testid="org-team-policy-owner"]`)
	}

	card := page("team1")
	require.Equal(t, 1, card.Length())
	assert.Equal(t, "Policy owner", strings.TrimSpace(card.Find(".wk-team-sub").Text()))
	rows := card.Find(".wk-team-rows li")
	require.Equal(t, 1, rows.Length(), "repo3's policy does not parse, so it gives nothing")
	assert.Equal(t, "org3/repo21", strings.TrimSpace(rows.Find(".wk-team-row-main").Text()))
	assert.Equal(t, "/org3/repo21/src/branch/trunk/.asc/policy.toml", rows.Find(".wk-team-row-main").AttrOr("href", ""))
	assert.Equal(t, "maintainer role", strings.TrimSpace(rows.Find(".ds-chip").Text()))
	assert.Equal(t, "payments/**", strings.TrimSpace(rows.Find(".wk-team-glob").Text()), "`*` is not the team")

	assert.Equal(t, 0, page("test_team").Length(), "no policy names the team: no card")
}
