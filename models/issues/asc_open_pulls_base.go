// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"

	"forgejo.org/models/db"
)

// AscOpenPullsByBase counts the open pull requests of the repository by the branch they target.
func AscOpenPullsByBase(ctx context.Context, repoID int64) (map[string]int64, error) {
	var rows []struct {
		BaseBranch string
		N          int64
	}
	err := db.GetEngine(ctx).Table("pull_request").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where("pull_request.base_repo_id = ? AND issue.is_closed = ?", repoID, false).
		GroupBy("pull_request.base_branch").
		Select("pull_request.base_branch AS base_branch, COUNT(*) AS n").
		Find(&rows)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.BaseBranch] = r.N
	}
	return out, nil
}
