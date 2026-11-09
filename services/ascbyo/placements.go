// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// MaxPlacementsPerCall is the most repositories one ListRepoPlacements call of asc answers for; RepoPlacements
// asks in batches of this size.
const MaxPlacementsPerCall = 100

// Placement says where a repository keeps its objects.
type Placement struct {
	// TargetID is the storage target new objects go to; "" for the server default.
	TargetID string
	// TargetName is that target's name; "" for the server default or a target that is gone.
	TargetName string
	// State is that target's state; UNSPECIFIED for the server default or a target that is gone.
	State ascv1.StorageTargetState
	// HasFallback says objects remain in an earlier place until a migration moves them.
	HasFallback bool
	// Migrating says a migration is copying, so writes go to two places.
	Migrating bool
}

// Default reports whether the repository uses the server default.
func (p Placement) Default() bool { return p.TargetID == "" }

// Gone reports whether the assigned target no longer exists.
func (p Placement) Gone() bool { return p.TargetID != "" && p.TargetName == "" }

// Split reports whether objects remain in an earlier place with no migration moving them.
func (p Placement) Split() bool { return p.HasFallback && !p.Migrating }

// Off reports whether the assigned target exists but is not enabled, so reads or writes may fail.
func (p Placement) Off() bool {
	return !p.Default() && !p.Gone() && p.State != ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED
}

// Mismatch reports whether where the objects are and where they should be disagree: the assigned target is gone or
// not enabled, or objects remain in an earlier place with no migration moving them.
func (p Placement) Mismatch() bool { return p.Gone() || p.Off() || p.Split() }

// RepoPlacements asks asc where each repository of names ("owner/name") keeps its objects. The answer is keyed by
// the lower-case name; names that are not valid repository names, and repositories asc does not know, are left out.
// It is a site administrator's call.
func (s *Service) RepoPlacements(ctx context.Context, names []string) (map[string]Placement, error) {
	out := make(map[string]Placement, len(names))
	seen := make(map[string]bool, len(names))
	ask := make([]string, 0, len(names))
	for _, n := range names {
		sc, err := NewScope(KindRepo, n)
		if err != nil || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		ask = append(ask, sc.Name)
	}
	for len(ask) > 0 {
		batch := ask[:min(len(ask), MaxPlacementsPerCall)]
		ask = ask[len(batch):]
		if err := s.placements(ctx, batch, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) placements(ctx context.Context, batch []string, out map[string]Placement) error {
	sc := Scope{Kind: KindRepo, Name: batch[0]}
	cctx, cancel := s.call(ctx, sc, "ListRepoPlacements", "", false)
	defer cancel()
	resp, err := s.api.Storage.ListRepoPlacements(cctx, &ascv1.ListRepoPlacementsRequest{Repos: batch})
	if err != nil {
		return done("list repository placements", sc, err)
	}
	asked := make(map[string]bool, len(batch))
	for _, n := range batch {
		asked[strings.ToLower(n)] = true
	}
	for _, p := range resp.GetPlacements() {
		key := strings.ToLower(p.GetRepo())
		// An answer for a repository that was not asked about would be a server bug; it is dropped.
		if !asked[key] {
			continue
		}
		id := p.GetTargetId()
		if id != "" && !ValidTargetID(id) {
			continue
		}
		pl := Placement{TargetID: id, HasFallback: p.GetHasFallback(), Migrating: p.GetMigrating()}
		if id != "" {
			pl.TargetName, pl.State = p.GetTargetName(), p.GetTargetState()
		}
		out[key] = pl
	}
	return nil
}
