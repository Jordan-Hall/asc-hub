// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"context"
	"errors"
	"time"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	notify_service "forgejo.org/services/notify"
)

// TrashOrDeleteRepository is what a person's "Delete" does. On an ASC hub the repository goes to the trash for 30
// days and can be restored by its owner (models/repo/repo_trash.go); anywhere else it is deleted at once, as before.
// The administrators' delete and the purge call DeleteRepository, which always deletes.
func TrashOrDeleteRepository(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, notify bool) error {
	if !setting.Asc.Enabled || repo.IsTrashed() {
		return DeleteRepository(ctx, doer, repo, notify)
	}
	if notify {
		// the webhooks of the repository learn of the deletion now; after a restore they are not told again
		notify_service.DeleteRepository(ctx, doer, repo)
	}
	var by int64
	if doer != nil {
		by = doer.ID
	}
	return repo_model.TrashRepository(ctx, repo, by)
}

// purgeBatch bounds the repositories purged by one run of the cron task.
const purgeBatch = 50

// PurgeTrashedRepositories deletes the repositories whose 30 days are over.
func PurgeTrashedRepositories(ctx context.Context) error {
	due, err := repo_model.FindTrashDue(ctx, time.Now(), purgeBatch)
	if err != nil {
		return err
	}
	for _, t := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		repo, err := repo_model.GetRepositoryByID(ctx, t.RepoID)
		switch {
		case err == nil:
			if err := DeleteRepositoryDirectly(ctx, repo.ID, DeleteRepositoryOpts{}); err != nil {
				log.Error("purge of the trashed repository %d: %v", t.RepoID, err)
				continue
			}
		case repo_model.IsErrRepoNotExist(err):
			// it is gone already; only the record is left
		default:
			return errors.Join(err)
		}
		if err := repo_model.DeleteTrashRecord(ctx, t.RepoID); err != nil {
			log.Error("purge of the trashed repository %d: record: %v", t.RepoID, err)
		}
	}
	return nil
}
