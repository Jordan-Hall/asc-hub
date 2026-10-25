// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"errors"

	repo_model "forgejo.org/models/repo"
	system_model "forgejo.org/models/system"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
	ascwriter "forgejo.org/modules/repostore/asc"
)

// ascRepoDoctor checks a repository stored in asc with RepoAdmin.verify: the closure of every ref exists and the git
// map round-trips. A failure is reported as a repository notice, like a failed git fsck, and is not an error of the
// caller's loop. While the instance does not write through asc there is nothing to ask and the repository is skipped.
func ascRepoDoctor(ctx context.Context, repo *repo_model.Repository) error {
	if !repostore.AscWriterEnabled() {
		return nil
	}
	report, err := ascwriter.Verify(ctx, repo.StoreRef())
	if err != nil {
		if errors.Is(err, ascwriter.ErrNotConfigured) {
			return err
		}
		log.Warn("Failed to health check repository (%-v): %v", repo, err)
		if err = system_model.CreateRepositoryNotice("Failed to health check repository (%s): %v", repo.FullName(), err); err != nil {
			log.Error("CreateRepositoryNotice: %v", err)
		}
		return nil
	}
	log.Trace("Health check of repository %-v: %s", repo, report)
	return nil
}
