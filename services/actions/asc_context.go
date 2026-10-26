// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"errors"
	"fmt"

	actions_model "forgejo.org/models/actions"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"

	"google.golang.org/protobuf/types/known/structpb"
)

// The keys K14 adds to the task's github context for a storage_backend=asc repository. A repository on
// any other backend gets none of them.
const (
	// ascContextServerURL is the public URL of asc serve ([asc] SERVER_URL).
	ascContextServerURL = "asc_server_url"
	// ascContextChangeset is the asc changeset id (lowercase hex) of the commit github.sha names.
	ascContextChangeset = "asc_changeset"
	// ascContextBaseMergeBase is the asc changeset id of the pull request's merge base. Events without a
	// pull request, and pull requests with no recorded merge base, do not get the key.
	ascContextBaseMergeBase = "asc_base_merge_base"
)

// changesetMapper is what a git backend offers when it can name a commit's asc changeset (ascbackend).
type changesetMapper interface {
	ChangesetIDs(ctx context.Context, ids []git.ObjectID) ([]string, error)
}

// generateTaskContextWithAsc is generateTaskContext plus the asc keys.
func generateTaskContextWithAsc(ctx context.Context, t *actions_model.ActionTask, ac *repo_model.ActionsConfig) (*structpb.Struct, error) {
	taskContext, err := generateTaskContext(t, ac)
	if err != nil {
		return nil, err
	}
	if err := addAscContext(ctx, taskContext, t.Job.Run); err != nil {
		return nil, fmt.Errorf("asc context: %w", err)
	}
	return taskContext, nil
}

// addAscContext adds the asc keys to a task context built for run. It does nothing for a repository that is
// not storage_backend=asc. It fails closed for an asc repository: when the commit has no changeset or the
// server cannot answer, the task is not handed out, because a job would otherwise check out a blank or
// wrong revision. github.sha is left as it is, the git SHA of the commit.
func addAscContext(ctx context.Context, taskContext *structpb.Struct, run *actions_model.ActionRun) error {
	sb, err := gitrepo.StorageBackendOf(run.Repo)
	if err != nil {
		return err
	}
	if sb != gitrepo.StorageBackendAsc {
		return nil
	}
	if !setting.Asc.Enabled || setting.Asc.ServerURL == "" {
		return errors.New("the repository is on the asc storage backend but [asc] is not enabled")
	}

	// Name the commit github.sha shows: the head, or the base for pull_request_target.
	sha := taskContext.GetFields()["sha"].GetStringValue()
	head, err := parseSha1ID(sha)
	if err != nil {
		return fmt.Errorf("github.sha: %w", err)
	}
	ids := []git.ObjectID{head}
	withMergeBase := false
	if payload, err := run.GetPullRequestEventPayload(); err == nil && payload.PullRequest != nil && payload.PullRequest.MergeBase != "" {
		mergeBase, err := parseSha1ID(payload.PullRequest.MergeBase)
		if err != nil {
			return fmt.Errorf("pull request merge base: %w", err)
		}
		ids = append(ids, mergeBase)
		withMergeBase = true
	}

	gitRepo, err := gitrepo.OpenRepository(ctx, run.Repo)
	if err != nil {
		return err
	}
	defer gitRepo.Close()
	be, err := gitRepo.Backend()
	if err != nil {
		return err
	}
	mapper, ok := be.(changesetMapper)
	if !ok {
		return errors.New("the repository backend cannot name changesets")
	}
	changesets, err := mapper.ChangesetIDs(ctx, ids)
	if err != nil {
		return err
	}
	if len(changesets) != len(ids) {
		return errors.New("the asc server answered the wrong number of changesets")
	}

	fields := taskContext.GetFields()
	fields[ascContextServerURL] = structpb.NewStringValue(setting.AscPublicURL())
	fields[ascContextChangeset] = structpb.NewStringValue(changesets[0])
	if withMergeBase {
		fields[ascContextBaseMergeBase] = structpb.NewStringValue(changesets[1])
	}
	return addAscStackContext(ctx, taskContext, run)
}

// parseSha1ID reads one full SHA-1 commit id. Anything else is refused: an abbreviated or empty id would
// let the server pick the commit.
func parseSha1ID(hexID string) (git.ObjectID, error) {
	id, err := git.NewIDFromString(hexID)
	if err != nil || id.Type() != git.Sha1ObjectFormat {
		return nil, errors.New("not a full SHA-1 commit id")
	}
	return id, nil
}
