// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"errors"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
)

// A deleted repository waits in the trash for TrashDays before it is purged, and its owner can restore it until then.
// Trashing closes the repository (GetUserRepoPermission answers no access to everyone), makes it private and takes it
// out of every list; its name stays taken, so a restore never meets a new repository of the same name.

// TrashDays is how long a deleted repository can be restored.
const TrashDays = setting.AscTrashDays

// ErrNotTrashed is returned for a repository that is not in the trash.
var ErrNotTrashed = errors.New("the repository is not in the trash")

// RepoTrash is the record of a repository in the trash.
type RepoTrash struct {
	ID          int64              `xorm:"pk autoincr"`
	RepoID      int64              `xorm:"UNIQUE NOT NULL"`
	OwnerID     int64              `xorm:"INDEX NOT NULL"`
	DeletedByID int64              `xorm:"NOT NULL DEFAULT 0"`
	WasPrivate  bool               `xorm:"NOT NULL DEFAULT false"`
	DeletedUnix timeutil.TimeStamp `xorm:"NOT NULL"`
	PurgeUnix   timeutil.TimeStamp `xorm:"INDEX NOT NULL"`
}

func init() {
	db.RegisterModel(new(RepoTrash))
}

// TrashRepository moves a repository to the trash. deletedBy is the person who deleted it (0 for the system).
func TrashRepository(ctx context.Context, repo *Repository, deletedBy int64) error {
	if repo.IsTrashed() {
		return nil
	}
	now := timeutil.TimeStampNow()
	return db.WithTx(ctx, func(ctx context.Context) error {
		if err := db.Insert(ctx, &RepoTrash{
			RepoID: repo.ID, OwnerID: repo.OwnerID, DeletedByID: deletedBy, WasPrivate: repo.IsPrivate,
			DeletedUnix: now, PurgeUnix: timeutil.TimeStamp(time.Now().AddDate(0, 0, TrashDays).Unix()),
		}); err != nil {
			return err
		}
		repo.Status = RepositoryTrashed
		repo.IsPrivate = true
		_, err := db.GetEngine(ctx).ID(repo.ID).Cols("status", "is_private").Update(repo)
		return err
	})
}

// RestoreRepository takes a repository out of the trash and gives back its visibility.
func RestoreRepository(ctx context.Context, repoID int64) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		t, err := GetRepoTrash(ctx, repoID)
		if err != nil {
			return err
		}
		if _, err := db.GetEngine(ctx).ID(repoID).Cols("status", "is_private").Update(&Repository{Status: RepositoryReady, IsPrivate: t.WasPrivate}); err != nil {
			return err
		}
		// xorm skips zero values without Cols; the two columns above are named, so Ready (0) and public (false) are written
		_, err = db.GetEngine(ctx).ID(t.ID).Delete(new(RepoTrash))
		return err
	})
}

// GetRepoTrash returns the trash record of a repository, or ErrNotTrashed.
func GetRepoTrash(ctx context.Context, repoID int64) (*RepoTrash, error) {
	t := new(RepoTrash)
	has, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Get(t)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotTrashed
	}
	return t, nil
}

// FindTrashOfOwners returns the trash records of the repositories of the given owners, the soonest purge first.
func FindTrashOfOwners(ctx context.Context, ownerIDs []int64) ([]*RepoTrash, error) {
	var out []*RepoTrash
	if len(ownerIDs) == 0 {
		return out, nil
	}
	return out, db.GetEngine(ctx).In("owner_id", ownerIDs).OrderBy("purge_unix ASC").Find(&out)
}

// FindTrashDue returns the records whose purge time has come, at most limit.
func FindTrashDue(ctx context.Context, now time.Time, limit int) ([]*RepoTrash, error) {
	var out []*RepoTrash
	return out, db.GetEngine(ctx).Where("purge_unix <= ?", now.Unix()).OrderBy("purge_unix ASC").Limit(limit).Find(&out)
}

// DeleteTrashRecord removes the record of a repository that is being purged.
func DeleteTrashRecord(ctx context.Context, repoID int64) error {
	_, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Delete(new(RepoTrash))
	return err
}
