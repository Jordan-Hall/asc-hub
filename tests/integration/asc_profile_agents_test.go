// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"fmt"
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
	"forgejo.org/modules/cache"
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

type profileAgents struct {
	ascv1.UnimplementedAgentsServer
	mu        sync.Mutex
	n         int
	createdBy []string
}

func (f *profileAgents) SummarizeAgents(_ context.Context, req *ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdBy = append(f.createdBy, req.GetCreatedBy())
	resp := &ascv1.SummarizeAgentsResponse{}
	for i, r := range req.GetRepos() {
		if r.GetName() != "user2/repo1" {
			continue
		}
		for k := range f.n {
			runs := uint64(10 - k)
			resp.Agents = append(resp.Agents, &ascv1.AgentSummary{Agent: fmt.Sprintf("bot-%d", k), Runs: runs, Active: uint64(k % 2), Places: []*ascv1.AgentPlace{{Repo: uint32(i), Runs: runs, Active: uint64(k % 2)}}})
		}
	}
	return resp, nil
}

func (f *profileAgents) set(t *testing.T, n int) {
	require.NoError(t, cache.GetCache().Flush(), "a new answer, not the kept one")
	f.mu.Lock()
	f.n = n
	f.createdBy = nil
	f.mu.Unlock()
}

func (f *profileAgents) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createdBy
}

// A profile has an "Agents <name> runs" card with the agents the person runs, and New agent on one's own profile.
func TestTheProfileHasTheAgentsThePersonRuns(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := &profileAgents{}
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, f)
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
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	self := loginUser(t, "user2")
	other := loginUser(t, "user5")
	card := func(s *TestSession) *goquery.Selection {
		doc := NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
		return doc.Find("[data-asc-pf-agents]")
	}

	t.Run("Agent features off: no card", func(t *testing.T) {
		f.set(t, 2)
		assert.Equal(t, 0, card(self).Length())
		assert.Empty(t, f.asked(), "asc is not asked")
	})

	require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, true))
	require.NoError(t, ascagentui.SetFeatures(t.Context(), user5, true))

	t.Run("One's own profile names the agents and has New agent", func(t *testing.T) {
		f.set(t, 2)
		c := card(self)
		require.Equal(t, 1, c.Length())
		assert.Equal(t, "Agents user2 runs", strings.TrimSpace(c.Find("h2").Text()))
		assert.Equal(t, "/-/asc/agents", c.Find("[data-asc-pf-new-agent]").AttrOr("href", ""))
		names := c.Find("a.wk-pf-agent-name")
		require.Equal(t, 2, names.Length())
		assert.Equal(t, "@user2/bot-0", strings.TrimSpace(names.First().Text()))
		assert.Equal(t, "/user2/repo1/asc/agents?agent=bot-0", names.First().AttrOr("href", ""))
		assert.Equal(t, "10 agent runs · user2/repo1", strings.TrimSpace(c.Find(".wk-pf-agent-meta").First().Text()))
		assert.Equal(t, "1 active", strings.TrimSpace(c.Find(".ds-chip-agent").Text()), "only bot-1 has a run going")
		assert.Equal(t, []string{"user2"}, f.asked(), "the runs the person created")
	})

	t.Run("At most five, then how many more", func(t *testing.T) {
		f.set(t, 7)
		c := card(other)
		assert.Equal(t, 5, c.Find("a.wk-pf-agent-name").Length())
		assert.Equal(t, "and 2 more", strings.TrimSpace(c.Find("[data-asc-pf-agents-more]").Text()))
		assert.Equal(t, 0, c.Find("[data-asc-pf-new-agent]").Length(), "New agent is on one's own profile only")
	})

	t.Run("No agents: an empty card for oneself, none for someone else", func(t *testing.T) {
		f.set(t, 0)
		c := card(self)
		require.Equal(t, 1, c.Length())
		assert.Equal(t, "No agent runs yet.", strings.TrimSpace(c.Find("[data-asc-pf-agents-none]").Text()))
		assert.Equal(t, 1, c.Find("[data-asc-pf-new-agent]").Length())
		assert.Equal(t, 0, card(other).Length())
	})

	t.Run("A visitor gets no agents", func(t *testing.T) {
		f.set(t, 2)
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find("[data-asc-pf-agents]").Length())
		assert.Empty(t, f.asked())
	})
}
