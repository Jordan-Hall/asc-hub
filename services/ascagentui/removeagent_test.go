// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAgentOfUser(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	bot := func(name string) *user_model.User { return &user_model.User{Name: name, Type: user_model.UserTypeBot} }

	assert.Equal(t, "fixer", AgentOfUser(ctx, bot("agent-fixer"), 1))
	assert.Empty(t, AgentOfUser(ctx, &user_model.User{Name: "agent-fixer"}, 1), "a person is no agent")
	assert.Empty(t, AgentOfUser(ctx, bot("builder"), 1), "a bot no agent posts as")
	assert.Empty(t, AgentOfUser(ctx, nil, 1))

	long := strings.Repeat("a", 40)
	short := AgentUserName(long)
	assert.Equal(t, strings.TrimPrefix(short, "agent-"), AgentOfUser(ctx, bot(short), 1), "without a run the login reads as a full name")
	_, err := db.GetEngine(ctx).Insert(&issues_model.PullRequestAscRun{PullID: 9101, RepoID: 1, RunID: 3, Agent: long, StartedBy: "user2"})
	require.NoError(t, err)
	assert.Equal(t, long, AgentOfUser(ctx, bot(short), 1))
	assert.NotEqual(t, long, AgentOfUser(ctx, bot(short), 2), "the runs of another repository do not count")
}

type fakeRunsAPI struct {
	list      *ascv1.ListAgentRunsRequest
	listErr   error
	runs      []*ascv1.AgentRun
	cancelled []uint64
	reasons   []string
	failOn    uint64
}

func (f *fakeRunsAPI) ListRuns(_ context.Context, in *ascv1.ListAgentRunsRequest, _ ...grpc.CallOption) (*ascv1.ListAgentRunsResponse, error) {
	f.list = in
	return &ascv1.ListAgentRunsResponse{Runs: f.runs}, f.listErr
}

func (f *fakeRunsAPI) CancelRun(_ context.Context, in *ascv1.CancelAgentRunRequest, _ ...grpc.CallOption) (*ascv1.CancelAgentRunResponse, error) {
	if in.GetId() == f.failOn {
		return nil, errors.New("asc refused")
	}
	f.cancelled = append(f.cancelled, in.GetId())
	f.reasons = append(f.reasons, in.GetReason())
	return &ascv1.CancelAgentRunResponse{}, nil
}

func TestStopAgentRuns(t *testing.T) {
	ctx := context.Background()
	f := &fakeRunsAPI{runs: []*ascv1.AgentRun{{Id: 1, Agent: "fixer"}, {Id: 2, Agent: "other"}, {Id: 3, Agent: "fixer"}}}
	n, err := StopAgentRuns(ctx, f, "acme/pay", "fixer")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []uint64{1, 3}, f.cancelled, "only the removed agent's runs")
	assert.Equal(t, removeAgentReason, f.reasons[0])
	assert.Equal(t, "acme/pay", f.list.GetRepo().GetName())
	assert.Equal(t, "fixer", f.list.GetAgent())
	assert.ElementsMatch(t, []ascv1.AgentRunState{
		ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING,
		ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
	}, f.list.GetStates(), "only runs that are still going")

	f = &fakeRunsAPI{runs: []*ascv1.AgentRun{{Id: 1, Agent: "fixer"}, {Id: 2, Agent: "fixer"}}, failOn: 1}
	n, err = StopAgentRuns(ctx, f, "acme/pay", "fixer")
	require.Error(t, err)
	assert.Equal(t, 1, n, "one failure does not keep the others going")

	f = &fakeRunsAPI{listErr: errors.New("down")}
	_, err = StopAgentRuns(ctx, f, "acme/pay", "fixer")
	require.Error(t, err)
	assert.Empty(t, f.cancelled)
}

func TestStopRemovedAgentOutsideAsc(t *testing.T) {
	ctx := context.Background()
	bot := &user_model.User{Name: "agent-fixer", Type: user_model.UserTypeBot}
	n, err := StopRemovedAgent(ctx, nil, bot, &repo_model.Repository{ID: 1, StorageBackend: "git"})
	require.NoError(t, err)
	assert.Zero(t, n, "a git repository has no agent runs to stop")
}
