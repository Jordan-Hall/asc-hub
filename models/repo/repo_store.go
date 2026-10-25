// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
)

// StoreRef addresses the repository in a repostore.Writer.
func (repo *Repository) StoreRef() repostore.RepoRef {
	return repostore.RepoRef{
		ID:           repo.ID,
		Owner:        repo.OwnerName,
		Name:         repo.Name,
		ObjectFormat: objectFormatName(repo.ObjectFormatName),
		Path:         repo.RepoPath(),

		StorageBackend: repo.GetStorageBackend(),
	}
}

// WikiStoreRef addresses the wiki repository in a repostore.Writer. A wiki is a repository of its own.
func (repo *Repository) WikiStoreRef() repostore.RepoRef {
	return repostore.RepoRef{
		ID:           repo.ID,
		Owner:        repo.OwnerName,
		Name:         repo.Name + ".wiki",
		ObjectFormat: objectFormatName(repo.ObjectFormatName),
		Path:         repo.WikiPath(),

		StorageBackend: repo.GetStorageBackend(),
	}
}

// objectFormatName gives repositories that predate the column the format they were made with.
func objectFormatName(name string) string {
	if name == "" {
		return git.Sha1ObjectFormat.Name()
	}
	return name
}
