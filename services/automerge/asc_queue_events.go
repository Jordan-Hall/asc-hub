// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
)

// What people see of the stack queue besides the queue page (PLAN unit C39).
//
//   - Queuing a pull request adds the same timeline line as "merge when checks succeed" (scheduled to auto
//     merge), and taking it out of the queue adds the matching line (canceled), so the pull request page
//     shows what the queue did. The line is written as the user who acted: for a cancel by the queue itself
//     (the queuer lost the right, the pull request left its stack) that is the user who queued it.
//   - When the queue stops a pull request, the user who queued it and the author of the pull request get a
//     notification: when it is cancelled, and when it is blocked and waits for a new head. The queuer who
//     acts is not told about their own action. The reason stays on the queue page; no text from asc or from
//     an error goes into a notification or a comment.
//   - A land raises the usual merged event (services/pull: afterAscStackMerge), so webhooks and
//     notifications for merged pull requests need nothing here.
//
// Every failure here is logged and dropped: the queue state is already stored, and a missing line or
// notification must never stop the queue.

// ascQueueNote writes the timeline line of an enqueue (scheduled) or a dequeue (canceled) on pr as actor.
func ascQueueNote(ctx context.Context, pullID int64, actor *user_model.User, scheduled bool) {
	pr, err := issues_model.GetPullRequestByID(ctx, pullID)
	if err != nil {
		if !issues_model.IsErrPullRequestNotExist(err) {
			log.Error("asc queue: timeline line of pull request %d: %v", pullID, err)
		}
		return
	}
	typ := issues_model.CommentTypePRUnScheduledToAutoMerge
	if scheduled {
		typ = issues_model.CommentTypePRScheduledToAutoMerge
	}
	if _, err := issues_model.CreateAutoMergeComment(ctx, typ, pr, actor); err != nil {
		log.Error("asc queue: timeline line of pull request %d: %v", pullID, err)
	}
}

// ascQueueTell notifies the user who queued the pull request, and its author, that the queue stopped it.
// actor is the user whose action it was, who is not notified; 0 for the queue itself.
func ascQueueTell(ctx context.Context, ent *AscStackQueueEntry, actorID int64) {
	pr, err := issues_model.GetPullRequestByID(ctx, ent.PullID)
	if err != nil {
		return
	}
	if err := pr.LoadIssue(ctx); err != nil {
		log.Error("asc queue: notification for pull request %d: %v", ent.PullID, err)
		return
	}
	seen := map[int64]bool{actorID: true, 0: true}
	for _, receiver := range []int64{ent.DoerID, pr.Issue.PosterID} {
		if seen[receiver] {
			continue
		}
		seen[receiver] = true
		if err := activities_model.CreateOrUpdateIssueNotifications(ctx, pr.IssueID, 0, actorID, receiver); err != nil {
			log.Error("asc queue: notification for pull request %d: %v", ent.PullID, err)
		}
	}
}

// ascQueueAnnounceStopped is called after entries were set to cancelled or blocked by the queue itself. The
// entries are the rows as they were before the change.
func ascQueueAnnounceStopped(ctx context.Context, entries []*AscStackQueueEntry, state string) {
	for _, ent := range entries {
		if state == AscQueueCancelled {
			doer, err := user_model.GetUserByID(ctx, ent.DoerID)
			if err == nil {
				ascQueueNote(ctx, ent.PullID, doer, false)
			}
		}
		ascQueueTell(ctx, ent, 0)
	}
}

// ascQueueEntriesToChange returns the rows of pullIDs that a change to state would alter: in one of onlyFrom
// when given, and not in state already.
func ascQueueEntriesToChange(ctx context.Context, state string, pullIDs []int64, onlyFrom []string) ([]*AscStackQueueEntry, error) {
	if len(pullIDs) == 0 {
		return nil, nil
	}
	sess := db.GetEngine(ctx).In("pull_id", pullIDs).And("state <> ?", state)
	if len(onlyFrom) > 0 {
		sess = sess.In("state", onlyFrom)
	}
	var out []*AscStackQueueEntry
	return out, sess.OrderBy("id").Find(&out)
}
