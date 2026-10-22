// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"context"

	"forgejo.org/modules/setting"
)

// Limits on the fields of an AscAuthzRequest. The endpoint rejects anything larger.
const (
	AscAuthzMaxBodySize = 16 * 1024
	AscAuthzMaxToken    = 4096
	AscAuthzMaxName     = 255
	AscAuthzMaxRef      = 1024
)

// Modes of an AscAuthzRequest.
const (
	AscAuthzModeRead  = "read"
	AscAuthzModeWrite = "write"
)

// AscAuthzRequest asks whether a principal may use a repository through `asc serve`.
// At most one of Token and KeyID is set; with neither the principal is anonymous
// and only read access to a public repository can be allowed.
type AscAuthzRequest struct {
	// Token is the bearer credential the client presented to asc: a personal
	// access token, an OAuth2 access token or an Actions task token.
	Token string `json:"token,omitempty"`
	// KeyID is the ID of an SSH public key or deploy key that asc already verified.
	KeyID int64  `json:"key_id,omitempty"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	// Mode is "read" or "write".
	Mode string `json:"mode"`
	// Ref is an optional branch name; it selects the protected-branch rule
	// that is reported in BranchRules.
	Ref string `json:"ref,omitempty"`
}

// AscAuthzTask describes the Actions run behind a task token.
type AscAuthzTask struct {
	RunID    int64  `json:"run_id"`
	Event    string `json:"event"`
	Ref      string `json:"ref"`
	PRIndex  int64  `json:"pr_index"`
	IsForkPR bool   `json:"is_fork_pr"`
}

// AscAuthzBranchRules carries the protected-branch settings that asc enforces.
type AscAuthzBranchRules struct {
	RequireSigned bool `json:"require_signed"`
}

// AscAuthzResponse is the answer of the internal authz endpoint. When Allowed is
// false every other field is zero: a repository the principal cannot read looks
// the same as a missing one.
type AscAuthzResponse struct {
	Allowed       bool                `json:"allowed"`
	UserID        int64               `json:"user_id"`
	Username      string              `json:"username"`
	Perm          string              `json:"perm"`
	DeployKeyID   int64               `json:"deploy_key_id"`
	IsActionsTask bool                `json:"is_actions_task"`
	Task          *AscAuthzTask       `json:"task"`
	BranchRules   AscAuthzBranchRules `json:"branch_rules"`
}

// AscAuthz calls the internal authz endpoint.
func AscAuthz(ctx context.Context, authzReq *AscAuthzRequest) (*AscAuthzResponse, ResponseExtra) {
	reqURL := setting.LocalURL + "api/internal/asc/authz"
	req := newInternalRequest(ctx, reqURL, "POST", authzReq)
	return requestJSONResp(req, &AscAuthzResponse{})
}
