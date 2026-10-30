// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"

	"forgejo.org/services/context"
	files_service "forgejo.org/services/repository/files"
)

// AscPolicyEditResponse is the answer to an edit of the policy file of a repository stored in asc through the contents
// API. The file was not committed: the edit became a change under review (docs/asc/BRANCH-PROTECTION.md), which a
// maintainer lands like any other.
type AscPolicyEditResponse struct {
	// Change is the short id of the change under review.
	Change string `json:"change"`
	// PullRequest is the address of its pull request; empty while the git view of the repository does not have the
	// change yet, when the pull request opens as soon as it does.
	PullRequest string `json:"pull_request,omitempty"`
}

// respondAscPolicyEdit answers a change of files that the policy hook took over (services/asctrust): 202 with the
// change when it was proposed, 403 with the reason when the person may not edit the policy or the edit cannot be
// proposed. It returns false for any other error.
func respondAscPolicyEdit(ctx *context.APIContext, err error) bool {
	var proposed *files_service.ErrAscPolicyProposed
	if errors.As(err, &proposed) {
		resp := AscPolicyEditResponse{Change: proposed.Change}
		if proposed.Link != "" {
			resp.PullRequest = proposed.Link
		}
		ctx.JSON(http.StatusAccepted, resp)
		return true
	}
	var refused *files_service.ErrAscPolicyRefused
	if errors.As(err, &refused) {
		ctx.Error(http.StatusForbidden, "PolicyEdit", refused.Reason)
		return true
	}
	return false
}
