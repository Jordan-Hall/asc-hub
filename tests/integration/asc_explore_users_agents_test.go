// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascpresence"
	"forgejo.org/services/asctrust"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type exploreUserAgents struct {
	ascv1.UnimplementedAgentsServer
}

func (exploreUserAgents) SummarizeAgents(_ context.Context, req *ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
	resp := &ascv1.SummarizeAgentsResponse{}
	for i, r := range req.GetRepos() {
		if r.GetName() != "user2/repo1" {
			continue
		}
		for k := range 7 {
			runs := uint64(20 - k)
			resp.Agents = append(resp.Agents, &ascv1.AgentSummary{Agent: fmt.Sprintf("bot-%d", k), Runs: runs, Active: uint64(k % 2), Places: []*ascv1.AgentPlace{{Repo: uint32(i), Runs: runs, Active: uint64(k % 2), LastRun: time.Now().Add(-time.Hour).Unix()}}})
		}
	}
	return resp, nil
}

func attrs(sel *goquery.Selection, name string) []string {
	var out []string
	sel.Each(func(_ int, s *goquery.Selection) { out = append(out, s.AttrOr(name, "")) })
	return out
}

// The people directory has the chips All, People, Agents, Maintainers and Online now, rows for the agents that ran
// in the asc repositories the viewer can read, and a "Who is active now" card.
func TestTheExplorePeopleHaveChipsAgentRowsAndWhoIsActive(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, exploreUserAgents{})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()

	_, err = db.GetEngine(db.DefaultContext).ID(1).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
	require.NoError(t, err)
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	now := strconv.FormatInt(time.Now().Unix(), 10)
	// user4 is online; user5 is online but hides it; user8 is online but keeps activity private
	for _, id := range []int64{4, 5, 8} {
		require.NoError(t, user_model.SetUserSetting(db.DefaultContext, id, "asc.last_seen", now))
	}
	require.NoError(t, ascpresence.SetShowOnline(db.DefaultContext, 5, false))
	_, err = db.GetEngine(db.DefaultContext).ID(8).Cols("keep_activity_private").Update(&user_model.User{KeepActivityPrivate: true})
	require.NoError(t, err)
	// user10 was seen long ago
	require.NoError(t, user_model.SetUserSetting(db.DefaultContext, 10, "asc.last_seen", strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)))

	session := loginUser(t, "user2")
	get := func(s *TestSession, q string) *HTMLDoc {
		if s == nil {
			return NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/users"+q), http.StatusOK).Body)
		}
		return NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/explore/users"+q), http.StatusOK).Body)
	}
	rowNames := func(doc *HTMLDoc) []string {
		var out []string
		doc.Find(".ds-userlist:not(.ds-agent-rows) li.ds-user-row .ds-li-title > a").Each(func(_ int, s *goquery.Selection) {
			out = append(out, strings.TrimSpace(s.Text()))
		})
		return out
	}

	t.Run("The chips keep the search and the order", func(t *testing.T) {
		doc := get(session, "?q=user&sort=alphabetically")
		chips := doc.Find(`[data-testid="explore-users-chips"] a`)
		assert.Equal(t, []string{"all", "people", "agents", "maintainers", "online"}, attrs(chips, "data-chip"))
		assert.Equal(t, "?q=user&show=online&sort=alphabetically", chips.Last().AttrOr("href", ""))
		assert.Equal(t, "true", chips.First().AttrOr("aria-current", ""))
		assert.Equal(t, []string{"all", "people", "agents", "online"}, attrs(get(nil, "").Find(`[data-testid="explore-users-chips"] a`), "data-chip"), "roles are for signed-in viewers")
	})

	t.Run("Maintainers keeps the people with the role", func(t *testing.T) {
		doc := get(session, "?show=maintainers")
		roles := attrs(doc.Find(`[data-testid="user-role-chip"]`), "data-role")
		require.NotEmpty(t, roles)
		for _, r := range roles {
			assert.Equal(t, "maintainer", r)
		}
		assert.Equal(t, "true", doc.Find(`[data-chip="maintainers"]`).AttrOr("aria-current", ""))
		assert.Equal(t, "true", get(nil, "?show=maintainers").Find(`[data-chip="all"]`).AttrOr("aria-current", ""))
	})

	t.Run("Online now keeps who shows as online", func(t *testing.T) {
		names := rowNames(get(session, "?show=online"))
		assert.Contains(t, names, "user4")
		assert.NotContains(t, names, "user5", "hides being online")
		assert.NotContains(t, names, "user8", "keeps activity private")
		assert.NotContains(t, names, "user10", "seen an hour ago")
	})

	t.Run("Who is active now names the people online", func(t *testing.T) {
		card := get(nil, "").Find("[data-asc-active-now]")
		require.Equal(t, 1, card.Length())
		people := attrs(card.Find("[data-asc-active-person]"), "data-asc-active-person")
		assert.Contains(t, people, "user4")
		assert.NotContains(t, people, "user5")
		assert.NotContains(t, people, "user8")
		assert.Equal(t, 0, card.Find("[data-asc-active-agent]").Length(), "agents are for agent features")
	})

	t.Run("Agent features off: no agent rows", func(t *testing.T) {
		assert.Equal(t, 0, get(session, "").Find("[data-asc-explore-agents]").Length())
	})

	require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, true))

	t.Run("People and agents: the first five agents and a link to all", func(t *testing.T) {
		doc := get(session, "")
		rows := doc.Find("[data-asc-explore-agents] li")
		require.Equal(t, 5, rows.Length())
		first := rows.First()
		assert.Equal(t, "@user2/bot-0", first.AttrOr("data-asc-agent", ""))
		assert.Equal(t, "/user2/repo1/asc/agents?agent=bot-0", first.Find(".ds-li-title a").AttrOr("href", ""))
		assert.Equal(t, "user2", strings.TrimSpace(first.Find("[data-asc-agent-owner] a").Text()))
		assert.Equal(t, "repo1", strings.TrimSpace(first.Find("[data-asc-agent-scope] a").Text()))
		assert.Equal(t, "20", strings.TrimSpace(first.Find("[data-asc-agent-runs] b").Text()))
		assert.Equal(t, "View runs", strings.TrimSpace(first.Find(".ds-row-acts a").Text()))
		assert.Equal(t, 0, first.Find("[data-asc-agent-active]").Length())
		assert.Equal(t, "1 active", strings.TrimSpace(rows.Eq(1).Find("[data-asc-agent-active]").Text()))
		assert.Equal(t, "?show=agents&sort=newest", doc.Find("[data-asc-explore-agents-all]").AttrOr("href", ""))
		assert.NotEmpty(t, rowNames(doc), "the people follow")

		active := attrs(doc.Find("[data-asc-active-now] [data-asc-active-agent]"), "data-asc-active-agent")
		assert.Equal(t, []string{"@user2/bot-1", "@user2/bot-3", "@user2/bot-5"}, active, "the agents with runs going")
	})

	t.Run("Agents shows all of them; the search keeps the ones that match", func(t *testing.T) {
		assert.Equal(t, 7, get(session, "?show=agents").Find("[data-asc-explore-agents] li").Length())
		rows := get(session, "?show=agents&q=%40user2%2Fbot-3").Find("[data-asc-explore-agents] li")
		assert.Equal(t, []string{"@user2/bot-3"}, attrs(rows, "data-asc-agent"))
	})

	t.Run("People, the second page and another organization have no agent rows", func(t *testing.T) {
		assert.Equal(t, 0, get(session, "?show=people").Find("[data-asc-explore-agents]").Length())
		assert.Equal(t, 0, get(session, "?page=2").Find("[data-asc-explore-agents]").Length())
		assert.Equal(t, 0, get(session, "?org=org3").Find("[data-asc-explore-agents]").Length())
	})
}
