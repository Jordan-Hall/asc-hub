// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascbabysit lets an agent watch the checks of a change (board NewChange, "Ask coder to babysit checks"): when
// a check of the head of the change fails, the hub starts a run of the agent as the person who asked, for the change
// and on its head branch, to fix lint and fixture failures and push a new version. There is one run for each head
// commit and at most MaxRuns for a change. The watch ends when the change is merged or closed, when the person can no
// longer start runs, or when someone who may change the pull request stops it.
package ascbabysit

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/asctrust"

	"github.com/google/uuid"
)

// MaxRuns is the most runs the hub starts for one change.
const MaxRuns = 3

var (
	// ErrBadAgent is a name that is not the name of an agent.
	ErrBadAgent = errors.New("not the name of an agent")
	// ErrFork is a pull request from another repository: the agent cannot push to its branch.
	ErrFork = errors.New("the change comes from another repository")
)

// AscBabysit is a change whose checks an agent watches.
type AscBabysit struct {
	ID          int64              `xorm:"pk autoincr"`
	PullID      int64              `xorm:"UNIQUE NOT NULL"`
	RepoID      int64              `xorm:"INDEX NOT NULL"`
	Agent       string             `xorm:"VARCHAR(40) NOT NULL"`
	DoerID      int64              `xorm:"NOT NULL"`
	Runs        int                `xorm:"NOT NULL DEFAULT 0"`
	LastSHA     string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedUnix timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func init() {
	db.RegisterModel(new(AscBabysit))
}

// TableName is the table of the model.
func (AscBabysit) TableName() string { return "asc_babysit" }

// Left is how many runs the hub may still start.
func (w *AscBabysit) Left() int { return max(0, MaxRuns-w.Runs) }

// Watch makes agent watch the checks of pr for doer. Asking again names the agent and the person anew and starts the
// count of runs again.
func Watch(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, agent string) error {
	if !ascagentui.ValidAgentName(agent) {
		return ErrBadAgent
	}
	if pr.HeadRepoID != pr.BaseRepoID {
		return ErrFork
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		cur := &AscBabysit{}
		has, err := e.Where("pull_id = ?", pr.ID).Get(cur)
		if err != nil {
			return err
		}
		if !has {
			_, err = e.Insert(&AscBabysit{PullID: pr.ID, RepoID: pr.BaseRepoID, Agent: agent, DoerID: doer.ID})
			return err
		}
		cur.Agent, cur.DoerID, cur.Runs, cur.LastSHA = agent, doer.ID, 0, ""
		_, err = e.ID(cur.ID).Cols("agent", "doer_id", "runs", "last_sha").Update(cur)
		return err
	})
}

// Get returns the watch of the pull request, or nil.
func Get(ctx context.Context, pullID int64) (*AscBabysit, error) {
	w := &AscBabysit{}
	has, err := db.GetEngine(ctx).Where("pull_id = ?", pullID).Get(w)
	if err != nil || !has {
		return nil, err
	}
	return w, nil
}

// Stop ends the watch of the pull request. Stopping a change that is not watched does nothing.
func Stop(ctx context.Context, pullID int64) error {
	_, err := db.GetEngine(ctx).Where("pull_id = ?", pullID).Delete(new(AscBabysit))
	return err
}

var (
	// dispatch runs f off the caller's path. Tests make it synchronous.
	dispatch = func(f func()) { go f() }
	// pullsAt are the pull requests whose head is sha. Tests replace it.
	pullsAt = ascsync.PullsAtCommit
	// startRun asks asc for the run as doer. Tests replace it.
	startRun = createRun
)

// NotifyCommitStatus starts the run of the agent that watches a change whose head is sha when the status says a
// check failed. It never blocks the caller.
func NotifyCommitStatus(ctx context.Context, repo *repo_model.Repository, sha string, state api.CommitStatusState, check string) {
	if !setting.Asc.Enabled || repo == nil || sha == "" || !(state.IsFailure() || state.IsError()) {
		return
	}
	if has, err := db.GetEngine(ctx).Exist(&AscBabysit{RepoID: repo.ID}); err != nil || !has {
		return
	}
	ctx = context.WithoutCancel(ctx)
	dispatch(func() {
		prs, err := pullsAt(ctx, repo, sha)
		if err != nil {
			log.Error("asc babysit: pull requests of %s: %v", sha, err)
			return
		}
		for _, pr := range prs {
			kick(ctx, repo, pr, sha, check)
		}
	})
}

// kick starts the run for pr at sha unless the watch already started one for that commit or used up its runs.
func kick(ctx context.Context, repo *repo_model.Repository, pr *issues_model.PullRequest, sha, check string) {
	w, err := Get(ctx, pr.ID)
	if err != nil || w == nil {
		return
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return
	}
	if pr.HasMerged || pr.Issue.IsClosed {
		_ = Stop(ctx, pr.ID)
		return
	}
	doer, err := user_model.GetUserByID(ctx, w.DoerID)
	if err != nil || !doer.IsActive || doer.ProhibitLogin {
		_ = Stop(ctx, pr.ID)
		return
	}
	// The claim: concurrent statuses of one commit start one run.
	res, err := db.GetEngine(ctx).Exec("UPDATE `asc_babysit` SET runs = runs + 1, last_sha = ?, updated_unix = ? WHERE id = ? AND last_sha <> ? AND runs < ?",
		sha, timeutil.TimeStampNow(), w.ID, sha, MaxRuns)
	if err != nil {
		log.Error("asc babysit: claim of pull request %d: %v", pr.ID, err)
		return
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return
	}
	req := &ascv1.CreateAgentRunRequest{
		Agent:  w.Agent,
		Title:  fmt.Sprintf("Fix the failing checks of #%d", pr.Index),
		Task:   task(sha, check),
		Target: pr.HeadBranch,
	}
	if st, err := issues_model.GetPullRequestAscStack(ctx, pr.ID); err == nil && st != nil {
		req.ForChange = ascagentui.ChangeBytes(st.AscChange)
	}
	switch err := startRun(ctx, doer, repo, req); {
	case errors.Is(err, asctrust.ErrForbidden):
		log.Info("asc babysit: user %d can no longer start runs in repository %d: the watch of pull request %d ends", doer.ID, repo.ID, pr.ID)
		_ = Stop(ctx, pr.ID)
	case err != nil:
		log.Warn("asc babysit: run of %s for pull request %d: %v", w.Agent, pr.ID, err)
	default:
		log.Info("asc audit: babysit: repository %d, pull request %d: %s started for user %d at %s", repo.ID, pr.ID, w.Agent, doer.ID, sha)
	}
}

// task is what the run is asked to do.
func task(sha, check string) string {
	check = asctrust.CleanText(check, 255)
	if check == "" {
		check = "a check"
	}
	return fmt.Sprintf("The check %q failed on commit %s of this change. Find out why. When it is a lint or fixture "+
		"failure, fix it and push a new version of the change to its branch. Do not change what the change does; "+
		"when the failure is not one of these, explain it and push nothing.", check, sha)
}

// createRun starts the run with the credential of doer.
func createRun(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, req *ascv1.CreateAgentRunRequest) error {
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return err
	}
	req.Repo = &ascv1.RepoRef{Name: name}
	callCtx, done, err := ascagentui.UserCall(ctx, doer, repo)
	defer done()
	if err != nil {
		return err
	}
	if id, err := uuid.NewV7(); err == nil {
		callCtx = ascclient.WithOpID(callCtx, id)
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(callCtx, asctrust.CallTimeout)
	defer cancel()
	_, err = ascv1.NewAgentsClient(conn).CreateRun(callCtx, req)
	return err
}
