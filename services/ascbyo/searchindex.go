// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"
	"math"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// The "Search indexes" card of a repository's storage page (design board "RepoSettingsStorage"): how much of the
// default branch the text index and the meaning index cover. asc builds the meaning index only when its server has
// an embedding model, which an instance admin installs; CodeIntel.IndexStatus then names the model on the "vectors"
// index, and without one the meaning index is off.

// maxIndexModelBytes bounds the model name shown.
const maxIndexModelBytes = 120

// SearchIndex is the state of the indexes of a repository's default branch.
type SearchIndex struct {
	// TextPercent is how much of the files the text index covers, rounded down; TextKnown is false when asc did
	// not list it.
	TextPercent int
	TextKnown   bool
	// MeaningOn is true when the server has an embedding model; MeaningModel names it and MeaningPercent is how
	// much of the code it has embedded.
	MeaningOn      bool
	MeaningModel   string
	MeaningPercent int
	// LagMinutes is how far the indexes are behind the branch, rounded up.
	LagMinutes int64
}

// percentOf turns a coverage between 0 and 1 into a whole percentage; anything else is not a coverage.
func percentOf(c float32) (int, bool) {
	f := float64(c)
	if math.IsNaN(f) || f < 0 || f > 1 {
		return 0, false
	}
	// a float32 such as 0.9 is a little under it: the epsilon keeps it at 90
	return int(math.Floor(f*100 + 1e-4)), true
}

// SearchIndexOf reads the indexes of the repository itself; the entries of mounted submodules ("files@<path>")
// are left out.
func SearchIndexOf(resp *ascv1.IndexStatusResponse) *SearchIndex {
	si := &SearchIndex{}
	for _, st := range resp.GetIndexes() {
		p, ok := percentOf(st.GetCoverage())
		if !ok {
			continue
		}
		switch st.GetIndex() {
		case "files":
			si.TextPercent, si.TextKnown = p, true
			si.LagMinutes = (max(st.GetLagSeconds(), 0) + 59) / 60
		case "vectors":
			if m := CleanText(st.GetVersion(), maxIndexModelBytes); m != "" {
				si.MeaningOn, si.MeaningModel, si.MeaningPercent = true, m, p
			}
		}
	}
	return si
}

// RepoSearchIndex reads the indexes of the repository's default branch.
func (s *Service) RepoSearchIndex(ctx context.Context, repo Scope) (*SearchIndex, error) {
	if repo.Kind != KindRepo || !repo.Valid() {
		return nil, failure(FailInvalid)
	}
	if s.api.Intel == nil {
		return nil, failure(FailUnavailable)
	}
	cctx, cancel := s.call(ctx, repo, "IndexStatus", "", false)
	defer cancel()
	resp, err := s.api.Intel.IndexStatus(cctx, &ascv1.IndexStatusRequest{Scope: &ascv1.Scope{Repo: repo.Repo()}})
	if err != nil {
		return nil, done("read repository search indexes", repo, err)
	}
	return SearchIndexOf(resp), nil
}
