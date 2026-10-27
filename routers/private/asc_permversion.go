// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/context"
)

// ascPermVersion is the fingerprint of what an authz decision rested on (private.AscAuthzResponse.PermVersion).
//
// It is computed from the same facts the decision used and nothing else: the repository as the hub stores it, the
// state of the asker's account, the permission the hub's model gave on every unit of the repository, and the scope
// of the token. A change of a collaborator, a team, the organisation, the visibility, the name or the owner of the
// repository, or the scope of the token, reaches the decision only through one of these, so it changes the value
// without a hook in each place that makes such a change. No secret goes in.
func ascPermVersion(ctx *context.PrivateContext, repo *repo_model.Repository, p *ascPrincipal, access access_model.Permission) (uint64, error) {
	if err := repo.LoadUnits(ctx); err != nil {
		return 0, fmt.Errorf("LoadUnits: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "asc-perm-v1|repo=%d|owner=%d|%s/%s|private=%t|archived=%t|mirror=%t|status=%d|storage=%s",
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
		fmt.Fprintf(&b, "|user=%d|active=%t|prohibit=%t|mustchange=%t|restricted=%t|admin=%t|scope=%s",
			u.ID, u.IsActive, u.ProhibitLogin, u.MustChangePassword, u.IsRestricted, u.IsAdmin, p.scope)
	}
	if k := p.deployKey; k != nil {
		fmt.Fprintf(&b, "|deploykey=%d|keymode=%d", k.ID, k.Mode)
	}
	if p.task != nil {
		fmt.Fprintf(&b, "|task=%d", p.task.ID)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(b.String()))
	return h.Sum64(), nil
}
