// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	pull_service "forgejo.org/services/pull"
)

// What the queue page and the pull request page show: the status of a branch's queue, the queue entry of a pull
// request, and the dry run of "Merge when ready" and "Merge now". Nothing here writes.

// AscQueueRecentLimit is the number of finished entries the queue page lists.
const AscQueueRecentLimit = 10

// AscQueueRow is one entry of the queue page. PR is nil when the pull request is gone.
type AscQueueRow struct {
	Entry *AscStackQueueEntry
	PR    *issues_model.PullRequest
	// Position is 1 for the entry that lands next; 0 for a finished entry.
	Position int
	// CanCancel is true when the viewer may remove the entry (its owner or an administrator).
	CanCancel bool
}

// AscQueueView is the queue of one branch.
type AscQueueView struct {
	Branch string
	Paused bool
	// Landing is true while a worker holds the branch.
	Landing bool
	Active  []AscQueueRow
	Recent  []AscQueueRow
}

func ascQueueRow(ctx context.Context, e *AscStackQueueEntry, viewer *user_model.User, admin bool) (AscQueueRow, error) {
	row := AscQueueRow{Entry: e, CanCancel: viewer != nil && (admin || e.DoerID == viewer.ID)}
	pr, err := issues_model.GetPullRequestByID(ctx, e.PullID)
	switch {
	case err == nil:
		if err := pr.LoadIssue(ctx); err != nil {
			return row, err
		}
		// Issue.Link needs the repository.
		if err := pr.Issue.LoadRepo(ctx); err != nil {
			return row, err
		}
		row.PR = pr
	case issues_model.IsErrPullRequestNotExist(err):
	default:
		return row, err
	}
	return row, nil
}

// AscQueueViewOf reads the queue of branch of repo as the viewer sees it. The caller has checked that the viewer
// may read the pull requests of the repository.
func AscQueueViewOf(ctx context.Context, repo *repo_model.Repository, branch string, viewer *user_model.User) (*AscQueueView, error) {
	admin, err := mayAdminAscQueue(ctx, viewer, repo)
	if err != nil {
		return nil, err
	}
	v := &AscQueueView{Branch: branch}
	if v.Paused, err = AscQueuePaused(ctx, repo.ID, branch); err != nil {
		return nil, err
	}
	var br AscStackQueueBranch
	if has, err := db.GetEngine(ctx).Where("repo_id = ? AND branch = ?", repo.ID, branch).Get(&br); err != nil {
		return nil, err
	} else if has {
		v.Landing = br.LockUntil > ascQueueNow().UnixNano()
	}
	active, err := ascQueueEntries(ctx, repo.ID, branch, AscQueueQueued, AscQueueLanding, AscQueueBlocked)
	if err != nil {
		return nil, err
	}
	for i, e := range active {
		row, err := ascQueueRow(ctx, e, viewer, admin)
		if err != nil {
			return nil, err
		}
		row.Position = i + 1
		v.Active = append(v.Active, row)
	}
	var recent []*AscStackQueueEntry
	if err := db.GetEngine(ctx).Where("repo_id = ? AND branch = ?", repo.ID, branch).
		In("state", AscQueueMerged, AscQueueCancelled).OrderBy("updated_unix DESC, id DESC").Limit(AscQueueRecentLimit).Find(&recent); err != nil {
		return nil, err
	}
	for _, e := range recent {
		row, err := ascQueueRow(ctx, e, viewer, admin)
		if err != nil {
			return nil, err
		}
		v.Recent = append(v.Recent, row)
	}
	return v, nil
}

// AscQueueEntries returns the queue entry of each pull request that has one, keyed by pull request id.
func AscQueueEntries(ctx context.Context, pullIDs []int64) (map[int64]*AscStackQueueEntry, error) {
	out := map[int64]*AscStackQueueEntry{}
	if len(pullIDs) == 0 {
		return out, nil
	}
	var rows []*AscStackQueueEntry
	if err := db.GetEngine(ctx).In("pull_id", pullIDs).Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.PullID] = r
	}
	return out, nil
}

// AscQueueMayAdmin reports whether doer may pause and resume the queue of repo: a site or repository admin.
func AscQueueMayAdmin(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (bool, error) {
	return mayAdminAscQueue(ctx, doer, repo)
}

// AscQueueDryRun is what "Merge when ready" or "Merge now" would find for the prefix bottom..pr.
type AscQueueDryRun struct {
	Checks []pull_service.AscStackCheck
	// Ready is true when every pull request of the prefix passes now: a land would be attempted.
	Ready bool
	// Paused is true when the branch is paused: nothing lands until an administrator resumes it.
	Paused bool
	// Queued maps a pull request id to its queue entry, for those already queued.
	Queued map[int64]*AscStackQueueEntry
	// Restack is asc's dry run of a restack of the stack. NeedsContentMerge is true when it would rewrite a
	// change, which means a land now would need a content merge (and is refused while REVALIDATE is on).
	Restack           *pull_service.AscRestackStackResult
	RestackErr        error
	NeedsContentMerge bool
	// NoContentMerge is the setting in force.
	NoContentMerge bool
}

// AscQueueDryRunOf checks what a merge of the prefix bottom..pr would meet, as doer, and writes nothing.
func AscQueueDryRunOf(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscQueueDryRun, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return nil, ErrAscQueueNotAllowed
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if ok, err := ascQueueBacked(pr.BaseRepo); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrAscQueueDisabled
	}
	perm, err := access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer)
	if err != nil {
		return nil, err
	}
	checks, err := pull_service.CheckAscStack(ctx, doer, perm, pr)
	if err != nil {
		return nil, err
	}
	d := &AscQueueDryRun{Checks: checks, Ready: true, NoContentMerge: ascQueueNoContentMerge()}
	ids := make([]int64, len(checks))
	for i, c := range checks {
		ids[i] = c.Item.PR.ID
		if c.Err != nil {
			d.Ready = false
		}
	}
	if d.Queued, err = AscQueueEntries(ctx, ids); err != nil {
		return nil, err
	}
	if d.Paused, err = AscQueuePaused(ctx, pr.BaseRepoID, pr.BaseBranch); err != nil {
		return nil, err
	}
	d.Restack, d.RestackErr = pull_service.RestackAscStack(ctx, doer, pr, pull_service.AscRestackOptions{DryRun: true})
	if d.RestackErr == nil && d.Restack != nil {
		d.NeedsContentMerge = d.Restack.Needs()
	}
	return d, nil
}
