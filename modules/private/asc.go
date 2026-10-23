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

	// Limits on the fields of an AscSSHKeyRequest. Key is base64, so
	// AscSSHKeyMaxKey allows a wire blob of 6 KiB, above any key type that
	// Forgejo accepts (a 16384-bit RSA key is about 2 KiB).
	AscSSHKeyMaxType        = 64
	AscSSHKeyMaxKey         = 8192
	AscSSHKeyMaxFingerprint = 128
)

// Kinds of an SSH key that AscSSHKeyResponse reports.
const (
	AscSSHKeyKindUser   = "user"
	AscSSHKeyKindDeploy = "deploy"
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
	// access token, an OAuth2 access token or an Actions task token. With User
	// set it is the Basic password instead.
	Token string `json:"token,omitempty"`
	// User is the Basic user name. When it is set, Token is verified together
	// with it as an HTTP Basic login (an account password or an access token,
	// two-factor respected) and not as a bearer token.
	User string `json:"user,omitempty"`
	// KeyID is the ID of an SSH public key or deploy key that asc already verified.
	KeyID int64 `json:"key_id,omitempty"`
	// SSHKey is a public key that asc's SSH listener verified the client holds.
	// The endpoint finds the key in Forgejo's own tables and decides with the
	// rights of its owner, or with the one repository of a deploy key. It
	// excludes Token, User and KeyID.
	SSHKey *AscSSHKeyRequest `json:"ssh_key,omitempty"`
	Owner  string            `json:"owner"`
	Repo   string            `json:"repo"`
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

// AscSSHKeyRequest names an SSH public key by its algorithm and wire blob, as
// `asc serve` has them after the SSH library checked the client's signature.
type AscSSHKeyRequest struct {
	// Type is the algorithm name: the key type ("ssh-ed25519") or, for an RSA
	// key, a signature algorithm of it ("rsa-sha2-256", "rsa-sha2-512").
	Type string `json:"type"`
	// Key is the standard base64 of the wire blob.
	Key string `json:"key"`
	// Fingerprint is optional, "SHA256:<base64>". When set it must match Key.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// AscSSHKeyResponse is the answer of the internal SSH key lookup. A key that
// is unknown, or whose owner may not sign in, gives the zero value: the two
// look the same to the caller. UserID and Username are those of the owner of a
// user key and are zero for a deploy key, which belongs to repositories.
type AscSSHKeyResponse struct {
	Known    bool   `json:"known"`
	KeyID    int64  `json:"key_id"`
	Kind     string `json:"kind"`
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
}

// AscSSHKey calls the internal SSH key lookup endpoint.
func AscSSHKey(ctx context.Context, keyReq *AscSSHKeyRequest) (*AscSSHKeyResponse, ResponseExtra) {
	reqURL := setting.LocalURL + "api/internal/asc/ssh-key"
	req := newInternalRequest(ctx, reqURL, "POST", keyReq)
	return requestJSONResp(req, &AscSSHKeyResponse{})
}
