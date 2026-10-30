// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
)

// A pull request that the hub opened for the change of an agent run (the run's draft was published for review)
// remembers the run: the pull request page links back to it, and a run is not opened twice. It is a table beside
// pull_request_asc_stack; the pull request is found by (repository, run) or by its id.

// PullRequestAscRun links a pull request to the agent run that made its change.
type PullRequestAscRun struct {
	ID     int64  `xorm:"pk autoincr"`
	PullID int64  `xorm:"UNIQUE NOT NULL"`
	RepoID int64  `xorm:"INDEX(repo_run) NOT NULL"`
	RunID  uint64 `xorm:"INDEX(repo_run) NOT NULL"`
	// Agent is the name of the agent of the run ([a-z0-9-], at most 40 characters).
	Agent string `xorm:"VARCHAR(40) NOT NULL"`
	// StartedBy is the login of the person who started the run when the pull request was opened.
	StartedBy string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
}

// TableName gives the table its name.
func (*PullRequestAscRun) TableName() string { return "pull_request_asc_run" }

func init() {
	db.RegisterModel(new(PullRequestAscRun))
}

// SetPullRequestAscRun records that pr was opened for run runID of agent. It is idempotent for the same run and
// refuses to move a pull request to another run.
func SetPullRequestAscRun(ctx context.Context, pr *PullRequest, runID uint64, agent, startedBy string) error {
	if pr.ID == 0 || runID == 0 || agent == "" || len(agent) > 40 {
		return fmt.Errorf("bad run link")
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		cur, err := GetPullRequestAscRun(ctx, pr.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			_, err = e.Insert(&PullRequestAscRun{PullID: pr.ID, RepoID: pr.BaseRepoID, RunID: runID, Agent: agent, StartedBy: startedBy})
			return err
		}
		if cur.RunID != runID {
			return fmt.Errorf("pull request belongs to another run")
		}
		return nil
	})
}

// GetPullRequestAscRun returns the run link of the pull request with the given id, or nil.
func GetPullRequestAscRun(ctx context.Context, pullID int64) (*PullRequestAscRun, error) {
	r := new(PullRequestAscRun)
	has, err := db.GetEngine(ctx).Where("pull_id = ?", pullID).Get(r)
	if err != nil || !has {
		return nil, err
	}
	return r, nil
}

// ListPullRequestAscRuns returns the run links of the pull requests that were opened for run runID of repoID.
func ListPullRequestAscRuns(ctx context.Context, repoID int64, runID uint64) ([]*PullRequestAscRun, error) {
	var rows []*PullRequestAscRun
	return rows, db.GetEngine(ctx).Where("repo_id = ? AND run_id = ?", repoID, runID).OrderBy("id").Find(&rows)
}
