// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"strings"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
)

// getRebaseAmendMessage composes the message to amend commits in rebase merge of a pull request.
// lastMessage is the message of the commit that is amended.
func getRebaseAmendMessage(ctx context.Context, baseGitRepo *git.Repository, pr *issues_model.PullRequest, lastMessage string) (message string, err error) {
	commitTitle, commitBody, _ := strings.Cut(lastMessage, "\n")
	extraVars := map[string]string{"CommitTitle": strings.TrimSpace(commitTitle), "CommitBody": strings.TrimSpace(commitBody)}

	message, body, err := getMergeMessage(ctx, baseGitRepo, pr, repo_model.MergeStyleRebase, extraVars)
	if err != nil || message == "" {
		return "", err
	}

	if len(body) > 0 {
		message = message + "\n\n" + body
	}
	return message, err
}
