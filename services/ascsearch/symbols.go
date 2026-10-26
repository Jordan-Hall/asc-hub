// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"
	"slices"
	"strings"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// SymbolQuery looks a symbol up by name over several repositories.
type SymbolQuery struct {
	Name            string
	Match           ascv1.SymbolMatch
	Kinds           []string
	Langs           []string
	DefinitionsOnly bool
	// Limit bounds the hits; zero means DefaultLimit.
	Limit     int
	PageToken string
}

// SymbolResult is the answer to a symbol lookup.
type SymbolResult struct {
	Hits          []SymbolHit
	Truncated     bool
	NextPageToken string
	// Fallback says asc lacks the cross-repository service and each repository was asked on its own.
	Fallback bool
	Failed   int
}

const maxKinds = 8

func (q *SymbolQuery) check() error {
	if q.Name == "" || len(q.Name) > MaxName || !Plain(q.Name, MaxName) {
		return input("the name must be 1 to 256 bytes without control characters")
	}
	if len(q.Kinds) > maxKinds || len(q.Langs) > MaxLangs {
		return input("too many scope values")
	}
	for _, k := range q.Kinds {
		if k == "" || !Plain(k, 64) {
			return input("bad kind")
		}
	}
	for _, l := range q.Langs {
		if !CheckLang(l) {
			return input("bad language")
		}
	}
	if !CheckToken(q.PageToken) {
		return input("bad page token")
	}
	return nil
}

func (q *SymbolQuery) limit() int {
	switch {
	case q.Limit <= 0:
		return DefaultLimit
	case q.Limit > MaxLimit:
		return MaxLimit
	}
	return q.Limit
}

