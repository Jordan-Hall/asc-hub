// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"fmt"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	notify_service "forgejo.org/services/notify"
)

// "Restore version" of a stacked pull request (SPEC 20.5 flow 2, PLAN unit C47).
//
// A stacked pull request keeps every version of its asc change (C37). Restoring version v moves
// refs/pull/<n>/head back to the git commit that version had. It writes no new version: asc's list is
// unchanged, so the newest version stays one click away and a restore can be undone by restoring it.
//
// Rules:
//   - Who may restore is the update authorisation of C34 (the same as for a push to the change): the pull
//     request's poster, an owner or admin of the repository, or a writer when the pull request allows
//     maintainer edits. The poster must still be able to read the pull requests of the repository.
//   - The pull request must be open and its repository not archived.
//   - The change comes from the pull request's own asc link, and the version number must be one of the
//     versions asc returned for it (verified as in C37). The commit comes from that list, never from the request.
//   - The commit must still be in the repository, and the head must not already be at it.
//   - Every restore is logged with the actor, and recorded on the pull request as a push comment, so that
//     reviewers see the head move.
//   - Errors returned here never carry server messages.

var (
	// ErrAscRestoreForbidden means the doer may not restore versions of the pull request.
	ErrAscRestoreForbidden = errors.New("not allowed to restore a version of this pull request")
	// ErrAscRestoreNotOpen means the pull request is closed or merged, or its repository is archived.
	ErrAscRestoreNotOpen = errors.New("pull request is not open for changes")
	// ErrAscRestoreGone means the commit of the version is no longer in the repository.
	ErrAscRestoreGone = errors.New("the commit of that version is no longer in the repository")
	// ErrAscRestoreCurrent means the head of the pull request is already at that version.
	ErrAscRestoreCurrent = errors.New("the pull request is already at that version")
)

// AscRestoreResult says what a restore did.
type AscRestoreResult struct {
	Version   uint32
	OldCommit string
	NewCommit string
}

// ascHeadStore is the repository access a restore needs. Tests replace ascHeads.
type ascHeadStore interface {
	// Head returns the commit refs/pull/<n>/head is at.
	Head(ctx context.Context, pr *issues_model.PullRequest) (string, error)
	// HasCommit reports whether the base repository holds the commit.
	HasCommit(ctx context.Context, pr *issues_model.PullRequest, id string) (bool, error)
	// Move points refs/pull/<n>/head at the commit and returns once the move is complete.
	Move(ctx context.Context, pr *issues_model.PullRequest, id string) error
}

type gitHeadStore struct{}

func (gitHeadStore) Head(ctx context.Context, pr *issues_model.PullRequest) (string, error) {
	gitRepo, err := gitrepo.OpenRepository(ctx, pr.BaseRepo)
	if err != nil {
		return "", err
	}
	defer gitRepo.Close()
	return gitRepo.GetRefCommitID(pr.GetGitRefName())
}

