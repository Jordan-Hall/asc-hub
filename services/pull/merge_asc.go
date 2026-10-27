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
	"forgejo.org/services/ascsync"
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

// pullHeadWriter is the Writer that moves a pull head; tests replace it.
var pullHeadWriter = repostore.Default

// setPullHeadThroughAsc points refs/pull/<n>/head of a pull request at a revision, with asc: the closure is copied
// from the repository the revision is in (from, nil for the base repository itself) and the ref moves with
// UpdateRefs and HOOK_MODE_SKIP_PRE (K12, SPEC 13.5). The bridge follows through asc.
func setPullHeadThroughAsc(ctx context.Context, pr *issues_model.PullRequest, from *repo_model.Repository, rev string) error {
	writer, err := pullHeadWriter()
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
	results, err := writer.UpdateRefs(ctx, req)
	if err != nil {
		return fmt.Errorf("set %s through asc: %w", pr.GetGitRefName(), err)
	}
	// The ref is asc's; the git bridge follows through bridge_promote a moment later, and the callers read the
	// pull head with git straight away (the compare of a new pull request). Wait until git shows it.
	for _, r := range results {
		if r.Name != pr.GetGitRefName() || r.New == nil {
			continue
		}
		if err := ascsync.WaitForRefs(ctx, pr.BaseRepo, []ascsync.PushRef{{Name: r.Name, New: r.New.String()}}); err != nil {
			return fmt.Errorf("set %s through asc: %w", pr.GetGitRefName(), err)
		}
	}
	return nil
}
