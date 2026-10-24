// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/gitrepo"
)

// The merge_base rule of a stacked pull request (SPEC section 20.5, PLAN unit C34).
//
// A pull request with an asc change whose parent change is unlanded has the trunk as its base branch, but its
// diff base is its own parent commit: merge_base is the first parent of the head. The diff, the commits tab,
// review, "viewed" and outdated-comment logic all key off merge_base, so they show only the change. Forgejo
// recomputes merge_base on every push and base-branch update (testPatch); a pinned pull request takes this
// rule there and is never recomputed from the base branch. Once the parent has merged, or for the bottom of
// a stack, the ordinary merge base with the trunk applies.

// ascHeadFirstParent returns the first parent of the head commit of pr. Tests replace it.
var ascHeadFirstParent = func(ctx context.Context, pr *issues_model.PullRequest) (string, error) {
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return "", err
	}
	gitRepo, err := gitrepo.OpenRepository(ctx, pr.BaseRepo)
	if err != nil {
		return "", err
	}
	defer gitRepo.Close()
	commit, err := gitRepo.GetCommit(pr.HeadCommitID)
	if err != nil {
		return "", err
	}
	id, err := commit.ParentID(0)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// AscStackMergeBase returns the merge_base that the rule of a stacked pull request gives pr, and whether
// the rule applies. It does not apply to a pull request without an asc change, to the bottom of a stack, or
// to a change whose parent pull request has merged. pr.HeadCommitID must be the head to compute for.
func AscStackMergeBase(ctx context.Context, pr *issues_model.PullRequest) (string, bool, error) {
	if pr == nil || pr.ID == 0 {
		return "", false, nil
	}
	stack, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil {
		return "", false, err
	}
	if stack == nil || stack.StackParentID == 0 {
		return "", false, nil
	}
	parent, err := issues_model.GetPullRequestByID(ctx, stack.StackParentID)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			// A parent that is gone cannot hold the stack up: fall back to the trunk's merge base.
			return "", false, nil
		}
		return "", false, err
	}
	if parent.HasMerged {
		return "", false, nil
	}
	if pr.HeadCommitID == "" {
		return "", false, errors.New("asc stack merge base: no head commit")
	}
	base, err := ascHeadFirstParent(ctx, pr)
	if err != nil {
		return "", false, fmt.Errorf("asc stack merge base: %w", err)
	}
	return base, true, nil
}

// applyAscStackBase is the patch in testPatch: when the stack rule applies it replaces the merge base that
// was just computed from the base branch.
func applyAscStackBase(ctx context.Context, pr *issues_model.PullRequest) error {
	base, pinned, err := AscStackMergeBase(ctx, pr)
	if err != nil {
		return err
	}
	if pinned {
		pr.MergeBase = base
	}
	return nil
}

// RecomputeAscStackBase applies the rule to pr and stores the merge base when it applies. AGit calls it after
// it has recorded the stack link; the merge of a parent calls it for the children to return to the trunk's
// merge base through the ordinary check.
func RecomputeAscStackBase(ctx context.Context, pr *issues_model.PullRequest) error {
	if err := applyAscStackBase(ctx, pr); err != nil {
		return err
	}
	_, err := db.GetEngine(ctx).ID(pr.ID).Cols("merge_base").Update(pr)
	return err
}
