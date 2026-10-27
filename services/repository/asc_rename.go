// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"
	"strings"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
	ascwriter "forgejo.org/modules/repostore/asc"
	"forgejo.org/modules/setting"
)

// asc authorises a repository by the name "owner/repo" in its own registry (docs/asc/ARCHITECTURE.md, "Trust and
// failure rules"). When Forgejo renames a repository, transfers it or renames its owner, the registry entry must move
// with it, or the old name would still point at the asc repository: a new repository made under that name, on git
// storage, would be reached through the entry of the old one. The moves below run before the directory is moved and
// before the database is changed, a failure stops the whole operation, and the returned function puts the registry
// back when a later step fails.

// ascStored reports whether the repository is stored in asc and asc is on.
func ascStored(repo *repo_model.Repository) bool {
	return setting.Asc.Enabled && repo.GetStorageBackend() == string(gitrepo.StorageBackendAsc)
}

func ascRenameRefs(repo *repo_model.Repository, oldOwner, oldName, newOwner, newName string) (from, to repostore.RepoRef) {
	from = repo.StoreRef()
	from.Owner, from.Name = oldOwner, oldName
	to = from
	to.Owner, to.Name = newOwner, newName
	return from, to
}

// ascRenameRepo moves one repository in asc's registry. It does nothing for a repository that is not stored in asc
// and for a change of capitals only (asc names are lower case). The returned function undoes the move; it is never nil.
func ascRenameRepo(ctx context.Context, repo *repo_model.Repository, oldOwner, oldName, newOwner, newName string) (undo func(), err error) {
	noop := func() {}
	if !ascStored(repo) || (strings.EqualFold(oldOwner, newOwner) && strings.EqualFold(oldName, newName)) {
		return noop, nil
	}
	from, to := ascRenameRefs(repo, oldOwner, oldName, newOwner, newName)
	if err := ascwriter.RenameRegistry(ctx, from, to); err != nil {
		return noop, fmt.Errorf("rename %s/%s in asc: %w", oldOwner, oldName, err)
	}
	return func() {
		if err := ascwriter.RenameRegistry(context.WithoutCancel(ctx), to, from); err != nil {
			log.Critical("Unable to move repository %s/%s back to %s/%s in asc: %v", newOwner, newName, oldOwner, oldName, err)
		}
	}, nil
}

// AscRenameOwnerRepos moves every repository of an owner that is stored in asc to the new owner name, for the rename
// of a user or an organisation. If one move fails the ones already made are undone and the error is returned.
func AscRenameOwnerRepos(ctx context.Context, ownerID int64, oldOwner, newOwner string) (undo func(), err error) {
	noop := func() {}
	if !setting.Asc.Enabled || strings.EqualFold(oldOwner, newOwner) {
		return noop, nil
	}
	var repos []*repo_model.Repository
	if err := db.GetEngine(ctx).Where("owner_id = ? AND storage_backend = ?", ownerID, string(gitrepo.StorageBackendAsc)).Find(&repos); err != nil {
		return noop, fmt.Errorf("list the asc repositories of %s: %w", oldOwner, err)
	}
	var undos []func()
	undoAll := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			undos[i]()
		}
	}
	for _, repo := range repos {
		u, err := ascRenameRepo(ctx, repo, oldOwner, repo.Name, newOwner, repo.Name)
		if err != nil {
			undoAll()
			return noop, err
		}
		undos = append(undos, u)
	}
	return undoAll, nil
}
