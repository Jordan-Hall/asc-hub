// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"math"
	"slices"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api "forgejo.org/modules/structs"
)

// The index a mode reads (CodeIntel.IndexStatus): text and symbol searches read the files index and a
// search by meaning the vectors index. A submodule mounted in the scope has its own entry, "files@<path>".
const (
	IndexFiles   = "files"
	IndexVectors = "vectors"
)

// maxCoverageRepos bounds the repositories asked about their index: one IndexStatus call each.
const maxCoverageRepos = 32

// Coverage says how much of the searched scope the server has indexed.
type Coverage = api.AscCoverage

func percentOf(c float32) (int, bool) {
	if math.IsNaN(float64(c)) || c < 0 || c > 1 {
		return 0, false
	}
	return int(math.Floor(float64(c) * 100)), true
}

// Coverage asks IndexStatus about up to 32 of the repositories and summarises the index the mode uses.
// The note is advice about the answer, so a failed call only lowers Known; it is never an error.
func (s *Service) Coverage(ctx context.Context, repos []Repo, index string) Coverage {
	cov := Coverage{Sampled: len(repos) > maxCoverageRepos}
	if len(repos) > maxCoverageRepos {
		repos = repos[:maxCoverageRepos]
	}
	cov.Asked = len(repos)
	if len(repos) == 0 {
		return cov
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	type one struct {
		percent  int
		lag      int64
		revision string
		ok       bool
	}
	out := make([]one, len(repos))
	parallel(len(repos), fanOut, func(i int) {
		resp, err := s.Intel.IndexStatus(ctx, &ascv1.IndexStatusRequest{Scope: &ascv1.Scope{
			Repo: &ascv1.RepoRef{Name: repos[i].Name, IdMode: ascv1.IdMode_ID_MODE_GIT},
			At:   &ascv1.Scope_Rev{Rev: repos[i].Rev},
		}})
		if err != nil {
			return
		}
		var lowest = 101
		for _, st := range resp.GetIndexes() {
			label := st.GetIndex()
			if label != index && !strings.HasPrefix(label, index+"@") {
				continue
			}
			p, ok := percentOf(st.GetCoverage())
			if !ok {
				return
			}
			out[i].ok = true
			lowest = min(lowest, p)
			out[i].lag = max(out[i].lag, st.GetLagSeconds(), 0)
		}
		if !out[i].ok {
			// No index of the mode is listed: the coverage of the resolved scope stands in for it.
			if sc := resp.GetScope(); sc != nil {
				if p, ok := percentOf(sc.GetCoverage()); ok {
					out[i].ok, lowest = true, p
				}
			}
		}
		out[i].percent = lowest
		if rev := Hex(resp.GetScope().GetChangeset()); rev != "" {
			out[i].revision = rev[:min(len(rev), 7)]
		}
	})
	sum := 0
	for _, o := range out {
		if !o.ok {
			continue
		}
		cov.Known++
		sum += o.percent
		cov.LagSeconds = max(cov.LagSeconds, o.lag)
	}
	if cov.Known > 0 {
		cov.Percent = sum / cov.Known
	}
	full := cov.Known == cov.Asked && !cov.Sampled
	for _, o := range out {
		if o.ok && o.percent < 100 {
			full = false
		}
	}
	cov.Complete = full && cov.Known > 0
	if len(out) == 1 && out[0].ok {
		cov.Revision = out[0].revision
	}
	return cov
}

// Facet is a value with the number of files that have it.
type Facet = api.AscFacet

// maxFacets bounds a facet list.
const maxFacets = 8

// Facets counts the files of the hits by language and by their top-level directory.
func Facets(repos []RepoResult) (langs, dirs []Facet) {
	l, d := map[string]int{}, map[string]int{}
	for _, r := range repos {
		for _, f := range r.Files {
			if f.Lang != "" {
				l[f.Lang]++
			}
			if dir, _, ok := strings.Cut(f.Path, "/"); ok {
				d[dir+"/"]++
			} else {
				d["(root)"]++
			}
		}
	}
	top := func(m map[string]int) []Facet {
		out := make([]Facet, 0, len(m))
		for name, n := range m {
			out = append(out, Facet{Name: name, Count: n})
		}
		slices.SortFunc(out, func(a, b Facet) int {
			if a.Count != b.Count {
				return b.Count - a.Count
			}
			return strings.Compare(a.Name, b.Name)
		})
		if len(out) > maxFacets {
			out = out[:maxFacets]
		}
		return out
	}
	return top(l), top(d)
}
