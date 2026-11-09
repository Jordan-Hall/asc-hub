// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
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

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// A person's page in site administration has an Agents card with the agents the person runs.
func TestTheAdminPersonPageHasTheAgentsThePersonRuns(t *testing.T) {
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
	admin := loginUser(t, "user1")
	card := func() *goquery.Selection {
		doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/admin/users/2"), http.StatusOK).Body)
		return doc.Find(`[data-testid="admin-user-agents"]`)
	}

	f.set(t, 2)
	assert.Equal(t, 0, card().Length(), "agent features are off for the administrator")
	assert.Empty(t, f.asked())

	require.NoError(t, ascagentui.SetFeatures(t.Context(), unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1}), true))
	f.set(t, 7)
	got := card()
	require.Equal(t, 1, got.Length())
	rows := got.Find("li[data-agent]")
	require.Equal(t, 5, rows.Length(), "at most five")
	first := rows.First().Find("a")
	assert.Equal(t, "@user2/bot-0", strings.TrimSpace(first.Text()))
	assert.Equal(t, "/user2/repo1/asc/agents?agent=bot-0", first.AttrOr("href", ""))
	assert.Contains(t, got.Text(), "And 2 more.")
	assert.Equal(t, []string{"user2"}, f.asked(), "the runs the person created")

	f.set(t, 0)
	got = card()
	require.Equal(t, 1, got.Length())
	assert.Contains(t, got.Text(), "No agent runs started by this person.")
}
