// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
	asymkey_service "forgejo.org/services/asymkey"
)

// prHeadRev names the head of a pull request in the head repository: a full ref, or a commit id.
func prHeadRev(pr *issues_model.PullRequest) string {
	if pr.Flow == issues_model.PullRequestFlowGithub {
		return pr.GetGitHeadBranchRefName()
	}
	objectFormat := git.ObjectFormatFromName(pr.BaseRepo.ObjectFormatName)
	if len(pr.HeadCommitID) == objectFormat.FullLength() { // for not created pull request
		return pr.HeadCommitID
	}
	return pr.GetGitRefName()
}

// newMergeRequest loads what a merge of a pull request needs and returns the part of the writer request that
// names the base and the head. The caller sets the rest.
func newMergeRequest(ctx context.Context, pr *issues_model.PullRequest) (*repostore.MergeRequest, error) {
	if err := pr.LoadHeadRepo(ctx); err != nil {
		log.Error("%-v LoadHeadRepo: %v", pr, err)
		return nil, fmt.Errorf("%v LoadHeadRepo: %w", pr, err)
	} else if pr.HeadRepo == nil {
		log.Error("%-v HeadRepo %d does not exist", pr, pr.HeadRepoID)
		return nil, repo_model.ErrRepoNotExist{
			ID: pr.HeadRepoID,
		}
	} else if err := pr.LoadBaseRepo(ctx); err != nil {
		log.Error("%-v LoadBaseRepo: %v", pr, err)
		return nil, fmt.Errorf("%v LoadBaseRepo: %w", pr, err)
	} else if pr.BaseRepo == nil {
		log.Error("%-v BaseRepo %d does not exist", pr, pr.BaseRepoID)
		return nil, repo_model.ErrRepoNotExist{
			ID: pr.BaseRepoID,
		}
	} else if err := pr.HeadRepo.LoadOwner(ctx); err != nil {
		log.Error("%-v HeadRepo.LoadOwner: %v", pr, err)
		return nil, fmt.Errorf("%v HeadRepo.LoadOwner: %w", pr, err)
	} else if err := pr.BaseRepo.LoadOwner(ctx); err != nil {
		log.Error("%-v BaseRepo.LoadOwner: %v", pr, err)
		return nil, fmt.Errorf("%v BaseRepo.LoadOwner: %w", pr, err)
	}

	return &repostore.MergeRequest{
		Base:    pr.BaseRepo.StoreRef(),
		BaseRef: git.BranchPrefix + pr.BaseBranch,
		Head:    pr.HeadRepo.StoreRef(),
		HeadRev: prHeadRev(pr),
	}, nil
}

// mergeSigner decides whether the merge commit of a pull request is signed.
func mergeSigner(pr *issues_model.PullRequest, doer *user_model.User) repostore.SignFunc {
	return func(ctx context.Context, in repostore.SignInput) (*repostore.SignDecision, error) {
		// An error means the commit is not signed: the reasons not to sign are errors (asymkey_service.ErrWontSign).
		sign, keyID, signer, _ := asymkey_service.SignMerge(ctx, pr, doer, in.GitDir, in.Base.String(), in.Head.String())
		if !sign {
			return nil, nil
		}
		decision := &repostore.SignDecision{KeyID: keyID}
		if trust := pr.BaseRepo.GetTrustModel(); trust == repo_model.CommitterTrustModel || trust == repo_model.CollaboratorCommitterTrustModel {
			decision.Committer = signer
		}
		return decision, nil
	}
}

// mapMergeError turns the errors of the repository writer into the errors the callers of this package handle.
func mapMergeError(err error, pr *issues_model.PullRequest) error {
	var (
		conflict *repostore.ConflictError
		mismatch *repostore.HeadMismatchError
		missing  *repostore.RefNotFoundError
	)
	switch {
	case errors.As(err, &conflict):
		style := repo_model.MergeStyle(conflict.Style)
		switch {
		case conflict.Unrelated:
			return models.ErrMergeUnrelatedHistories{Style: style, StdOut: conflict.StdOut, StdErr: conflict.StdErr, Err: conflict.Err}
		case conflict.Diverging:
			return models.ErrMergeDivergingFastForwardOnly{StdOut: conflict.StdOut, StdErr: conflict.StdErr, Err: conflict.Err}
		case conflict.Rebase:
			return models.ErrRebaseConflicts{Style: style, CommitSHA: conflict.CommitSHA, StdOut: conflict.StdOut, StdErr: conflict.StdErr, Err: conflict.Err}
		}
		return models.ErrMergeConflicts{Style: style, StdOut: conflict.StdOut, StdErr: conflict.StdErr, Err: conflict.Err}
	case errors.As(err, &mismatch):
		return models.ErrSHADoesNotMatch{GivenSHA: mismatch.Given, CurrentSHA: mismatch.Current}
	case errors.As(err, &missing):
		return git_model.ErrBranchNotExist{BranchName: pr.HeadBranch}
	}
	return err
}
