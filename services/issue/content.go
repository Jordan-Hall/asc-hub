// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issue

import (
	"context"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	user_model "forgejo.org/models/user"
	notify_service "forgejo.org/services/notify"
)

// ChangeContent changes issue content, as the given user.
func ChangeContent(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, content string, contentVersion int) (err error) {
	oldContent := issue.Content

	// A hidden issue keeps the text a moderator gave it until the hide is undone.
	if content != oldContent {
		if err := moderation.CheckNotHidden(ctx, doer != nil && doer.IsAdmin, moderation.ReportedContentTypeIssue, issue.ID); err != nil {
			return err
		}
	}

	if err := issues_model.ChangeIssueContent(ctx, issue, doer, content, contentVersion); err != nil {
		return err
	}

	notify_service.IssueChangeContent(ctx, doer, issue, oldContent)

	return nil
}
