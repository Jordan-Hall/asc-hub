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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type exploreRunCounts struct {
	ascv1.UnimplementedAgentsServer
}

func (exploreRunCounts) CountRuns(_ context.Context, req *ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
	per := make([]uint64, len(req.GetRepos()))
	for i, r := range req.GetRepos() {
		if r.GetName() == "user2/repo1" {
			per[i] = 3
		}
	}
	return &ascv1.CountAgentRunsResponse{PerRepo: per}, nil
}

// A row of Explore carries the agent activity chip of its asc repository for a person with agent features on.
func TestTheExploreRowsCarryTheAgentRunsChip(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, exploreRunCounts{})
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
	session := loginUser(t, "user2")
	chip := `a[data-testid="repo-agent-runs"]`

	t.Run("Agent features off: no chip", func(t *testing.T) {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(chip).Length())
	})

	require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, true))

	t.Run("The asc row has the chip, linking to its agent runs", func(t *testing.T) {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1"), http.StatusOK).Body)
		a := doc.Find(chip)
		require.Equal(t, 1, a.Length())
		assert.Equal(t, "/user2/repo1/asc/agents", a.AttrOr("href", ""))
		assert.Equal(t, "3 agent runs", strings.TrimSpace(a.Text()))
		assert.Equal(t, "3 agent runs in the last 7 days", a.AttrOr("aria-label", ""))
		assert.True(t, a.HasClass("ds-chip-agent"))
	})

	t.Run("The profile list has it too", func(t *testing.T) {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2?tab=repositories&q=repo1"), http.StatusOK).Body)
		assert.Equal(t, 1, doc.Find(chip).Length())
	})

	t.Run("A visitor has no chip", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos?q=repo1"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(chip).Length())
	})
}
