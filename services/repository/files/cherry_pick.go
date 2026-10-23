// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forgejo.org/models"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/structs"
)

// CherryPick cherrypicks or reverts a commit to the given repository
func CherryPick(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, revert bool, opts *ApplyDiffPatchOptions) (*structs.FileResponse, error) {
	if err := opts.Validate(ctx, repo, doer); err != nil {
		return nil, err
	}
	message := strings.TrimSpace(opts.Message)

	author, committer := GetAuthorAndCommitterUsers(opts.Author, opts.Committer, doer)

	writer, err := repostore.Default()
	if err != nil {
		return nil, err
	}

	// Get the commit of the original branch
	gitRepo, closeRepo, commit, err := openBranch(ctx, repo, opts.OldBranch)
	if closeRepo != nil {
		defer closeRepo()
	}
	if err != nil {
		return nil, err // Couldn't get a commit for the branch
	}

	// Assigned LastCommitID in opts if it hasn't been set
	if opts.LastCommitID == "" {
		opts.LastCommitID = commit.ID.String()
	} else {
		lastCommitID, err := gitRepo.ConvertToGitID(opts.LastCommitID)
		if err != nil {
			return nil, fmt.Errorf("CherryPick: Invalid last commit ID: %w", err)
		}
		opts.LastCommitID = lastCommitID.String()
		if commit.ID.String() != opts.LastCommitID {
			return nil, models.ErrCommitIDDoesNotMatch{
				GivenCommitID:   opts.LastCommitID,
				CurrentCommitID: opts.LastCommitID,
			}
		}
	}

	picked, err := gitRepo.GetCommit(strings.TrimSpace(opts.Content))
	if err != nil {
		return nil, err
	}

	request := repostore.PickRequest{
		Repo:    repo.StoreRef(),
		Parent:  commit.ID,
		Commit:  picked.ID,
		Meta:    CommitMeta(author, committer, message, opts.Signoff, opts.Dates, CRUDSigner(repo, author)),
		Update:  branchRef(repo, opts.NewBranch, false),
		HookEnv: repo_module.PushingEnvironment(doer, repo),
	}
	var result *repostore.CommitResult
	if revert {
		result, err = writer.Revert(ctx, request)
	} else {
		result, err = writer.CherryPick(ctx, request)
	}
	if err != nil {
		var conflict *repostore.ConflictError
		if errors.As(err, &conflict) {
			return nil, errors.New("failed to merge due to conflicts")
		}
		return nil, convertStoreError(err)
	}

	commit, err = gitRepo.GetCommit(result.Commit.String())
	if err != nil {
		return nil, err
	}

	fileCommitResponse, _ := GetFileCommitResponse(repo, commit) // ok if fails, then will be nil
	verification := GetPayloadCommitVerification(ctx, commit)
	fileResponse := &structs.FileResponse{
		Commit:       fileCommitResponse,
		Verification: verification,
	}

	return fileResponse, nil
}
