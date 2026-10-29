// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package doctor

import (
	"context"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
)

// asc protects one branch (path rules, secret scanning, review): the trunk it holds for the repository. A repository
// that was imported before the hub told asc its default branch, or whose default branch was changed in the hub before
// it did, has the wrong branch protected and the real one open to anything. This check finds them and, with --fix,
// tells asc. It is idempotent (a repository whose trunk is right is not touched) and asc audits every change as
// repo.default_branch.
func checkAscDefaultBranches(ctx context.Context, logger log.Logger, autofix bool) error {
	if !setting.Asc.Enabled {
		logger.Info("asc is not enabled: nothing to check")
		return nil
	}
	checked, wrong, fixed, failed := 0, 0, 0, 0
	err := iterateRepositories(ctx, func(repo *repo_model.Repository) error {
		if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) || repo.IsEmpty || repo.DefaultBranch == "" || repo.Status != repo_model.RepositoryReady {
			return nil
		}
		checked++
		trunk, err := ascsync.TrunkOf(ctx, repo)
		if err != nil {
			failed++
			logger.Warn("%s/%s: cannot read the trunk from asc: %v", repo.OwnerName, repo.Name, err)
			return nil
		}
		if trunk == repo.DefaultBranch {
			return nil
		}
		wrong++
		logger.Warn("%s/%s: asc protects %q but the default branch is %q", repo.OwnerName, repo.Name, trunk, repo.DefaultBranch)
		if !autofix {
			return nil
		}
		if err := ascsync.SetDefaultBranch(ctx, repo, repo.DefaultBranch); err != nil {
			failed++
			logger.Warn("%s/%s: asc did not take %q as the default branch: %v", repo.OwnerName, repo.Name, repo.DefaultBranch, err)
			return nil
		}
		fixed++
		return nil
	})
	if err != nil {
		logger.Critical("Error when checking the default branches in asc: %v", err)
		return err
	}
	switch {
	case wrong == 0 && failed == 0:
		logger.Info("All %d asc repositories protect their default branch", checked)
	case autofix:
		logger.Info("Out of %d asc repositories, %d protected the wrong branch: %d are fixed, %d could not be checked or fixed", checked, wrong, fixed, failed)
	default:
		logger.Critical("Out of %d asc repositories, %d protect a branch that is not their default branch (run with --fix), %d could not be checked", checked, wrong, failed)
	}
	return nil
}

func init() {
	Register(&Check{
		Title:     "Check that asc protects the default branch of every asc repository",
		Name:      "asc-default-branch",
		IsDefault: false,
		Run:       checkAscDefaultBranches,
		Priority:  8,
	})
}
