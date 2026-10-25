// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package lfs

import (
	"net/http"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/context"
)

// ascMessage is what the LFS endpoints say about a repository stored in asc.
const ascMessage = "Git LFS is not available for repositories stored in asc"

// refusesLFS reports whether the LFS endpoints must not serve the repository: a repository stored in asc has no
// LFS (SPEC 2.5). A pointer file there is an ordinary blob, and historic LFS content is archived outside asc.
func refusesLFS(repo *repo_model.Repository) bool {
	return repo != nil && !repo.LFSAllowedByBackend()
}

// RefuseAscRepository is middleware for every LFS endpoint (batch, objects, verify and locks): it answers 404,
// which git-lfs reads as "this server has no LFS for the repository", when the repository is stored in asc.
// A repository that does not exist is left to the handlers, which answer as they always did.
func RefuseAscRepository(ctx *context.Context) {
	rc := getRequestContext(ctx)
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, rc.User, rc.Repo)
	if err != nil || !refusesLFS(repo) {
		return
	}
	writeStatusMessage(ctx, http.StatusNotFound, ascMessage)
}
