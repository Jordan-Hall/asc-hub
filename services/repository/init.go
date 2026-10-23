// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	asymkey_service "forgejo.org/services/asymkey"
)

// initialCommitSigner decides whether the first commit of a repository is signed.
func initialCommitSigner(repo *repo_model.Repository, u *user_model.User) repostore.SignFunc {
	return func(ctx context.Context, _ repostore.SignInput) (*repostore.SignDecision, error) {
		// An error means the commit is not signed: the reasons not to sign are errors (asymkey_service.ErrWontSign).
		sign, keyID, signer, _ := asymkey_service.SignInitialCommit(ctx, repo.RepoPath(), u)
		if !sign {
			return nil, nil
		}
		decision := &repostore.SignDecision{KeyID: keyID}
		if trust := repo.GetTrustModel(); trust == repo_model.CommitterTrustModel || trust == repo_model.CollaboratorCommitterTrustModel {
			// need to set the committer to the KeyID owner
			decision.Committer = signer
		}
		return decision, nil
	}
}

// initRepoCommit makes the first commit of a repository, with the given files, and publishes it as the default branch.
func initRepoCommit(ctx context.Context, repo *repo_model.Repository, u *user_model.User, defaultBranch string, ops []repostore.FileOp) error {
	writer, err := repostore.Default()
	if err != nil {
		return err
	}

	if len(defaultBranch) == 0 {
		defaultBranch = setting.Repository.DefaultBranch
	}

	sig := u.NewGitSig()
	storeRef := repo.StoreRef()
	if _, err := writer.CreateCommit(ctx, repostore.CommitRequest{
		Repo: storeRef,
		Ops:  ops,
		Meta: repostore.CommitMeta{
			Author:    sig,
			Committer: sig,
			Message:   "Initial commit",
			Sign:      initialCommitSigner(repo, u),
		},
		Update:  &repostore.RefUpdate{Repo: storeRef, Ref: git.BranchPrefix + defaultBranch},
		HookEnv: repo_module.InternalPushingEnvironment(u, repo),
	}); err != nil {
		return fmt.Errorf("initial commit: %w", err)
	}
	return nil
}
