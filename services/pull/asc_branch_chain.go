// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"fmt"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascsync"
)

// Branch-chain pull requests (SPEC section 20.5 flow 6, PLAN unit C46).
//
// A branch-chain pull request is an ordinary pull request whose base branch is the head branch of another
// open pull request in the same repository (plain `git push` of a chain of branches). It is recognised as
// stacked. When the parent merges, the child is retargeted to the parent's base branch, as GitHub does.
// Nothing is rebased on the server: the author's branches are never moved (the "allow server restack"
// option is not part of this unit).
//
// The rule applies only in branch mode: when the repository's branchless setting is on (SPEC 21.5.2) the
// branch names are not the stack and the rule is off. It fails closed: when the setting of an asc-backed
// repository cannot be read, no pull request is retargeted.

// ascBranchless reports whether the base repository of pr runs in branchless mode. A repository that is
// not stored in asc has no such setting and is in branch mode. Tests replace it.
var ascBranchless = func(ctx context.Context, pr *issues_model.PullRequest) (bool, error) {
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return false, err
	}
	backed, err := ascsync.BackedBy(pr.BaseRepo)
	if err != nil || !backed {
		return false, err
	}
	s, err := ascsync.LoadBranchlessSettings(ctx, pr.BaseRepo)
	if err != nil {
		return false, err
	}
	return s.Branchless, nil
}

// ascChangeTarget moves a pull request to another base branch. Tests replace it.
var ascChangeTarget = ChangeTargetBranch

// AscBranchChainChildren returns the open pull requests that are chained on the merged pull request parent:
// same base repository, base branch equal to the parent's head branch, no asc change of their own (those
// follow the stack rules of D8). A parent whose head branch lives in a fork is not a chain parent: its
// branch name says nothing about the branch of that name in the base repository.
func AscBranchChainChildren(ctx context.Context, parent *issues_model.PullRequest) ([]*issues_model.PullRequest, error) {
	if parent == nil || !parent.HasMerged || parent.HeadRepoID != parent.BaseRepoID ||
		parent.HeadBranch == "" || parent.HeadBranch == parent.BaseBranch ||
		parent.Flow != issues_model.PullRequestFlowGithub {
		return nil, nil
	}
	var candidates []*issues_model.PullRequest
	err := db.GetEngine(ctx).
		Where("base_repo_id = ? AND base_branch = ? AND has_merged = ? AND id <> ? AND flow = ?",
			parent.BaseRepoID, parent.HeadBranch, false, parent.ID, issues_model.PullRequestFlowGithub).
		Find(&candidates)
	if err != nil {
		return nil, err
	}
	children := make([]*issues_model.PullRequest, 0, len(candidates))
	for _, c := range candidates {
		if err := c.LoadIssue(ctx); err != nil {
			return nil, err
		}
		// the retarget writes a comment, which needs the repository (and its owner) of the issue
		if err := c.Issue.LoadRepo(ctx); err != nil {
			return nil, err
		}
		if c.Issue.IsClosed {
			continue
		}
		stack, err := issues_model.GetPullRequestAscStack(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if stack != nil {
			continue
		}
		children = append(children, c)
	}
	return children, nil
}

// RetargetAscBranchChain retargets the branch-chain children of the just-merged parent to the parent's base
// branch. It returns the number retargeted. A failure on one child is logged and does not stop the others;
// the first error is returned after all have been tried.
func RetargetAscBranchChain(ctx context.Context, parent *issues_model.PullRequest, doer *user_model.User) (int, error) {
	if parent == nil || !parent.HasMerged {
		return 0, nil
	}
	branchless, err := ascBranchless(ctx, parent)
	if err != nil {
		return 0, fmt.Errorf("asc branch chain: repository mode unknown, not retargeting: %w", err)
	}
	if branchless {
		return 0, nil
	}
	children, err := AscBranchChainChildren(ctx, parent)
	if err != nil {
		return 0, err
	}
	var first error
	done := 0
	for _, c := range children {
		if err := ascChangeTarget(ctx, c, doer, parent.BaseBranch); err != nil {
			log.Error("asc branch chain: retarget %-v to %s: %v", c, parent.BaseBranch, err)
			if first == nil {
				first = err
			}
			continue
		}
		done++
	}
	return done, first
}
