// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// PullRepoIDsUpdatedBetween returns the repositories with a pull request updated in (from, to], at most limit of
// them. The live Stacks list (event asc-stacks) asks it once per event-source tick.
func PullRepoIDsUpdatedBetween(ctx context.Context, from, to timeutil.TimeStamp, limit int) ([]int64, error) {
	ids := make([]int64, 0, 8)
	err := db.GetEngine(ctx).Table("issue").
		Where("is_pull = ? AND updated_unix > ? AND updated_unix <= ?", true, from, to).
		Distinct("repo_id").OrderBy("repo_id").Limit(limit).Find(&ids)
	return ids, err
}
