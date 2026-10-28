// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mailer

import (
	"context"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascpush"
)

// ascPushIssue tells a person in their browsers that something about an issue or pull request is for them, when they
// asked for notifications for the event and may read the repository. Email follows its own rules.
func ascPushIssue(ctx context.Context, issue *issues_model.Issue, doer, receiver *user_model.User, event string) {
	if receiver == nil || !receiver.IsActive || (doer != nil && doer.ID == receiver.ID) {
		return
	}
	if err := issue.LoadRepo(ctx); err != nil {
		log.Error("asc push: LoadRepo: %v", err)
		return
	}
	checkUnit := unit.TypeIssues
	if issue.IsPull {
		checkUnit = unit.TypePullRequests
	}
	if !access_model.CheckRepoUnitUser(ctx, issue.Repo, receiver, checkUnit) {
		return
	}
	who := ""
	if doer != nil {
		who = doer.Name + " in "
	}
	ascpush.Notify(ctx, receiver.ID, event, ascpush.Message{
		Title: issue.Title,
		Body:  who + issue.Repo.FullName(),
		Path:  issue.Link(),
		Tag:   event + "-" + issue.Repo.FullName(),
	})
}
