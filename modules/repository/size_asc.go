// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	git_model "forgejo.org/models/git"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
)

// Sizing a repository stored in asc walks everything it holds, and right after a large import asc is busy with
// that import: the call can take minutes. The page that asks for the size waits ascSizeWait for it; a slower
// answer is finished in the background, up to ascSizeLimit, and written to the database when it arrives. The
// size shown meanwhile is the one stored before.
var (
	ascSizeWait  = 5 * time.Second
	ascSizeLimit = 10 * time.Minute

	sizing sync.Map // repository id -> struct{}: one size call per repository at a time
)

// ascSize asks asc for the size of a repository; tests replace it.
var ascSize = func(ctx context.Context, repo *repo_model.Repository) (repostore.Size, error) {
	writer, err := repostore.Default()
	if err != nil {
		return repostore.Size{}, err
	}
	return writer.Size(ctx, repo.StoreRef())
}

// updateAscRepoSize is UpdateRepoSize for a repository stored in asc.
func updateAscRepoSize(ctx context.Context, repo *repo_model.Repository) error {
	if _, running := sizing.LoadOrStore(repo.ID, struct{}{}); running {
		return nil // the call in flight stores its answer
	}
	type answer struct {
		size repostore.Size
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		defer sizing.Delete(repo.ID)
		// The call outlives the request that asked for it, and stops with the server.
		bg, cancel := context.WithTimeout(graceful.GetManager().ShutdownContext(), ascSizeLimit)
		defer cancel()
		size, err := ascSize(bg, repo)
		if err == nil {
			err = storeRepoSize(bg, repo.ID, size.Logical)
		}
		if err != nil {
			log.Warn("updateSize of %-v in the background: %v", repo, err)
		}
		done <- answer{size, err}
	}()
	select {
	case a := <-done:
		if a.err != nil {
			return fmt.Errorf("updateSize: %w", a.err)
		}
	case <-time.After(ascSizeWait):
		log.Debug("updateSize of %-v takes longer than %v: it finishes in the background", repo, ascSizeWait)
	case <-ctx.Done():
	}
	return nil
}

// storeRepoSize writes the size of the repository with the size of its LFS objects.
func storeRepoSize(ctx context.Context, repoID, gitSize int64) error {
	lfsSize, err := git_model.GetRepoLFSSize(ctx, repoID)
	if err != nil {
		return fmt.Errorf("GetLFSMetaObjects: %w", err)
	}
	return repo_model.UpdateRepoSize(ctx, repoID, gitSize, lfsSize)
}
