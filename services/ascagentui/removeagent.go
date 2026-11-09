// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"strings"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"

	"google.golang.org/grpc"
)

// When an agent is removed from the people of a repository (design board "RepoSettingsAccess"), the runs of that
// agent on the repository that are still going stop. The pull requests it already opened stay open, with the person
// who started each run as their owner: nothing else changes.

// removeAgentReason is the reason recorded on each stopped run.
const removeAgentReason = "The agent was removed from the repository."

// AgentOfUser is the asc agent name of a bot account: the inverse of AgentUserName. A shortened login is matched
// against the agents of the repository's runs. It is "" for a person or an account no agent posts as.
func AgentOfUser(ctx context.Context, u *user_model.User, repoID int64) string {
	if u == nil || !u.IsBot() {
		return ""
	}
	// a run of the repository names the agent best: a shortened login could also be the full login of another name
	var agents []string
	if err := db.GetEngine(ctx).Table(new(issues_model.PullRequestAscRun)).Where("repo_id = ?", repoID).
		Distinct("agent").Find(&agents); err == nil {
		for _, a := range agents {
			if ValidAgentName(a) && AgentUserName(a) == u.Name {
				return a
			}
		}
	}
	if rest, ok := strings.CutPrefix(u.Name, "agent-"); ok && ValidAgentName(rest) && AgentUserName(rest) == u.Name {
		return rest
	}
	return ""
}

// AgentRunsAPI is the part of the Agents service that stopping the runs of an agent needs.
type AgentRunsAPI interface {
	ListRuns(ctx context.Context, in *ascv1.ListAgentRunsRequest, opts ...grpc.CallOption) (*ascv1.ListAgentRunsResponse, error)
	CancelRun(ctx context.Context, in *ascv1.CancelAgentRunRequest, opts ...grpc.CallOption) (*ascv1.CancelAgentRunResponse, error)
}

// StopAgentRuns stops the runs of agent on the repository that are queued, running or waiting for input, and returns
// how many it stopped. It stops as many as it can and returns the first failure.
func StopAgentRuns(ctx context.Context, api AgentRunsAPI, repoName, agent string) (int, error) {
	ref := &ascv1.RepoRef{Name: repoName}
	resp, err := api.ListRuns(ctx, &ascv1.ListAgentRunsRequest{
		Repo: ref, Agent: agent, Limit: 100,
		States: []ascv1.AgentRunState{
			ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING,
			ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
		},
	})
	if err != nil {
		return 0, err
	}
	stopped := 0
	var first error
	for _, r := range resp.GetRuns() {
		if r.GetAgent() != agent {
			continue
		}
		if _, err := api.CancelRun(ctx, &ascv1.CancelAgentRunRequest{Repo: ref, Id: r.GetId(), Reason: removeAgentReason}); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		stopped++
	}
	return stopped, first
}

// StopRemovedAgent stops the runs of the agent of bot on repo as doer, when repo is an asc repository; it reports
// how many runs it stopped. A person, or a repository that is not on asc, stops nothing.
func StopRemovedAgent(ctx context.Context, doer, bot *user_model.User, repo *repo_model.Repository) (int, error) {
	if !setting.Asc.Enabled || repo == nil || repo.StorageBackend != "asc" {
		return 0, nil
	}
	agent := AgentOfUser(ctx, bot, repo.ID)
	if agent == "" {
		return 0, nil
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return 0, err
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return 0, err
	}
	callCtx, done, err := UserCall(ctx, doer, repo)
	defer done()
	if err != nil {
		return 0, err
	}
	return StopAgentRuns(callCtx, ascv1.NewAgentsClient(conn), name, agent)
}
