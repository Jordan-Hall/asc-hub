// Copyright 2023 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT
package user

import (
	"context"

	model "forgejo.org/models"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	actions_service "forgejo.org/services/actions"

	"xorm.io/builder"
)

// BlockUser adds a blocked user entry for userID to block blockID.
// TODO: Figure out if instance admins should be immune to blocking.
// TODO: Add more mechanism like removing blocked user as collaborator on
// repositories where the user is an owner.
func BlockUser(ctx context.Context, userID, blockID int64) error {
	if userID == blockID || user_model.IsBlocked(ctx, userID, blockID) {
		return nil
	}

	ctx, committer, err := db.TxContext(ctx)
	if err != nil {
		return err
	}
	defer committer.Close()

	// Add the blocked user entry.
	_, err = db.GetEngine(ctx).Insert(&user_model.BlockedUser{UserID: userID, BlockID: blockID})
	if err != nil {
		return err
	}

	// Unfollow the user from the block's perspective.
	err = user_model.UnfollowUser(ctx, blockID, userID)
	if err != nil {
		return err
	}

	// Unfollow the user from the doer's perspective.
	err = user_model.UnfollowUser(ctx, userID, blockID)
	if err != nil {
		return err
	}

	// Blocked user unwatch all repository owned by the doer.
	repoIDs, err := repo_model.GetWatchedRepoIDsOwnedBy(ctx, blockID, userID)
	if err != nil {
		return err
	}

	err = repo_model.UnwatchRepos(ctx, blockID, repoIDs)
	if err != nil {
		return err
	}

	// Remove blocked user as collaborator from repositories the user owns as an
	// individual.
	collabsID, err := repo_model.GetCollaboratorWithUser(ctx, userID, blockID)
	if err != nil {
		return err
	}

	_, err = db.GetEngine(ctx).In("id", collabsID).Delete(&repo_model.Collaboration{})
	if err != nil {
		return err
	}

	// Remove pending repository transfers, and set the status on those repository
	// back to ready.
	pendingTransfersIDs, err := model.GetPendingTransferIDs(ctx, userID, blockID)
	if err != nil {
		return err
	}

	// Use a subquery instead of a JOIN, because not every database supports JOIN
	// on a UPDATE query.
	_, err = db.GetEngine(ctx).Table("repository").
		In("id", builder.Select("repo_id").From("repo_transfer").Where(builder.In("id", pendingTransfersIDs))).
		Cols("status").
		Update(&repo_model.Repository{Status: repo_model.RepositoryReady})
	if err != nil {
		return err
	}

	_, err = db.GetEngine(ctx).In("id", pendingTransfersIDs).Delete(&model.RepoTransfer{})
	if err != nil {
		return err
	}

	err = db.Iterate(ctx, builder.Eq{"owner_id": userID}, func(ctx context.Context, repo *repo_model.Repository) error {
		return actions_service.RevokeTrust(ctx, repo.ID, blockID)
	})
	if err != nil {
		return err
	}

	if err := dropPendingReviewRequests(ctx, userID, blockID); err != nil {
		return err
	}

	return committer.Commit()
}

// dropPendingReviewRequests removes the review requests that still wait between the person who blocks and the one who is
// blocked, on open pull requests: the blocked person asked to review a pull request in a repository the blocker owns, and
// the blocker asked to review a pull request that the blocked person opened. Each removal leaves the usual entry in the
// timeline of the pull request, made in the name of the blocker.
func dropPendingReviewRequests(ctx context.Context, userID, blockID int64) error {
	openPulls := func(cond builder.Cond) *builder.Builder {
		return builder.Select("issue.id").From("issue").
			InnerJoin("repository", "repository.id = issue.repo_id").
			Where(builder.And(builder.Eq{"issue.is_pull": true, "issue.is_closed": false}, cond))
	}
	var requests []*issues_model.Review
	err := db.GetEngine(ctx).Where(builder.And(
		// requests made to a person of this hub only: the removal below looks for exactly these, and when it finds none it
		// ends its (nested) transaction early, which would take the block with it
		builder.Eq{"type": issues_model.ReviewTypeRequest, "original_author_id": 0},
		builder.Or(
			builder.And(builder.Eq{"reviewer_id": blockID}, builder.In("issue_id", openPulls(builder.Eq{"repository.owner_id": userID}))),
			builder.And(builder.Eq{"reviewer_id": userID}, builder.In("issue_id", openPulls(builder.Eq{"issue.poster_id": blockID}))),
		),
	)).Find(&requests)
	if err != nil {
		return err
	}
	if len(requests) == 0 {
		return nil
	}
	blocker, err := user_model.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	for _, r := range requests {
		issue, err := issues_model.GetIssueByID(ctx, r.IssueID)
		if err != nil {
			return err
		}
		if err := issue.LoadRepo(ctx); err != nil {
			return err
		}
		reviewer, err := user_model.GetUserByID(ctx, r.ReviewerID)
		if err != nil {
			return err
		}
		if _, err := issues_model.RemoveReviewRequest(ctx, issue, reviewer, blocker); err != nil {
			return err
		}
	}
	return nil
}
