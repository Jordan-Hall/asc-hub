// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repoquick

import (
	"context"
	"net"
	"sync"
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
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeRunCounts struct {
	ascv1.UnimplementedAgentsServer
	mu     sync.Mutex
	calls  []*ascv1.CountAgentRunsRequest
	answer func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error)
}

func (f *fakeRunCounts) CountRuns(_ context.Context, req *ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	answer := f.answer
	f.mu.Unlock()
	return answer(req)
}

func (f *fakeRunCounts) took() []*ascv1.CountAgentRunsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakeRunCounts) set(answer func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error)) {
	f.mu.Lock()
	f.answer = answer
	f.mu.Unlock()
}

func startRunCounts(t *testing.T) *fakeRunCounts {
	t.Helper()
	f := &fakeRunCounts{}
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

func loadAgentRuns(t *testing.T, doer *user_model.User, repos repo_model.RepositoryList) (map[int64]uint64, bool) {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "/explore/repos")
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	LoadAgentRuns(ctx, repos)
	runs, ok := ctx.Data["AscRowAgentRuns"].(map[int64]uint64)
	return runs, ok
}

// The agent chip of a row counts the runs of the last week of each asc repository the viewer can read, in one call.
func TestTheRowsGetTheirAgentRunsInOneCall(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startRunCounts(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	defer test.MockVariableValue(&Now, func() time.Time { return now })()

	for _, id := range []int64{1, 2} {
		_, err := db.GetEngine(db.DefaultContext).ID(id).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
		require.NoError(t, err)
	}
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	var repos repo_model.RepositoryList
	for _, id := range []int64{1, 2, 3} {
		repos = append(repos, unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: id}))
	}
	f.set(func(req *ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
		per := make([]uint64, len(req.GetRepos()))
		for i, r := range req.GetRepos() {
			if r.GetName() == "user2/repo1" {
				per[i] = 3
			}
			if r.GetName() == "user2/repo2" {
				per[i] = 5000
			}
		}
		return &ascv1.CountAgentRunsResponse{PerRepo: per}, nil
	})

	t.Run("Agent features off, or a visitor: no chips and nothing asked", func(t *testing.T) {
		_, ok := loadAgentRuns(t, user2, repos)
		assert.False(t, ok)
		_, ok = loadAgentRuns(t, nil, repos)
		assert.False(t, ok)
		assert.Empty(t, f.took())
	})

	require.NoError(t, ascagentui.SetFeatures(context.Background(), user2, true))
	require.NoError(t, ascagentui.SetFeatures(context.Background(), user4, true))

	t.Run("One call over the asc rows the viewer can read, since a week ago", func(t *testing.T) {
		runs, ok := loadAgentRuns(t, user2, repos)
		require.True(t, ok)
		assert.Equal(t, map[int64]uint64{1: 3, 2: AgentRunsMax}, runs, "a large count is bounded")
		calls := f.took()
		require.Len(t, calls, 1)
		var names []string
		for _, r := range calls[0].GetRepos() {
			names = append(names, r.GetName())
		}
		assert.Equal(t, []string{"user2/repo1", "user2/repo2"}, names, "repo3 is a git repository")
		assert.Equal(t, now.AddDate(0, 0, -repo_model.AscAgentActivityDays).Unix(), calls[0].GetSince())
		assert.Empty(t, calls[0].GetStates())
		assert.Empty(t, calls[0].GetCreatedBy())
	})

	t.Run("A private repository the viewer cannot read is not named", func(t *testing.T) {
		runs, ok := loadAgentRuns(t, user4, repos)
		require.True(t, ok)
		assert.Equal(t, map[int64]uint64{1: 3}, runs)
		calls := f.took()
		require.Len(t, calls, 1)
		require.Len(t, calls[0].GetRepos(), 1)
		assert.Equal(t, "user2/repo1", calls[0].GetRepos()[0].GetName())
	})

	t.Run("A row without runs has no chip", func(t *testing.T) {
		runs, ok := loadAgentRuns(t, user2, repos[1:])
		require.True(t, ok)
		assert.Equal(t, map[int64]uint64{2: AgentRunsMax}, runs)
		f.took()
	})

	t.Run("asc cannot be asked, or answers the wrong number of counts: no chips", func(t *testing.T) {
		f.set(func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
			return nil, status.Error(codes.Unavailable, "down")
		})
		_, ok := loadAgentRuns(t, user2, repos)
		assert.False(t, ok)
		f.set(func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
			return &ascv1.CountAgentRunsResponse{PerRepo: []uint64{1}}, nil
		})
		_, ok = loadAgentRuns(t, user2, repos)
		assert.False(t, ok)
	})

	t.Run("asc off: nothing asked", func(t *testing.T) {
		f.took()
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		_, ok := loadAgentRuns(t, user2, repos)
		assert.False(t, ok)
		assert.Empty(t, f.took())
	})
}

// The cached counts are read back only when they match the rows.
func TestTheCachedAgentRunCountsRoundTrip(t *testing.T) {
	counts, ok := parseCounts(formatCounts([]uint64{0, 7, 12}), 3)
	assert.True(t, ok)
	assert.Equal(t, []uint64{0, 7, 12}, counts)
	_, ok = parseCounts("1,2", 3)
	assert.False(t, ok)
	_, ok = parseCounts("1,x,3", 3)
	assert.False(t, ok)
}
