// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"

	"google.golang.org/grpc"
)

// A run that ended opens itself for review (docs/AGENTS-UI.md). asc serve tells the hub when an agent run ends with
// changes (the outbox effect agent_run_finished, delivered at least once); the hub then does what the "Open for
// review" button on the run page does: a pull request for each change of the run that has none. The button stays as
// the way to do it by hand, and the way to open a run that ended in any other state.
//
// The run is read again from asc, so the hub acts on what asc says and never on the body of the call alone. Only a
// run that succeeded is opened. The person who started the run must still be able to write to the repository: a
// run is opened with the same right the button asks for. Everything here is idempotent, because a change with an open
// pull request is linked, not opened again.

// RunFinishedGiveUp is how long after a run ended the hub keeps asking asc to deliver the event again because a change
// has no git id yet. After it, the hub answers that it is done and the run page's button remains.
const RunFinishedGiveUp = time.Hour

// The outcomes of OpenFinishedRun that are not an error.
const (
	RunFinishedOpened  = "opened"  // pull requests were opened or already existed
	RunFinishedSkipped = "skipped" // there was nothing to do, or the hub is not allowed to
	RunFinishedWaiting = "waiting" // a change is not in the git view yet: deliver the event again later
)

// RunFinishedResult says what OpenFinishedRun did.
type RunFinishedResult struct {
	Outcome string
	Reason  string // for Skipped: why, in words for the log
	Opened  int
	Linked  int
	Failed  int
	Waiting int
}

// OpenFinishedRun opens the changes of the finished run id of repo for review. conn is the hub's connection to asc;
// seams are the parts that need a git view (DefaultRunPullSeams in production).
func OpenFinishedRun(ctx context.Context, conn grpc.ClientConnInterface, repo *repo_model.Repository, id uint64, seams RunPullSeams, now time.Time) (*RunFinishedResult, error) {
	skip := func(why string) (*RunFinishedResult, error) {
		return &RunFinishedResult{Outcome: RunFinishedSkipped, Reason: why}, nil
	}
	if repo.IsArchived || !repo.AllowsPulls(ctx) {
		return skip("the repository takes no pull requests")
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), 30*time.Second)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: asctrust.RepoRef(name), Id: id})
	if err != nil {
		return nil, err
	}
	run := resp.GetRun()
	if run.GetId() != id || run.GetRepo() != name {
		return nil, fmt.Errorf("%w: run", asctrust.ErrBadAnswer)
	}
	if run.GetState() != ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED {
		return skip("the run did not succeed")
	}
	if len(run.GetChanges()) == 0 {
		return skip("the run made no change")
	}
	starter, err := user_model.GetUserByName(ctx, run.GetCreatedBy())
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			return skip("the person who started the run is not a user of this hub")
		}
		return nil, err
	}
	if !starter.IsActive || starter.ProhibitLogin {
		return skip("the person who started the run cannot sign in")
	}
	perm, err := access.GetUserRepoPermission(ctx, repo, starter)
	if err != nil {
		return nil, err
	}
	if !perm.CanWrite(unit.TypeCode) {
		return skip("the person who started the run cannot write to the repository")
	}

	results, err := OpenRunPulls(ctx, conn, repo, RunPullRun{
		ID:        id,
		Agent:     SafeName(run.GetAgent()),
		Title:     CleanLine(run.GetTitle(), MaxTitleBytes),
		CreatedBy: run.GetCreatedBy(),
		Changes:   run.GetChanges(),
		RunURL:    repo.Link() + "/asc/agents/runs/" + strconv.FormatUint(id, 10),
	}, seams)
	if err != nil {
		if errors.Is(err, ErrAgentNameTaken) {
			return skip("the account the agent would post as belongs to a person")
		}
		return nil, err
	}
	out := &RunFinishedResult{Outcome: RunFinishedOpened}
	for _, r := range results {
		switch r.State {
		case RunPullOpened:
			out.Opened++
		case RunPullLinked, RunPullLanded:
			out.Linked++
		case RunPullWaiting:
			out.Waiting++
		default:
			out.Failed++
		}
	}
	if out.Waiting > 0 {
		finished := Time(run.GetFinished())
		if finished.IsZero() || now.Sub(finished) < RunFinishedGiveUp {
			out.Outcome = RunFinishedWaiting
		}
	}
	return out, nil
}
