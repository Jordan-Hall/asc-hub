// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeActivity struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedUsageServer

	mu    sync.Mutex
	resp  *ascv1.GetActivityResponse
	fail  bool
	asked int
}

func (*fakeActivity) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeatureUsageActivity}}, nil
}

func (f *fakeActivity) GetActivity(gocontext.Context, *ascv1.GetActivityRequest) (*ascv1.GetActivityResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	if f.fail {
		return nil, status.Error(codes.Internal, "no")
	}
	return f.resp, nil
}

func (f *fakeActivity) set(r *ascv1.GetActivityResponse, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resp, f.fail = r, fail
}

func (f *fakeActivity) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

func startActivity(t *testing.T) *fakeActivity {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeActivity{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterUsageServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

func forgetActivity() {
	activityCache.Lock()
	activityCache.at = time.Time{}
	activityCache.Unlock()
}

func TestTheDashboardShowsTodaysActivity(t *testing.T) {
	f := startActivity(t)
	defer test.MockVariableValue(&activityTTL, 0)()
	forgetActivity()
	dashboard := func(ctx *context.Context) {
		dashboardFacts(ctx)
		ctx.HTML(http.StatusOK, tplDashboard)
	}

	f.set(&ascv1.GetActivityResponse{Changes: 10, ChangesByAgents: 3, Runs: 6, RunsActive: 1, RunsSucceeded: 4, RunsFailed: 1}, false)
	r := runBackups(t, "GET", 1, nil, "", dashboard)
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Contains(t, r.body, `data-testid="adm-stat-changes"`)
	assert.Contains(t, r.body, "asc_admin.stat_changes_agents")
	assert.Contains(t, r.body, `href="/admin/asc/agents" data-testid="adm-stat-runs"`)
	assert.Contains(t, r.body, "asc_admin.stat_runs_finished")
	assert.NotContains(t, r.body, "asc_admin.stat_orgs", "the asc tiles take the places of organizations and issues")
	assert.Contains(t, r.body, "asc_admin.stat_people_week")

	// Nothing yet today.
	f.set(&ascv1.GetActivityResponse{Runs: 2, RunsActive: 2}, false)
	r = runBackups(t, "GET", 1, nil, "", dashboard)
	assert.Contains(t, r.body, "asc_admin.stat_changes_none")
	assert.Contains(t, r.body, "asc_admin.stat_runs_running")

	// An answer that does not add up, or no answer, gives the hub's own tiles.
	for _, fail := range []bool{false, true} {
		f.set(&ascv1.GetActivityResponse{Changes: 1, ChangesByAgents: 2}, fail)
		r = runBackups(t, "GET", 1, nil, "", dashboard)
		require.Equal(t, http.StatusOK, r.status, r.body)
		assert.NotContains(t, r.body, `data-testid="adm-stat-changes"`)
		assert.Contains(t, r.body, "asc_admin.stat_orgs")
	}

	// Someone who is not a site administrator does not ask asc.
	n := f.calls()
	ctx, _ := contexttest.MockContext(t, "GET /admin")
	contexttest.LoadUser(t, ctx, 2)
	_, ok := todaysActivity(ctx, time.Now())
	assert.False(t, ok)
	assert.Equal(t, n, f.calls())
}

func TestTodaysActivityIsKeptForAMinute(t *testing.T) {
	f := startActivity(t)
	forgetActivity()
	f.set(&ascv1.GetActivityResponse{Changes: 4, Runs: 1, RunsSucceeded: 1}, false)
	ctx, _ := contexttest.MockContext(t, "GET /admin")
	contexttest.LoadUser(t, ctx, 1)
	now := time.Date(2026, 11, 9, 23, 59, 0, 0, time.UTC)

	a, ok := todaysActivity(ctx, now)
	require.True(t, ok)
	assert.EqualValues(t, 4, a.Changes)
	f.set(&ascv1.GetActivityResponse{Changes: 9}, false)
	a, _ = todaysActivity(ctx, now.Add(30*time.Second))
	assert.EqualValues(t, 4, a.Changes, "kept")
	assert.Equal(t, 1, f.calls())
	// A new UTC day asks again, so the figures are never yesterday's.
	a, _ = todaysActivity(ctx, now.Add(time.Minute))
	assert.EqualValues(t, 9, a.Changes)
	f.set(&ascv1.GetActivityResponse{Changes: 11}, false)
	a, _ = todaysActivity(ctx, now.Add(90*time.Second))
	assert.EqualValues(t, 9, a.Changes, "kept")
	assert.Equal(t, 2, f.calls())
	a, _ = todaysActivity(ctx, now.Add(3*time.Minute))
	assert.EqualValues(t, 11, a.Changes, "asked again after a minute")
	assert.Equal(t, 3, f.calls())
}

func TestNewPeopleThisWeek(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	all, err := db.GetEngine(db.DefaultContext).Where("type = ?", user_model.UserTypeIndividual).Count(new(user_model.User))
	require.NoError(t, err)
	n, err := newPeopleSince(db.DefaultContext, time.Unix(0, 0))
	require.NoError(t, err)
	assert.Equal(t, all, n, "people only, not organizations")
	_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET created_unix = ? WHERE id = 2", 4_000_000_000)
	require.NoError(t, err)
	n, err = newPeopleSince(db.DefaultContext, time.Unix(3_999_999_999, 0))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
}
