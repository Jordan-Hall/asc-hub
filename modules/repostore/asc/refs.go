// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"context"
	"fmt"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
)

// UpdateRefs moves, creates and deletes refs in one call, so that asc runs its hooks once. Objects named by
// NewRev come from req.From, which is copied into Repo first when it is another repository. Every update
// of one request is forced or none is: asc takes one update mode per call.
func (w *Writer) UpdateRefs(ctx context.Context, req repostore.UpdateRefsRequest) ([]repostore.RefResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	to, err := handle(req.Repo)
	if err != nil {
		return nil, err
	}
	from := to
	if req.From != nil {
		if from, err = handle(*req.From); err != nil {
			return nil, err
		}
	}
	forced := 0
	for _, u := range req.Updates {
		if u.Force && !u.Delete {
			forced++
		}
	}
	mode := ascv1.UpdateMode_UPDATE_MODE_FAST_FORWARD
	switch moves := countMoves(req.Updates); {
	case forced == 0:
	case forced == moves:
		mode = ascv1.UpdateMode_UPDATE_MODE_FORCE
	default:
		return nil, fmt.Errorf("%w: forced and unforced updates in one request", repostore.ErrUnsupported)
	}

	s, err := w.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()

	results := make([]repostore.RefResult, len(req.Updates))
	updates := make([]*ascv1.RefUpdate, len(req.Updates))
	var copies [][]byte
	for i, u := range req.Updates {
		old, err := s.resolve(to, u.Name)
		if err != nil {
			return nil, err
		}
		results[i] = repostore.RefResult{Name: u.Name, Old: old}
		updates[i] = &ascv1.RefUpdate{Name: u.Name, Expect: &ascv1.RefUpdate_Any{Any: true}}
		if u.Delete {
			updates[i].Delete = true
			continue
		}
		tip, err := s.mustResolve(from, u.NewRev)
		if err != nil {
			return nil, err
		}
		p, err := s.pair(from, tip)
		if err != nil {
			return nil, err
		}
		copies = append(copies, p.asc)
		updates[i].New = &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: p.asc}}
	}
	if err := s.copyClosure(from, to, copies...); err != nil {
		return nil, err
	}
	resp, err := s.api.UpdateRefs(s.ctx, &ascv1.UpdateRefsRequest{
		Repo: to.ascRef(), Updates: updates, Mode: mode, Hooks: ascv1.HookMode_HOOK_MODE_RUN,
	})
	if err != nil {
		return nil, publishErr("UpdateRefs", err)
	}
	if len(resp.GetResults()) != len(req.Updates) {
		return nil, fmt.Errorf("%w: UpdateRefs answered %d results for %d updates", ErrBadAnswer, len(resp.GetResults()), len(req.Updates))
	}
	for i, u := range req.Updates {
		if u.Delete {
			continue
		}
		var tip git.ObjectID
		if tip, err = s.mustResolve(to, u.Name); err != nil {
			return nil, err
		}
		results[i].New = tip
	}
	return results, nil
}

func countMoves(updates []repostore.RefUpdateSpec) int {
	n := 0
	for _, u := range updates {
		if !u.Delete {
			n++
		}
	}
	return n
}
