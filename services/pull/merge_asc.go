// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/timeutil"
)

// writtenThroughAsc reports whether the repository is stored in asc and the instance writes it through asc.v1
// (Stage 4: [asc] WRITER = asc). Before that, an asc repository is written by pushes into its git bridge.
func writtenThroughAsc(repo *repo_model.Repository) bool {
	return repostore.AscWriterEnabled() && repo.GetStorageBackend() == repostore.StorageBackendAsc
}

// markMergedThroughAsc records a pull request as merged after asc made the merge. On a git repository the
// post-receive hook of the merge push does this (it is the only call that carries the pull request id); asc's own
// post-receive call is a plain push, so the merge itself has to say it.
func markMergedThroughAsc(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, commit string) error {
	if doer == nil {
		return errors.New("markMergedThroughAsc: no merger")
	}
	pr.MergedCommitID = commit
	pr.MergedUnix = timeutil.TimeStampNow()
	pr.Merger = doer
	pr.MergerID = doer.ID
	return db.WithTx(ctx, func(ctx context.Context) error {
		// Removing an auto merge pull and ignore if not exist
		if err := pull_model.DeleteScheduledAutoMerge(ctx, pr.ID); err != nil && !db.IsErrNotExist(err) {
			return fmt.Errorf("DeleteScheduledAutoMerge[%d]: %w", pr.ID, err)
		}
		if _, err := pr.SetMerged(ctx); err != nil {
			return fmt.Errorf("SetMerged[%d]: %w", pr.ID, err)
		}
		return nil
	})
}

// setPullHeadThroughAsc points refs/pull/<n>/head of a pull request at a revision, with asc: the closure is copied
// from the repository the revision is in (from, nil for the base repository itself) and the ref moves with
// UpdateRefs and HOOK_MODE_SKIP_PRE (K12, SPEC 13.5). The bridge follows through asc.
func setPullHeadThroughAsc(ctx context.Context, pr *issues_model.PullRequest, from *repo_model.Repository, rev string) error {
	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	req := repostore.UpdateRefsRequest{
		Repo: pr.BaseRepo.StoreRef(),
		Updates: []repostore.RefUpdateSpec{{
			Name:   pr.GetGitRefName(),
			NewRev: rev,
			Force:  true,
		}},
	}
	if from != nil {
		ref := from.StoreRef()
		req.From = &ref
	}
	if _, err := writer.UpdateRefs(ctx, req); err != nil {
		return fmt.Errorf("set %s through asc: %w", pr.GetGitRefName(), err)
	}
	return nil
}
