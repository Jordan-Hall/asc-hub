// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"crypto/sha256"
	"encoding/binary"
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
// revocation ends in a refusal, and a refusal has no version. The value is the first 64 bits of a SHA-256 of the
// facts, which carry no secret. It is a fingerprint, not a counter: an access that changes and changes back has the
// old value again.
func ascPermVersion(ctx *context.PrivateContext, repo *repo_model.Repository, p *ascPrincipal, access access_model.Permission, ref string) (uint64, error) {
	if err := repo.LoadUnits(ctx); err != nil {
		return 0, fmt.Errorf("LoadUnits: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "asc-perm-v2|repo=%d|owner=%d|%s/%s|private=%t|archived=%t|mirror=%t|status=%d|storage=%s",
		repo.ID, repo.OwnerID, strings.ToLower(repo.OwnerName), repo.LowerName,
		repo.IsPrivate, repo.IsArchived, repo.IsMirror, repo.Status, repo.GetStorageBackend())
	if repo.Owner != nil {
		fmt.Fprintf(&b, "|ownertype=%d|ownervis=%d|owneractive=%t", repo.Owner.Type, repo.Owner.Visibility, repo.Owner.IsActive)
	}
	units := make([]string, 0, len(repo.Units))
	for _, u := range repo.Units {
		units = append(units, fmt.Sprintf("%d=%d", u.Type, access.UnitAccessMode(u.Type)))
	}
	sort.Strings(units)
	fmt.Fprintf(&b, "|mode=%d|units=%s", access.AccessMode, strings.Join(units, ","))

	if u := p.user; u != nil {
		required := u.MustHaveTwoFactor()
		enrolled, err := auth_model.HasTwoFactorByUID(ctx, u.ID)
		if err != nil {
			return 0, fmt.Errorf("HasTwoFactorByUID: %w", err)
		}
		fmt.Fprintf(&b, "|user=%d|active=%t|prohibit=%t|mustchange=%t|restricted=%t|admin=%t|2fa=%t/%t",
			u.ID, u.IsActive, u.ProhibitLogin, u.MustChangePassword, u.IsRestricted, u.IsAdmin, required, enrolled)
	}
	fmt.Fprintf(&b, "|token=%d@%d|scope=%s|expires=%d|grants=%s|password=%t",
		p.tokenID, p.tokenUpdated, p.scope, p.expires, p.grantScopes, p.passwordAuth)
	if k := p.deployKey; k != nil {
		fmt.Fprintf(&b, "|deploykey=%d|keymode=%d", k.ID, k.Mode)
	}
	if t := p.task; t != nil {
		fmt.Fprintf(&b, "|task=%d|taskstatus=%d|stopped=%d", t.ID, t.Status, t.Stopped)
	}

	fmt.Fprintf(&b, "|ref=%s", ref)
	if branch := strings.TrimPrefix(ref, "refs/heads/"); branch != "" {
		rule, err := git_model.GetFirstMatchProtectedBranchRule(ctx, repo.ID, branch)
		if err != nil {
			return 0, fmt.Errorf("GetFirstMatchProtectedBranchRule: %w", err)
		}
		if rule != nil {
			fmt.Fprintf(&b, "|rule=%d@%d|signed=%t", rule.ID, rule.UpdatedUnix, rule.RequireSignedCommits)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return binary.BigEndian.Uint64(sum[:8]), nil
}
