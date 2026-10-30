// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"net/http"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// A person who writes to the repository opens the changes of an agent run for review from the run page.
func TestAscAgentsOpenPulls(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)
	a, b := changeID(0x10), changeID(0x20)
	run := sampleRun(7, ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
	run.Changes = [][]byte{a, b}
	hub.ok("Agents.GetRun", &ascv1.GetAgentRunResponse{Run: run})

	var created []string
	var posters []string
	resolved := map[string]*ascv1.ChangeInfo{
		string(a): {Change: a, Git: make([]byte, 20)},
		string(b): {Change: b, Git: make([]byte, 20), ParentChange: a},
	}
	next := []int64{2, 5}
	old := ascAgentsPullSeams
	t.Cleanup(func() { ascAgentsPullSeams = old })
	ascAgentsPullSeams = ascagentui.RunPullSeams{
		Resolve: func(_ context.Context, _ grpc.ClientConnInterface, _ string, changes [][]byte) ([]*ascv1.ChangeInfo, error) {
			out := make([]*ascv1.ChangeInfo, len(changes))
			for i, c := range changes {
				out[i] = resolved[string(c)]
			}
			return out, nil
		},
		Stage:   func(context.Context, *repo_model.Repository, string, string) error { return nil },
		Unstage: func(context.Context, *repo_model.Repository, string) error { return nil },
		Commit: func(context.Context, *repo_model.Repository, string) (string, error) {
			return "Retry the call\n\nWith backoff.", nil
		},
		Create: func(ctx context.Context, _ *repo_model.Repository, bot *user_model.User, change, _ string, title, body string, parent int64) (*issues_model.PullRequest, error) {
			pr, err := issues_model.GetPullRequestByID(ctx, next[len(created)])
			if err != nil {
				return nil, err
			}
			created = append(created, title)
			posters = append(posters, bot.Name)
			return pr, issues_model.SetPullRequestAscStack(ctx, pr, change, parent)
		},
	}
	id := map[string]string{":id": "7"}
	back := "/user2/repo1/asc/agents/runs/7"
	post := func(t *testing.T, user int64) agentsPage {
		t.Helper()
		return runAgents(t, agentsReq{method: "POST", path: back + "/pulls", user: user, features: true, params: id}, AscAgentsOpenPulls)
	}

	t.Run("a reader may not", func(t *testing.T) {
		p := post(t, agentsReader)
		assert.Equal(t, http.StatusForbidden, p.status)
		assert.Empty(t, created)
	})

	t.Run("a writer opens the stack as the agent", func(t *testing.T) {
		p := post(t, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Equal(t, back, p.loc)
		assert.Contains(t, p.ctx.Flash.SuccessMsg, "run_open_review_done")
		assert.Equal(t, []string{"Retry the call", "Retry the call"}, created)
		assert.Equal(t, []string{"agent-coder", "agent-coder"}, posters)
		isSystemCredential(t, hub.callsOf("Agents.GetRun")[0])
		link, err := issues_model.GetPullRequestAscRun(t.Context(), 2)
		require.NoError(t, err)
		require.NotNil(t, link)
		assert.EqualValues(t, 7, link.RunID)
		assert.Equal(t, "coder", link.Agent)
		assert.Equal(t, "user2", link.StartedBy)
	})

	t.Run("again opens nothing new", func(t *testing.T) {
		p := post(t, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Contains(t, p.ctx.Flash.InfoMsg, "run_open_review_none")
		assert.Len(t, created, 2)
	})

	t.Run("a change that is not ready is reported", func(t *testing.T) {
		resolved[string(a)] = &ascv1.ChangeInfo{Change: a}
		next = append(next, 5) // the one that is ready is opened again, its pull request having been forgotten above
		_, err := db.GetEngine(t.Context()).Exec("DELETE FROM pull_request_asc_stack")
		require.NoError(t, err)
		p := post(t, agentsOwner)
		require.Equal(t, http.StatusSeeOther, p.status)
		assert.Contains(t, p.ctx.Flash.WarningMsg, "run_open_review_waiting")
	})
}
