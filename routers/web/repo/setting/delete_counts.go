// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"forgejo.org/services/context"
)

// prepareDeleteCounts sets DeleteChangeCount, the number of changes on the default branch, for the delete dialog
// ("Removes 1,204 changes, 14 releases and 23 issues"). A repository without a readable default branch leaves it out
// and the dialog then names the other counts only.
func prepareDeleteCounts(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if repo == nil || repo.IsEmpty || ctx.Repo.GitRepo == nil {
		return
	}
	commit, err := ctx.Repo.GitRepo.GetBranchCommit(repo.DefaultBranch)
	if err != nil {
		return
	}
	if n, err := commit.CommitsCount(); err == nil && n > 0 {
		ctx.Data["DeleteChangeCount"] = n
	}
}
