// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"

	"forgejo.org/models/db"

	"xorm.io/builder"
)

// CountAscOpenStacks counts the open stacks of each repository of repoIDs, the number the Stacks page of the
// repository lists: every open pull request is in one stack, and a stack is named by its tip, an open pull request
// that is not the stack parent of another open pull request of the same repository. A repository with no open pull
// request has no entry.
func CountAscOpenStacks(ctx context.Context, repoIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(repoIDs))
	if len(repoIDs) == 0 {
		return out, nil
	}
	type openPull struct {
		PullID        int64
		RepoID        int64
		StackParentID int64
	}
	var pulls []openPull
	err := db.GetEngine(ctx).Table("pull_request").
		Select("pull_request.id AS pull_id, issue.repo_id AS repo_id, COALESCE(pull_request_asc_stack.stack_parent_id, 0) AS stack_parent_id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Join("LEFT", "pull_request_asc_stack", "pull_request_asc_stack.pull_id = pull_request.id").
		Where(builder.In("issue.repo_id", repoIDs).And(builder.Eq{"issue.is_closed": false, "issue.is_pull": true})).
		Find(&pulls)
	if err != nil {
		return nil, err
	}
	repoOf := make(map[int64]int64, len(pulls))
	for _, p := range pulls {
		repoOf[p.PullID] = p.RepoID
	}
	parents := make(map[int64]bool)
	for _, p := range pulls {
		// A parent that is closed, merged or in another repository does not end this stack.
		if r, ok := repoOf[p.StackParentID]; ok && p.StackParentID != 0 && r == p.RepoID {
			parents[p.StackParentID] = true
		}
	}
	for _, p := range pulls {
		if !parents[p.PullID] {
			out[p.RepoID]++
		}
	}
	return out, nil
}
