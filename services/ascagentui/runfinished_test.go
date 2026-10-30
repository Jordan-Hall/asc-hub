// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// runConn answers Agents.GetRun with one run.
type runConn struct {
	run   *ascv1.AgentRun
	calls int
}

func (c *runConn) Invoke(_ context.Context, method string, _, reply any, _ ...grpc.CallOption) error {
	if method != ascv1.Agents_GetRun_FullMethodName {
		return errors.New("unexpected call " + method)
	}
	c.calls++
	reply.(*ascv1.GetAgentRunResponse).Run = c.run
	return nil
}

func (*runConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, errors.New("no streams")
}

func TestOpenFinishedRunOpensARunThatSucceeded(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := ascRepoForRuns(t)
	name, err := asctrust.RepoName(repo)
	require.NoError(t, err)
	a := changeID(0x11)
	newRun := func(mod func(*ascv1.AgentRun)) *runConn {
		r := &ascv1.AgentRun{
			Id: 7, Repo: name, Agent: "coder-2", Title: "users page", CreatedBy: "user2",
			State: ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED, Changes: [][]byte{a},
			Finished: time.Now().Unix(),
		}
		if mod != nil {
			mod(r)
		}
		return &runConn{run: r}
	}
	seamsFor := func() (*runPullLog, RunPullSeams) {
		l := &runPullLog{message: "Add the user page\n\nBody.", next: []int64{2}, infos: map[string]*ascv1.ChangeInfo{string(a): {Change: a, Git: gitIDOf(0xa1)}}}
		return l, l.seams()
	}

	t.Run("opens the changes as the agent", func(t *testing.T) {
		require.NoError(t, unittest.PrepareTestDatabase())
		l, seams := seamsFor()
		res, err := OpenFinishedRun(t.Context(), newRun(nil), repo, 7, seams, time.Now())
		require.NoError(t, err)
		assert.Equal(t, RunFinishedOpened, res.Outcome)
		assert.Equal(t, 1, res.Opened)
		assert.Equal(t, []string{"agent-coder-2"}, l.posters)
		assert.Contains(t, l.created[0], "[agent run #7](/user2/repo1/asc/agents/runs/7)")

		// asc delivers the event at least once: the second time the pull request is only linked.
		res, err = OpenFinishedRun(t.Context(), newRun(nil), repo, 7, seams, time.Now())
		require.NoError(t, err)
		assert.Equal(t, 0, res.Opened)
		assert.Equal(t, 1, res.Linked)
		assert.Len(t, l.created, 1)
	})

	skips := map[string]func(*ascv1.AgentRun){
		"a run that failed":                    func(r *ascv1.AgentRun) { r.State = ascv1.AgentRunState_AGENT_RUN_STATE_FAILED },
		"a run that was cancelled":             func(r *ascv1.AgentRun) { r.State = ascv1.AgentRunState_AGENT_RUN_STATE_CANCELLED },
		"a run with no change":                 func(r *ascv1.AgentRun) { r.Changes = nil },
		"a starter who is not a user":          func(r *ascv1.AgentRun) { r.CreatedBy = "nobody-here" },
		"a starter who cannot write":           func(r *ascv1.AgentRun) { r.CreatedBy = "user4" },
		"a starter who is an inactive account": func(r *ascv1.AgentRun) { r.CreatedBy = "org3" },
		"a starter that asc does not name":     func(r *ascv1.AgentRun) { r.CreatedBy = "" },
	}
	for why, mod := range skips {
		t.Run("skips "+why, func(t *testing.T) {
			require.NoError(t, unittest.PrepareTestDatabase())
			l, seams := seamsFor()
			res, err := OpenFinishedRun(t.Context(), newRun(mod), repo, 7, seams, time.Now())
			require.NoError(t, err)
			assert.Equal(t, RunFinishedSkipped, res.Outcome)
			assert.NotEmpty(t, res.Reason)
			assert.Empty(t, l.created, "nothing is opened")
			assert.Empty(t, l.staged, "nothing is published")
		})
	}

	t.Run("a run of another repository or id is refused", func(t *testing.T) {
		require.NoError(t, unittest.PrepareTestDatabase())
		_, seams := seamsFor()
		_, err := OpenFinishedRun(t.Context(), newRun(func(r *ascv1.AgentRun) { r.Repo = "someone/else" }), repo, 7, seams, time.Now())
		require.Error(t, err)
		_, err = OpenFinishedRun(t.Context(), newRun(func(r *ascv1.AgentRun) { r.Id = 8 }), repo, 7, seams, time.Now())
		require.Error(t, err)
	})

	t.Run("a change not in the git view yet is waited for, for an hour", func(t *testing.T) {
		require.NoError(t, unittest.PrepareTestDatabase())
		l := &runPullLog{message: "x", next: []int64{2}, infos: map[string]*ascv1.ChangeInfo{string(a): {Change: a}}}
		res, err := OpenFinishedRun(t.Context(), newRun(nil), repo, 7, l.seams(), time.Now())
		require.NoError(t, err)
		assert.Equal(t, RunFinishedWaiting, res.Outcome)
		assert.Equal(t, 1, res.Waiting)

		old := newRun(func(r *ascv1.AgentRun) { r.Finished = time.Now().Add(-2 * time.Hour).Unix() })
		res, err = OpenFinishedRun(t.Context(), old, repo, 7, l.seams(), time.Now())
		require.NoError(t, err)
		assert.Equal(t, RunFinishedOpened, res.Outcome, "after the hour the hub stops asking; the button remains")
		assert.Equal(t, 1, res.Waiting)
	})

	t.Run("a repository that takes no pull requests is skipped before asc is asked", func(t *testing.T) {
		require.NoError(t, unittest.PrepareTestDatabase())
		archived := *repo
		archived.IsArchived = true
		conn := newRun(nil)
		_, seams := seamsFor()
		res, err := OpenFinishedRun(t.Context(), conn, &archived, 7, seams, time.Now())
		require.NoError(t, err)
		assert.Equal(t, RunFinishedSkipped, res.Outcome)
		assert.Zero(t, conn.calls)
	})
}
