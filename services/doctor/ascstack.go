// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package doctor

import (
	"context"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	pull_service "forgejo.org/services/pull"
)

// repairAscStackLinks links the pull requests of a stack that were left without a parent: asc did not know the
// parent change when the push hook ran, so the link was never made. The parent is read from git (the open stacked
// pull request whose head is the git parent of the head). Without autofix it only counts them.
func repairAscStackLinks(ctx context.Context, logger log.Logger, autofix bool) error {
	ids, err := issues_model.ListAscStackRepoIDs(ctx)
	if err != nil {
		return err
	}
	linked := 0
	for _, id := range ids {
		repo, err := repo_model.GetRepositoryByID(ctx, id)
		if err != nil {
			continue
		}
		if !autofix {
			logger.Info("%s has stacked pull requests; run with --fix to link them from git and mark the landed ones merged", repo.FullName())
			continue
		}
		if m, err := pull_service.MarkLandedAscStackMerged(ctx, repo); err != nil {
			logger.Warn("Landed stacked pull requests of %s: %v", repo.FullName(), err)
		} else if m > 0 {
			logger.Info("Marked %d landed stacked pull requests of %s as merged", m, repo.FullName())
		}
		n, err := pull_service.RepairAscStackLinks(ctx, repo)
		if err != nil {
			logger.Warn("Stack links of %s: %v", repo.FullName(), err)
			continue
		}
		linked += n
	}
	if autofix {
		logger.Info("Linked %d stacked pull requests to their parents", linked)
	}
	return nil
}

func init() {
	Register(&Check{
		Title:     "Link stacked pull requests to their parents",
		Name:      "asc-stack-links",
		IsDefault: false,
		Run:       repairAscStackLinks,
		Priority:  9,
	})
}