func (gitHeadStore) HasCommit(ctx context.Context, pr *issues_model.PullRequest, id string) (bool, error) {
	gitRepo, err := gitrepo.OpenRepository(ctx, pr.BaseRepo)
	if err != nil {
		return false, err
	}
	defer gitRepo.Close()
	if _, err := gitRepo.GetCommit(id); err != nil {
		if git.IsErrNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (gitHeadStore) Move(ctx context.Context, pr *issues_model.PullRequest, id string) error {
	pr.HeadCommitID = id
	return UpdateRef(ctx, pr)
}

var (
	ascHeads ascHeadStore = gitHeadStore{}

	// ascRestoreFollowUp does what a push does after the head of a pull request moved: the merge base of the
	// stack rule, the conflict check, stale reviews, the push comment and the notification. Each step reports
	// its own failure to the log; the head has moved by then and the restore stands. Tests replace it.
	ascRestoreFollowUp = func(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, oldID, newID string) {
		if err := RecomputeAscStackBase(ctx, pr); err != nil {
			log.Error("asc restore version: merge base of pull request %d: %v", pr.ID, err)
		}
		AddToTaskQueue(ctx, pr)
		if err := pr.LoadIssue(ctx); err != nil {
			log.Error("asc restore version: load issue of pull request %d: %v", pr.ID, err)
			return
		}
		ValidatePullRequest(ctx, pr, newID, oldID, doer)
		comment, err := CreatePushPullComment(ctx, doer, pr, oldID, newID)
		if err != nil {
			log.Error("asc restore version: push comment of pull request %d: %v", pr.ID, err)
		} else if comment != nil {
			notify_service.PullRequestPushCommits(ctx, doer, pr, comment)
		}
		notify_service.PullRequestSynchronized(ctx, doer, pr)
	}
)

// AscMayRestoreVersion reports whether doer may restore versions of pr: the update authorisation of C34.
// A nil doer, and a pull request that is not open for changes, answer false.
func AscMayRestoreVersion(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User) (bool, error) {
	if pr == nil || doer == nil {
		return false, nil
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return false, err
	}
	perm, err := access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer)
	if err != nil {
		return false, err
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return false, err
	}
	return ascRestoreAllowed(
		pr.Issue.PosterID == doer.ID && perm.CanReadIssuesOrPulls(true),
		doer.IsAdmin || perm.IsOwner() || perm.IsAdmin(),
		perm.CanWrite(unit.TypeCode),
		pr.AllowMaintainerEdit,
	), nil
}

// ascRestoreAllowed is the update authorisation of C34: the poster (who can still read the pull requests), an
// owner or admin, or a writer when the pull request allows maintainer edits.
func ascRestoreAllowed(isPoster, isAdminOrOwner, canWrite, allowMaintainerEdit bool) bool {
	return isPoster || isAdminOrOwner || (canWrite && allowMaintainerEdit)
}

// AscPullIsOpen reports whether pr may still change: open, not merged and its repository not archived.
func AscPullIsOpen(ctx context.Context, pr *issues_model.PullRequest) (bool, error) {
	if pr == nil || pr.HasMerged {
		return false, nil
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return false, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return false, err
	}
	return !pr.Issue.IsClosed && !pr.BaseRepo.IsArchived, nil
}

// AscRestoreVersion moves refs/pull/<n>/head of the stacked pull request pr to the git commit of the given
// version of its change, as doer. The handler must have checked that doer may read the pull request; the
// permission to restore is checked here.
func AscRestoreVersion(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, version uint32) (*AscRestoreResult, error) {
	allowed, err := AscMayRestoreVersion(ctx, pr, doer)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAscRestoreForbidden
	}
	open, err := AscPullIsOpen(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, ErrAscRestoreNotOpen
	}

	versions, err := AscVersions(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !versions.Has(version) {
		return nil, ErrAscBadVersion
	}
	target := versions.Versions[version-1].GitCommit

	current, err := ascHeads.Head(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("head of pull request %d: %w", pr.ID, err)
	}
	if current == target {
		return nil, ErrAscRestoreCurrent
	}
	has, err := ascHeads.HasCommit(ctx, pr, target)
	if err != nil {
		return nil, fmt.Errorf("commit of version %d of pull request %d: %w", version, pr.ID, err)
	}
	if !has {
		return nil, ErrAscRestoreGone
	}

	if err := ascHeads.Move(ctx, pr, target); err != nil {
		return nil, fmt.Errorf("move head of pull request %d: %w", pr.ID, err)
	}
	pr.HeadCommitID = target
	log.Info("asc audit: restore version: pull request %d (repository %d, issue index %d): user %d moved %s from %s to %s (version %d of %d)",
		pr.ID, pr.BaseRepoID, pr.Index, doer.ID, pr.GetGitRefName(), current, target, version, versions.Latest())
	ascRestoreFollowUp(ctx, pr, doer, current, target)
	return &AscRestoreResult{Version: version, OldCommit: current, NewCommit: target}, nil
}

// AscHeadVersion returns the version whose git commit refs/pull/<n>/head is at, or 0 when the head is at none
// of them (a pull request that was not restored is at its newest version). versions must be the verified
// versions of pr.
func AscHeadVersion(ctx context.Context, pr *issues_model.PullRequest, versions *AscVersionList) (uint32, error) {
	if pr == nil || versions == nil || len(versions.Versions) == 0 {
		return 0, nil
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return 0, err
	}
	head, err := ascHeads.Head(ctx, pr)
	if err != nil {
		return 0, err
	}
	return versions.ByGitCommit(head), nil
}
