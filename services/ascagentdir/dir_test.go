// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentdir

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
	"forgejo.org/modules/cache"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAgents struct {
	ascv1.UnimplementedAgentsServer
	mu     sync.Mutex
	calls  []*ascv1.SummarizeAgentsRequest
	answer func(*ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error)
}

func (f *fakeAgents) SummarizeAgents(_ context.Context, req *ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	answer := f.answer
	f.mu.Unlock()
	return answer(req)
}

func (f *fakeAgents) took() []*ascv1.SummarizeAgentsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakeAgents) set(answer func(*ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error)) {
	f.mu.Lock()
	f.answer = answer
	f.mu.Unlock()
}

func startAgents(t *testing.T) *fakeAgents {
	t.Helper()
	f := &fakeAgents{}
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

func names(repos []*repo_model.Repository) []string {
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		out = append(out, r.FullName())
	}
	return out
}

func useAsc(t *testing.T, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		_, err := db.GetEngine(db.DefaultContext).ID(id).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
		require.NoError(t, err)
	}
}

// The repositories of a directory are the asc ones whose code the viewer can read.
func TestTheDirectoryReadsOnlyTheAscRepositoriesTheViewerCanRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useAsc(t, 1, 2, 3)
	ctx := context.Background()
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	repos, err := Repos(ctx, user2, 0, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2", "org3/repo3"}, names(repos))
	for _, r := range repos {
		assert.NotNil(t, r.Owner, "the owner is loaded")
	}

	repos, err = Repos(ctx, user4, 0, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"user2/repo1", "org3/repo3"}, names(repos), "user4 reads org3/repo3 through a team, not the private user2/repo2")

	repos, err = Repos(ctx, user2, 2, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2"}, names(repos), "one owner")

	repos, err = Repos(ctx, user2, 2, true)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"user2/repo1", "user2/repo2", "org3/repo3"}, names(repos), "and what the owner can reach")

	repos, err = Repos(ctx, nil, 0, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"user2/repo1"}, names(repos), "a visitor reads public code only")
}

// One call to asc over the repositories; each agent is split by the owner of the repositories where it ran.
func TestTheDirectoryFoldsTheAgentsByOwner(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NoError(t, cache.Init())
	f := startAgents(t)
	useAsc(t, 1, 2, 3)
	ctx := context.Background()
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	var repos []*repo_model.Repository
	for _, id := range []int64{1, 2, 3} {
		r := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: id})
		require.NoError(t, r.LoadOwner(ctx))
		repos = append(repos, r)
	}
	last := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	f.set(func(req *ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
		return &ascv1.SummarizeAgentsResponse{Total: 3, Agents: []*ascv1.AgentSummary{
			{Agent: "review", Runs: 9, Places: []*ascv1.AgentPlace{
				{Repo: 0, Runs: 2, LastRun: last.Add(-time.Hour).Unix(), People: []string{"user2"}},
				{Repo: 1, Runs: 4, Active: 1, LastRun: last.Unix(), People: []string{"user2", "user4"}},
				{Repo: 2, Runs: 3, Active: 2, LastRun: last.Unix(), People: []string{"user5"}},
			}},
			{Agent: "bad name!", Runs: 8, Places: []*ascv1.AgentPlace{{Repo: 0, Runs: 8}}},
			{Agent: "docs", Runs: 3, Places: []*ascv1.AgentPlace{{Repo: 0, Runs: 3}, {Repo: 7, Runs: 1}}},
		}}, nil
	})

	agents, err := Summarize(ctx, repos, "user2")
	require.NoError(t, err)
	calls := f.took()
	require.Len(t, calls, 1)
	var asked []string
	for _, r := range calls[0].GetRepos() {
		asked = append(asked, r.GetName())
	}
	assert.Equal(t, []string{"user2/repo1", "user2/repo2", "org3/repo3"}, asked)
	assert.Equal(t, "user2", calls[0].GetCreatedBy())
	assert.EqualValues(t, MaxAgents, calls[0].GetLimit())

	var handles []string
	for _, a := range agents {
		handles = append(handles, a.Handle())
	}
	assert.Equal(t, []string{"@user2/review", "@org3/review", "@user2/docs"}, handles, "most runs first, then by handle; a bad name is dropped")
	mine := agents[0]
	assert.EqualValues(t, 6, mine.Runs)
	assert.EqualValues(t, 1, mine.Active)
	assert.Equal(t, last.Unix(), mine.LastRun.Unix())
	assert.Equal(t, []string{"user2/repo2", "user2/repo1"}, names(mine.Repos), "most runs first")
	assert.Equal(t, []string{"user2", "user4"}, mine.People)
	assert.Equal(t, "/user2/repo2/asc/agents?agent=review", mine.RunsLink())
	assert.Equal(t, []string{"org3/repo3"}, names(agents[1].Repos))
	assert.EqualValues(t, 3, agents[2].Runs, "an index past the repositories is ignored")

	assert.Len(t, OfOwner(agents, 2), 2)
	assert.Len(t, OfOwner(agents, 3), 1)
	assert.Empty(t, OfOwner(agents, 4))

	t.Run("The answer is kept for a while", func(t *testing.T) {
		again, err := Summarize(ctx, repos, "user2")
		require.NoError(t, err)
		assert.Len(t, again, 3)
		assert.Empty(t, f.took(), "read back from the cache")
	})

	t.Run("No repositories: nothing asked", func(t *testing.T) {
		agents, err := Summarize(ctx, nil, "")
		require.NoError(t, err)
		assert.Empty(t, agents)
		assert.Empty(t, f.took())
	})

	t.Run("asc cannot be asked", func(t *testing.T) {
		f.set(func(*ascv1.SummarizeAgentsRequest) (*ascv1.SummarizeAgentsResponse, error) {
			return nil, status.Error(codes.Unavailable, "down")
		})
		_, err := Summarize(ctx, repos, "user4")
		require.ErrorIs(t, err, ErrUnavailable)
	})

	t.Run("Enabled needs asc, a signed-in viewer and agent features", func(t *testing.T) {
		assert.False(t, Enabled(ctx, nil))
		assert.False(t, Enabled(ctx, user2))
		require.NoError(t, ascagentui.SetFeatures(ctx, user2, true))
		assert.True(t, Enabled(ctx, user2))
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.False(t, Enabled(ctx, user2))
	})
}
