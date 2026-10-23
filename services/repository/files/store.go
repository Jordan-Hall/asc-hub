// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"errors"
	"fmt"
	"os"

	"forgejo.org/models"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/repostore"
	asymkey_service "forgejo.org/services/asymkey"
)

// CommitMeta describes a commit that a service writes with the repository writer.
// A zero date means the time of the commit.
func CommitMeta(author, committer *user_model.User, message string, signoff bool, dates *CommitDateOptions, sign repostore.SignFunc) repostore.CommitMeta {
	authorSig, committerSig := author.NewGitSig(), committer.NewGitSig()
	if dates != nil {
		authorSig.When, committerSig.When = dates.Author, dates.Committer
	}
	return repostore.CommitMeta{
		Author:    authorSig,
		Committer: committerSig,
		Message:   message,
		Signoff:   signoff,
		Sign:      sign,
	}
}

// CRUDSigner decides whether a commit that a user makes through the web or the API is signed.
func CRUDSigner(repo *repo_model.Repository, u *user_model.User) repostore.SignFunc {
	return func(ctx context.Context, in repostore.SignInput) (*repostore.SignDecision, error) {
		var (
			sign   bool
			keyID  string
			signer *git.Signature
		)
		// An error means the commit is not signed: the reasons not to sign are errors (asymkey_service.ErrWontSign).
		if in.Base != nil {
			sign, keyID, signer, _ = asymkey_service.SignCRUDAction(ctx, repo.RepoPath(), u, in.GitDir, in.Base.String())
		} else {
			sign, keyID, signer, _ = asymkey_service.SignInitialCommit(ctx, repo.RepoPath(), u)
		}
		if !sign {
			return nil, nil
		}
		decision := &repostore.SignDecision{KeyID: keyID}
		if trust := repo.GetTrustModel(); trust == repo_model.CommitterTrustModel || trust == repo_model.CollaboratorCommitterTrustModel {
			decision.Committer = signer
		}
		return decision, nil
	}
}

// branchRef is the update that publishes a commit to a branch.
func branchRef(repo *repo_model.Repository, branch string, force bool) *repostore.RefUpdate {
	return &repostore.RefUpdate{Repo: repo.StoreRef(), Ref: git.BranchPrefix + branch, Force: force}
}

// openBranch opens the repository and returns the tip of a branch. A branch that does not exist gives
// git.ErrBranchNotExist, and a repository that does not exist gives repo_model.ErrRepoNotExist.
func openBranch(ctx context.Context, repo *repo_model.Repository, branch string) (*git.Repository, func(), *git.Commit, error) {
	if _, err := os.Stat(repo.RepoPath()); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil, repo_model.ErrRepoNotExist{ID: repo.ID, UID: repo.OwnerID, OwnerName: repo.OwnerName, Name: repo.Name}
		}
		return nil, nil, nil, err
	}
	gitRepo, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, repo)
	if err != nil {
		return nil, nil, nil, err
	}
	closeFn := func() { _ = closer.Close() }
	if !gitRepo.IsBranchExist(branch) {
		return gitRepo, closeFn, nil, git.ErrBranchNotExist{Name: branch}
	}
	commit, err := gitRepo.GetBranchCommit(branch)
	if err != nil {
		closeFn()
		return nil, nil, nil, err
	}
	return gitRepo, closeFn, commit, nil
}

// checkLastCommit checks that the branch has not moved since the caller read it. A branch that is
// published to the same name cannot take a commit that does not build on its tip, so the change is
// refused, as pushing it would have been.
func checkLastCommit(gitRepo *git.Repository, tip *git.Commit, lastCommitID string, sameBranch bool) error {
	if lastCommitID == "" || !sameBranch {
		return nil
	}
	last, err := gitRepo.ConvertToGitID(lastCommitID)
	if err != nil {
		return fmt.Errorf("invalid last commit ID: %w", err)
	}
	if last.String() != tip.ID.String() {
		return &git.ErrPushOutOfDate{Err: errors.New("the branch changed since it was read")}
	}
	return nil
}

// convertStoreError turns the errors of the repository writer into the errors the callers of this package handle.
func convertStoreError(err error) error {
	var invalid *repostore.InvalidPathError
	var missing *repostore.FileNotFoundError
	switch {
	case errors.As(err, &invalid):
		return models.ErrFilePathInvalid{Message: invalid.Path, Path: invalid.Path}
	case errors.As(err, &missing):
		return models.ErrRepoFileDoesNotExist{Path: missing.Path}
	}
	return err
}
