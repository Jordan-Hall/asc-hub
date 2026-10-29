// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"fmt"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascsync"
)

// The parent of a stacked pull request is the pull request whose head is the git parent of its head: a stack is a
// line of commits, each change on the one below. asc's own answer for the parent change (ResolveChanges) can be
// missing while the push hook runs, so the links are made from git as well: from the push itself, again when the
// stack is viewed or merged, and by the doctor for rows that were left without one.

// OpenAscBridge opens the git repository the hub reads the commits of a repository from: the git bridge of an asc
// repository, the repository itself otherwise. The caller closes it.
func OpenAscBridge(ctx context.Context, repo *repo_model.Repository) (*git.Repository, error) {
	isAsc, err := ascsync.BackedBy(repo)
	if err != nil {
		return nil, fmt.Errorf("storage backend of repository %q: %w", repo.FullName(), err)
	}
	if !isAsc {
		return git.OpenRepository(ctx, repo.RepoPath())
	}
	return git.OpenRepositoryWithBackend(ctx, repo.RepoPath(), git.DefaultBackendName)
}

// LinkAscStackByGit sets the stack parent of each of prs from git: the parent is the open stacked pull request whose
// head is the first parent of the head of the pull request. heads names the heads the caller knows (a push that
// has just made them); the others are read from the pull request refs of the repository. A pull request with no such
// parent is left as it is (the bottom of a stack, or a change whose parent landed). It returns how many it linked.
func LinkAscStackByGit(ctx context.Context, bridge *git.Repository, heads map[int64]string, prs []*issues_model.PullRequest) (int, error) {
	headOf := make(map[int64]string, len(prs))
	byHead := make(map[string]*issues_model.PullRequest, len(prs))
	for _, pr := range prs {
		head := heads[pr.ID]
		if head == "" {
			var err error
			if head, err = bridge.GetRefCommitID(pr.GetGitRefName()); err != nil {
				continue // no ref, no head: nothing to match on
			}
		}
		headOf[pr.ID] = head
		byHead[head] = pr
	}
	linked := 0
	for _, pr := range prs {
		head := headOf[pr.ID]
		if head == "" {
			continue
		}
		id, err := git.NewIDFromString(head)
		if err != nil {
			continue
		}
		commit, err := bridge.GetCommit(id.String())
		if err != nil {
			continue
		}
		parentID, err := commit.ParentID(0)
		if err != nil {
			continue
		}
		parent := byHead[parentID.String()]
		if parent == nil || parent.ID == pr.ID {
			continue
		}
		row, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
		if err != nil {
			return linked, err
		}
		if row == nil || row.StackParentID == parent.ID {
			continue
		}
		if err := issues_model.SetAscStackParent(ctx, pr.ID, parent.ID); err != nil {
			return linked, err
		}
		pr.HeadCommitID = head // the merge base rule reads the head from the pull request
		if err := RecomputeAscStackBase(ctx, pr); err != nil {
			log.Error("asc stack: base of pull request %d: %v", pr.ID, err)
		}
		linked++
	}
	return linked, nil
}

// RepairAscStackLinks links the open stacked pull requests of repo from git (LinkAscStackByGit) and says how many
// it changed. A repository that cannot be read changes nothing.
func RepairAscStackLinks(ctx context.Context, repo *repo_model.Repository) (int, error) {
	prs, err := issues_model.ListOpenAscStackPulls(ctx, repo.ID)
	if err != nil || len(prs) < 2 {
		return 0, err
	}
	bridge, err := OpenAscBridge(ctx, repo)
	if err != nil {
		return 0, err
	}
	defer bridge.Close()
	return LinkAscStackByGit(ctx, bridge, nil, prs)
}
