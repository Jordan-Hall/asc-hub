// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
)

// updateHeadByRebaseOnToBase handles updating a PR's head branch by rebasing it on the PR current base branch
func updateHeadByRebaseOnToBase(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User) error {
	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	req, err := newMergeRequest(ctx, pr)
	if err != nil {
		return err
	}
	req.Style = repostore.MergeStyleRebaseUpdate

	sig := doer.NewGitSig()
	req.Meta = repostore.CommitMeta{Author: sig, Committer: sig, Sign: mergeSigner(pr, doer)}

	if pr.BaseRepo.LFSAllowed() && pr.HeadRepo.LFSAllowed() {
		// Now we need to ensure that the head repository contains any LFS objects between the new base and the old mergebase
		// It's questionable about where this should go - either after or before the push
		// I think in the interests of data safety - failures to push to the lfs should prevent
		// the push as you can always re-rebase.
		req.BeforePublish = func(ctx context.Context, in repostore.PublishInput) error {
			oldMergeBase := ""
			if in.MergeBase != nil {
				oldMergeBase = in.MergeBase.String()
			}
			if err := LFSPush(ctx, in.GitDir, in.Base.String(), oldMergeBase, &issues_model.PullRequest{
				HeadRepoID: pr.BaseRepoID,
				BaseRepoID: pr.HeadRepoID,
			}); err != nil {
				log.Error("Unable to push lfs objects between %s and %s up to head branch in %-v: %v", in.Base, oldMergeBase, pr, err)
				return err
			}
			return nil
		}
	}

	// Now determine who the pushing author should be
	var headUser *user_model.User
	if err := pr.HeadRepo.LoadOwner(ctx); err != nil {
		if !user_model.IsErrUserNotExist(err) {
			log.Error("Can't find user: %d for head repository in %-v - %v", pr.HeadRepo.OwnerID, pr, err)
			return err
		}
		log.Error("Can't find user: %d for head repository in %-v - defaulting to doer: %-v - %v", pr.HeadRepo.OwnerID, pr, doer, err)
		headUser = doer
	} else {
		headUser = pr.HeadRepo.Owner
	}

	// Push back to the head repository.
	// TODO: this cause an api call to "/api/internal/hook/post-receive/...",
	//       that prevents us from doint the whole merge in one db transaction
	req.HookEnv = repo_module.FullPushingEnvironment(
		headUser,
		doer,
		pr.HeadRepo,
		pr.HeadRepo.Name,
		pr.ID,
	)
	req.Publish = &repostore.RefUpdate{Repo: req.Head, Ref: pr.GetGitHeadBranchRefName(), Force: true}

	if _, err := writer.Merge(ctx, *req); err != nil {
		return mapMergeError(err, pr)
	}
	return nil
}
