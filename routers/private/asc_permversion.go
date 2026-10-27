// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	auth_model "forgejo.org/models/auth"
	git_model "forgejo.org/models/git"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/context"
)

// ascPermVersion is the fingerprint of everything an allowed authz answer rested on (private.AscAuthzResponse.PermVersion).
//
// Repository level (the same for every asker and every ref until the repository or the permission changes):
// the repository as the hub stores it (id, owner, name, visibility, archive, mirror, state, storage, units) and its
// owner (type, visibility, active).
//
// Asker level (the same for the same credential and the same asker): the account (active, may sign in, must change
// the password, restricted, administrator, two-factor required and enrolled), the permission the hub's model gave on
// every unit, and the credential: the stored token (id, last change), its scope, its expiry, the grant scopes of an
// OAuth token, whether a password was used, the deploy key (id, mode), the Actions task (id, status, stopped).
//
// Request level (depends on the ref asked): the ref, and the protected-branch rule that matches it (id, last change,
// whether it requires signed commits). The answer's branch_rules and task fields are of this level.
//
// A collaborator, team, organisation, visibility, rename, transfer, deletion or token change reaches the answer only
// through one of these, so it changes the value without a hook in each place that makes such a change. A deletion or a
// revocation ends in a refusal, and a refusal has no version. The value is the first 64 bits of a SHA-256 of a JSON
// encoding of the facts (a struct, so every field has its own name and quoting: no text of a ref or a scope can pass
// for another field), after a domain-separation line that names the format. The facts carry no secret. It is a
// fingerprint, not a counter: an access that changes and changes back has the old value again.
func ascPermVersion(ctx *context.PrivateContext, repo *repo_model.Repository, p *ascPrincipal, access access_model.Permission, ref string) (uint64, error) {
	if err := repo.LoadUnits(ctx); err != nil {
		return 0, fmt.Errorf("LoadUnits: %w", err)
	}
	f := permFacts{
		Repo: permRepo{
			ID: repo.ID, OwnerID: repo.OwnerID, OwnerName: strings.ToLower(repo.OwnerName), Name: repo.LowerName,
			Private: repo.IsPrivate, Archived: repo.IsArchived, Mirror: repo.IsMirror, Status: int(repo.Status),
			Storage: repo.GetStorageBackend(),
		},
		Mode: int(access.AccessMode),
		Ref:  ref,
		Cred: permCred{
			TokenID: p.tokenID, TokenUpdated: p.tokenUpdated, Expires: p.expires,
			Scope: p.scope, Grants: p.grantScopes, Password: p.passwordAuth,
		},
	}
	if repo.Owner != nil {
		f.Owner = &permOwner{Type: int(repo.Owner.Type), Visibility: int(repo.Owner.Visibility), Active: repo.Owner.IsActive}
	}
	f.Units = make([]string, 0, len(repo.Units))
	for _, u := range repo.Units {
		f.Units = append(f.Units, fmt.Sprintf("%d=%d", u.Type, access.UnitAccessMode(u.Type)))
	}
	sort.Strings(f.Units)
	if u := p.user; u != nil {
		enrolled, err := auth_model.HasTwoFactorByUID(ctx, u.ID)
		if err != nil {
			return 0, fmt.Errorf("HasTwoFactorByUID: %w", err)
		}
		f.User = &permUser{
			ID: u.ID, Active: u.IsActive, Prohibit: u.ProhibitLogin, MustChange: u.MustChangePassword,
			Restricted: u.IsRestricted, Admin: u.IsAdmin, TwoFactorRequired: u.MustHaveTwoFactor(), TwoFactorEnrolled: enrolled,
		}
	}
	if k := p.deployKey; k != nil {
		f.Key = &permKey{ID: k.ID, Mode: int(k.Mode)}
	}
	if t := p.task; t != nil {
		f.Task = &permTask{ID: t.ID, Status: int(t.Status), Stopped: int64(t.Stopped)}
	}
	if branch := strings.TrimPrefix(ref, "refs/heads/"); branch != "" {
		rule, err := git_model.GetFirstMatchProtectedBranchRule(ctx, repo.ID, branch)
		if err != nil {
			return 0, fmt.Errorf("GetFirstMatchProtectedBranchRule: %w", err)
		}
		if rule != nil {
			f.Rule = &permRule{ID: rule.ID, Updated: int64(rule.UpdatedUnix), Signed: rule.RequireSignedCommits}
		}
	}
	return permVersionOf(f)
}

// permFacts are the facts of the fingerprint. Every field is named in the encoding, strings are quoted by it, and a
// field that is absent (a nil pointer encodes as null) differs from one that is present with zero values.
type permFacts struct {
	Repo  permRepo   `json:"repo"`
	Owner *permOwner `json:"owner"`
	Mode  int        `json:"mode"`
	Units []string   `json:"units"`
	User  *permUser  `json:"user"`
	Cred  permCred   `json:"cred"`
	Key   *permKey   `json:"deploy_key"`
	Task  *permTask  `json:"task"`
	Ref   string     `json:"ref"`
	Rule  *permRule  `json:"rule"`
}

type permRepo struct {
	ID        int64
	OwnerID   int64
	OwnerName string
	Name      string
	Storage   string
	Private   bool
	Archived  bool
	Mirror    bool
	Status    int
}

type permOwner struct {
	Type       int
	Visibility int
	Active     bool
}

type permUser struct {
	ID                int64
	Active            bool
	Prohibit          bool
	MustChange        bool
	Restricted        bool
	Admin             bool
	TwoFactorRequired bool
	TwoFactorEnrolled bool
}

type permCred struct {
	TokenID      int64
	TokenUpdated int64
	Expires      int64
	Scope        string
	Grants       string
	Password     bool
}

type permKey struct {
	ID   int64
	Mode int
}

type permTask struct {
	ID      int64
	Status  int
	Stopped int64
}

type permRule struct {
	ID      int64
	Updated int64
	Signed  bool
}

// permDomain starts the hashed text. A change of the facts or of their encoding changes it.
const permDomain = "asc-hub perm_version v3\n"

// permVersionOf is the fingerprint of the facts.
func permVersionOf(f permFacts) (uint64, error) {
	text, err := json.Marshal(f)
	if err != nil {
		return 0, fmt.Errorf("encode the facts: %w", err)
	}
	sum := sha256.Sum256(append([]byte(permDomain), text...))
	return binary.BigEndian.Uint64(sum[:8]), nil
}
