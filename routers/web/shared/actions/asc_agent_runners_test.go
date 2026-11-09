// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	gocontext "context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type fakeRunners struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedMaintenanceServer

	mu       sync.Mutex
	features []string
	runners  []*ascv1.AgentRunner
	fail     bool
	asked    int
}

func (f *fakeRunners) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeRunners) ListAgentRunners(gocontext.Context, *ascv1.ListAgentRunnersRequest) (*ascv1.ListAgentRunnersResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	if f.fail {
		return nil, status.Error(codes.Unavailable, "no")
	}
	return &ascv1.ListAgentRunnersResponse{Runners: f.runners, Now: time.Now().Unix()}, nil
}

func (f *fakeRunners) set(runners []*ascv1.AgentRunner, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runners, f.fail = runners, fail
}

// mapSession is a map-backed session store; the embedded interface is nil and only Get, Set and Delete are used.
type mapSession struct {
	session.Store
	values map[any]any
}

func (s *mapSession) Get(k any) any      { return s.values[k] }
func (s *mapSession) Set(k, v any) error { s.values[k] = v; return nil }
func (s *mapSession) Delete(k any) error { delete(s.values, k); return nil }

func serveRunners(t *testing.T, f *fakeRunners) (string, func()) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterMaintenanceServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	return l.Addr().String(), srv.Stop
}

func someRunners() []*ascv1.AgentRunner {
	now := time.Now().Unix()
	return []*ascv1.AgentRunner{
		{
			Name: "agent-box-1", Slots: 4, Vcpu: 8, Version: "0.9.0", Started: now - 3600, LastSeen: now, Online: true,
			Runs: []*ascv1.AgentRunnerRun{{Id: 214, Repo: "user2/repo1", Title: "Fix the flaky test", Agent: "fixer", Started: now - 60}},
		},
		{Name: "agent-box-2", Slots: 2, Vcpu: 4, Version: "0.9.0", Started: now - 7200, LastSeen: now, Online: true},
		{Name: "agent-box-3", Slots: 2, Vcpu: 2, Version: "0.8.1", Started: now - 90000, LastSeen: now - 600},
	}
}

func TestTheAdminRunnersPageListsTheAgentRunners(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeRunners{features: []string{asctrust.FeatureAgentRunners}}
	addr, stop := serveRunners(t, f)
	defer stop()
	c, err := ascclient.New(ascclient.Config{Target: addr, InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()
	defer test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef"))()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	page := func(user int64, path string, flag string) (int, string) {
		ctx, rec := contexttest.MockContext(t, "GET "+path)
		ctx.Render = templates.HTMLRenderer()
		ctx.Session = &mapSession{values: map[any]any{}}
		contexttest.LoadUser(t, ctx, user)
		ctx.IsSigned = true
		ctx.Data[flag] = true
		RunnersList(ctx)
		return rec.Code, rec.Body.String()
	}

	f.set(someRunners(), false)
	code, body := page(1, "/admin/actions/runners", "PageIsAdmin")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-testid="agent-runners"`)
	assert.Contains(t, body, `data-runner="agent-box-1"`)
	assert.Contains(t, body, `data-runner="agent-box-3"`)
	assert.Contains(t, body, `/user2/repo1/asc/agents/runs/214"`, "a live run links to its run page")
	assert.Contains(t, body, "asc_admin.agent_runner_run")
	assert.Contains(t, body, "asc_admin.agent_runner_busy")
	assert.Contains(t, body, "asc_admin.runners_status_idle")
	assert.Contains(t, body, "asc_admin.runners_status_offline")
	assert.Contains(t, body, "asc_admin.agent_runner_slots")
	assert.Contains(t, body, "asc_admin.agent_runner_vcpu")
	assert.Contains(t, body, "asc_admin.agent_runners_live")

	// No runner has reported yet.
	f.set(nil, false)
	_, body = page(1, "/admin/actions/runners", "PageIsAdmin")
	assert.Contains(t, body, "asc_admin.agent_runners_none_title")

	// asc serve does not answer: the section says so, the page still draws.
	f.set(nil, true)
	code, body = page(1, "/admin/actions/runners", "PageIsAdmin")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-testid="agent-runners-unread"`)

	// The runners page of a user's settings does not ask asc serve.
	f.set(someRunners(), false)
	f.mu.Lock()
	n := f.asked
	f.mu.Unlock()
	code, body = page(2, "/user/settings/actions/runners", "PageIsUserSettings")
	require.Equal(t, http.StatusOK, code)
	assert.NotContains(t, body, `data-testid="agent-runners"`)
	f.mu.Lock()
	assert.Equal(t, n, f.asked)
	f.mu.Unlock()

	// With asc off there is no section.
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	_, body = page(1, "/admin/actions/runners", "PageIsAdmin")
	assert.NotContains(t, body, `data-testid="agent-runners"`)
}

func TestLoadAgentRunnersRefusesABadAnswer(t *testing.T) {
	f := &fakeRunners{}
	addr, stop := serveRunners(t, f)
	defer stop()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	f.set(someRunners(), false)
	got, err := asctrust.LoadAgentRunners(gocontext.Background(), conn)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.True(t, got[0].Busy())
	assert.Equal(t, uint64(214), got[0].Runs[0].ID)
	assert.Equal(t, "user2/repo1", got[0].Runs[0].Repo)
	assert.False(t, got[1].Busy())
	assert.False(t, got[2].Online)
	assert.Equal(t, uint32(8), got[0].VCPU)

	for _, bad := range [][]*ascv1.AgentRunner{
		{{Name: ""}},
		{{Name: "box", Runs: []*ascv1.AgentRunnerRun{{Id: 0, Repo: "a/b"}}}},
		{{Name: "box", Runs: []*ascv1.AgentRunnerRun{{Id: 1, Repo: "../../admin"}}}},
		{{Name: "box", Runs: []*ascv1.AgentRunnerRun{{Id: 1, Repo: "noslash"}}}},
		{{Name: "box", Runs: []*ascv1.AgentRunnerRun{{Id: 1, Repo: "../x"}}}},
		{{Name: "box", Runs: []*ascv1.AgentRunnerRun{{Id: 1, Repo: "a/b?c"}}}},
	} {
		f.set(bad, false)
		_, err := asctrust.LoadAgentRunners(gocontext.Background(), conn)
		assert.ErrorIs(t, err, asctrust.ErrBadAnswer)
	}
}
