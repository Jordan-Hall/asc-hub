// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/repository"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascbridge"
	notify_service "forgejo.org/services/notify"
)

// Restack on a trunk move (`[stacks] restack_on_main_move`, docs/BUILDOUT.md section 25).
//
// asc records the switch and leaves the restack to the host: a restack rewrites a person's changes, so it has to
// be done in that person's name. When the default branch of an asc repository moves, the repository is queued once
// (a burst of pushes is one job). The job reads the policy in force and, when the switch is on, restacks each open
// stack that targets the default branch, as the person who posted the bottom pull request. RestackAscStack checks
// that person may rewrite every pull request of the stack; a stack they may not, or one that conflicts, is left as
// it is for a person to restack, with its "needs restack" state unchanged. Nothing here acts with more right than
// the poster has.

// ascAutoRestackMax bounds the stacks one trunk move restacks; the rest wait for the next move.
const ascAutoRestackMax = 100

var ascAutoRestackQueue *queue.WorkerPoolQueue[int64]

// ascAutoRestackRun restacks one stack; a seam for tests.
var ascAutoRestackRun = func(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscRestackStackResult, error) {
	return RestackAscStack(ctx, doer, pr, AscRestackOptions{})
}

type ascAutoRestackNotifier struct {
	notify_service.NullNotifier
}

var _ notify_service.Notifier = (*ascAutoRestackNotifier)(nil)

// initAscAutoRestack starts the queue and the notifier that feeds it.
func initAscAutoRestack() error {
	ascAutoRestackQueue = queue.CreateUniqueQueue(graceful.GetManager().ShutdownContext(), "asc_auto_restack", ascAutoRestackHandler)
	if ascAutoRestackQueue == nil {
		return errors.New("unable to create asc_auto_restack queue")
	}
	go graceful.GetManager().RunWithCancel(ascAutoRestackQueue)
	notify_service.RegisterNotifier(&ascAutoRestackNotifier{})
	return nil
}

// PushCommits queues the repository when its default branch moved.
func (*ascAutoRestackNotifier) PushCommits(_ context.Context, _ *user_model.User, repo *repo_model.Repository, opts *repository.PushUpdateOptions, _ *repository.PushCommits) {
	if !ascTrunkMoved(repo, opts) {
		return
	}
	if err := ascAutoRestackQueue.Push(repo.ID); err != nil && !errors.Is(err, queue.ErrAlreadyInQueue) {
		log.Warn("asc auto restack of repository %d: %v", repo.ID, err)
	}
}

// ascTrunkMoved reports whether a push moved the default branch of an asc repository (a delete does not).
func ascTrunkMoved(repo *repo_model.Repository, opts *repository.PushUpdateOptions) bool {
	if !setting.Asc.Enabled || repo == nil || opts == nil || opts.IsDelRef() || !opts.RefFullName.IsBranch() {
		return false
	}
	if opts.RefFullName.BranchName() != repo.DefaultBranch {
		return false
	}
	backed, err := ascStackBacked(repo)
	return err == nil && backed
}

func ascAutoRestackHandler(ids ...int64) []int64 {
	ctx := graceful.GetManager().ShutdownContext()
	for _, id := range ids {
		if err := AscRestackOnTrunkMove(ctx, id); err != nil {
			log.Warn("asc auto restack of repository %d: %v", id, err)
		}
	}
	return nil
}

// AscRestackOnTrunkMove restacks the open stacks of the repository onto its default branch when its policy sets
// `[stacks] restack_on_main_move`. Each restack is logged; a stack that cannot be restacked is logged and
// skipped, and only a failure to read the repository, its policy or its stacks is returned.
func AscRestackOnTrunkMove(ctx context.Context, repoID int64) error {
	repo, err := repo_model.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return err
	}
	if backed, err := ascStackBacked(repo); err != nil || !backed {
		return err
	}
	rules, err := ascbridge.ReadStackRules(ctx, repo.FullName())
	if err != nil {
		return err
	}
	if !rules.RestackOnMainMove {
		return nil
	}
	bottoms, err := issues_model.ListAscStackBottoms(ctx, repo.ID)
	if err != nil {
		return err
	}
	done := 0
	for _, row := range bottoms {
		if done >= ascAutoRestackMax || ctx.Err() != nil {
			break
		}
		pr, err := issues_model.GetPullRequestByID(ctx, row.PullID)
		if err != nil {
			log.Warn("asc auto restack: pull request %d: %v", row.PullID, err)
			continue
		}
		if pr.BaseBranch != repo.DefaultBranch {
			continue
		}
		if open, err := AscPullIsOpen(ctx, pr); err != nil || !open {
			continue
		}
		poster, err := user_model.GetUserByID(ctx, pr.Issue.PosterID)
		if err != nil {
			log.Warn("asc auto restack: poster of %s#%d: %v", repo.FullName(), pr.Index, err)
			continue
		}
		done++
		res, err := ascAutoRestackRun(ctx, poster, pr)
		switch {
		case err != nil:
			// A conflict, a frozen change or a change the poster may not rewrite: a person restacks it.
			log.Info("asc auto restack of %s#%d left for a person: %v", repo.FullName(), pr.Index, err)
		case res.Applied:
			log.Info("asc auto restack of %s#%d as %s: %d moved, %d failed", repo.FullName(), pr.Index, poster.Name, len(res.Moved), len(res.Failed))
		}
	}
	return nil
}