// Symbols looks the name up in the repositories.
func (s *Service) Symbols(ctx context.Context, repos []Repo, q SymbolQuery) (*SymbolResult, error) {
	if err := q.check(); err != nil {
		return nil, err
	}
	if len(repos) > MaxRepos {
		return nil, input("too many repositories")
	}
	res := &SymbolResult{}
	if len(repos) == 0 {
		return res, nil
	}
	chunks := slices.Collect(slices.Chunk(repos, reposPerCall))
	if q.PageToken != "" && len(chunks) > 1 {
		return nil, input("a page token continues a lookup in at most 64 repositories")
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	type chunkOut struct {
		resp *ascv1.LookupSymbolResponse
		err  error
	}
	call := func(c []Repo) chunkOut {
		names := make([]string, len(c))
		for i, r := range c {
			names[i] = r.Name
		}
		resp, err := s.Cross.LookupSymbol(ctx, &ascv1.LookupSymbolRequest{
			Target: &ascv1.CrossSearchTarget{Repos: names}, Name: q.Name, Match: q.Match, Kinds: q.Kinds,
			Langs: q.Langs, DefinitionsOnly: q.DefinitionsOnly, Limit: uint32(q.limit()), PageToken: q.PageToken,
		})
		return chunkOut{resp, err}
	}
	out := make([]chunkOut, len(chunks))
	out[0] = call(chunks[0])
	if unimplemented(out[0].err) {
		return s.symbolsFallback(ctx, repos, q)
	}
	if len(chunks) > 1 {
		parallel(len(chunks)-1, 4, func(i int) { out[i+1] = call(chunks[i+1]) })
	}

	allowed := make(map[string]Repo, len(repos))
	for _, r := range repos {
		allowed[r.Name] = r
	}
	var first error
	for i, c := range out {
		if errors.Is(c.err, ascclient.ErrNoCredential) {
			return nil, c.err
		}
		switch {
		case c.err == nil:
		case refused(c.err):
			continue
		default:
			res.Failed += len(chunks[i])
			if first == nil {
				first = c.err
			}
			continue
		}
		res.Truncated = res.Truncated || c.resp.GetTruncated()
		if len(chunks) == 1 {
			res.NextPageToken = OutToken(c.resp.GetNextPageToken())
		}
		for _, h := range c.resp.GetHits() {
			name := h.GetRepo().GetName()
			if _, ok := allowed[name]; !ok { // a repository nobody asked for is dropped, not shown
				continue
			}
			sym, ok := SymbolOf(h.GetSymbol())
			if !ok || (q.DefinitionsOnly && !sym.IsDef) {
				continue
			}
			res.Hits = append(res.Hits, SymbolHit{Repo: name, Rev: Clean([]byte(h.GetRev()), 255), Symbol: sym, Score: finite(h.GetScore())})
		}
	}
	if res.Failed == len(repos) {
		return nil, first
	}
	finish(res, q.limit(), repos)
	return res, nil
}

func finite(f float32) float32 {
	if f != f || f > 1e9 || f < -1e9 {
		return 0
	}
	return f
}

// finish orders the hits by score (best first, then the order of the repositories, then position) and cuts
// them at limit.
func finish(res *SymbolResult, limit int, repos []Repo) {
	order := make(map[string]int, len(repos))
	for i, r := range repos {
		order[r.Name] = i
	}
	slices.SortStableFunc(res.Hits, func(a, b SymbolHit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return order[a.Repo] - order[b.Repo]
	})
	if len(res.Hits) > limit {
		res.Hits, res.Truncated = res.Hits[:limit], true
	}
}

// symbolsFallback asks each repository with CodeIntel.Symbols (FIND), for an asc that lacks CrossSearch.
func (s *Service) symbolsFallback(ctx context.Context, repos []Repo, q SymbolQuery) (*SymbolResult, error) {
	if q.PageToken != "" {
		return nil, input("this asc server does not continue a lookup across repositories")
	}
	type one struct {
		resp *ascv1.SymbolsResponse
		err  error
	}
	out := make([]one, len(repos))
	parallel(len(repos), fanOut, func(i int) {
		if ctx.Err() != nil {
			out[i].err = ctx.Err()
			return
		}
		out[i].resp, out[i].err = s.Intel.Symbols(ctx, &ascv1.SymbolsRequest{
			Scope: &ascv1.Scope{
				Repo:  &ascv1.RepoRef{Name: repos[i].Name, IdMode: ascv1.IdMode_ID_MODE_GIT},
				At:    &ascv1.Scope_Rev{Rev: repos[i].Rev},
				Langs: q.Langs,
			},
			Op: ascv1.SymbolOp_SYMBOL_OP_FIND, Name: q.Name, Kinds: q.Kinds,
			Budget: &ascv1.Budget{MaxResults: uint32(min(q.limit(), perRepoFiles))},
		})
	})
	res := &SymbolResult{Fallback: true}
	var first error
	for i, o := range out {
		if errors.Is(o.err, ascclient.ErrNoCredential) {
			return nil, o.err
		}
		switch {
		case o.err == nil:
		case refused(o.err):
			continue
		default:
			res.Failed++
			if first == nil {
				first = o.err
			}
			continue
		}
		res.Truncated = res.Truncated || o.resp.GetTruncated()
		for _, s := range o.resp.GetSymbols() {
			sym, ok := SymbolOf(s)
			if !ok || (q.DefinitionsOnly && !sym.IsDef) {
				continue
			}
			score, keep := matchScore(sym.Name, q.Name, q.Match)
			if !keep {
				continue
			}
			res.Hits = append(res.Hits, SymbolHit{Repo: repos[i].Name, Rev: repos[i].Rev, Symbol: sym, Score: score})
		}
	}
	if res.Failed == len(repos) {
		return nil, first
	}
	finish(res, q.limit(), repos)
	return res, nil
}

// matchScore rates a name against the wanted one and says whether the match kind keeps it.
func matchScore(have, want string, m ascv1.SymbolMatch) (float32, bool) {
	switch {
	case have == want:
		return 1, true
	case m == ascv1.SymbolMatch_SYMBOL_MATCH_EXACT:
		return 0, false
	case strings.EqualFold(have, want):
		return 0.9, true
	case strings.HasPrefix(strings.ToLower(have), strings.ToLower(want)):
		return 0.7, true
	case m == ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX:
		return 0, false
	case strings.Contains(strings.ToLower(have), strings.ToLower(want)):
		return 0.4, true
	}
	return 0.2, true
}
