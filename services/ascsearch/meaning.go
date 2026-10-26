// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"
	"slices"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// rrfK is the constant of reciprocal rank fusion, score = sum 1/(k+rank).
const rrfK = 60

// RRF merges ranked lists with reciprocal rank fusion: an item at 0-based position r of a list scores
// 1/(rrfK+r+1), and items sort by score, highest first. Ties keep the order of the lists, then of the
// positions, so the result is deterministic.
func RRF[T any](lists [][]T) []T {
	type scored struct {
		item  T
		score float64
		list  int
		pos   int
	}
	var all []scored
	for li, l := range lists {
		for pos, item := range l {
			all = append(all, scored{item: item, score: 1 / float64(rrfK+pos+1), list: li, pos: pos})
		}
	}
	slices.SortStableFunc(all, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		case a.list != b.list:
			return a.list - b.list
		}
		return a.pos - b.pos
	})
	out := make([]T, len(all))
	for i, s := range all {
		out[i] = s.item
	}
	return out
}

// MeaningQuery is a search by meaning.
type MeaningQuery struct {
	Query string
	// K is how many chunks to return; zero means 10, at most 50.
	K      int
	Rerank bool
	Paths  []string
	Langs  []string
}

// MeaningResult is the answer to a search by meaning.
type MeaningResult struct {
	Chunks []Chunk
	Model  string
	// Searched counts repositories that answered.
	Searched     int
	Failed       int
	EstTokens    uint32
	AnnExhausted bool
}

const maxK = 50

func (q *MeaningQuery) k() int {
	switch {
	case q.K <= 0:
		return 10
	case q.K > maxK:
		return maxK
	}
	return q.K
}

func (q *MeaningQuery) check() error {
	if q.Query == "" || len(q.Query) > MaxSemantic || !Plain(q.Query, MaxSemantic) {
		return input("the question must be 1 to 1000 bytes without control characters")
	}
	if len(q.Paths) > MaxPaths || len(q.Langs) > MaxLangs {
		return input("too many scope values")
	}
	for _, p := range q.Paths {
		if c, ok := CheckPath(p); !ok || c == "" {
			return input("bad path")
		}
	}
	for _, l := range q.Langs {
		if !CheckLang(l) {
			return input("bad language")
		}
	}
	return nil
}

// Meaning asks every repository the question with CodeIntel.Semantic and merges the ranked chunks with
// reciprocal rank fusion. Each repository costs a query embedding, so a search covers at most
// MaxSemanticRepos of them; the caller says which.
func (s *Service) Meaning(ctx context.Context, repos []Repo, q MeaningQuery) (*MeaningResult, error) {
	if err := q.check(); err != nil {
		return nil, err
	}
	if len(repos) > MaxSemanticRepos {
		return nil, input("too many repositories for a search by meaning")
	}
	res := &MeaningResult{}
	if len(repos) == 0 {
		return res, nil
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	out := make([]struct {
		resp *ascv1.SemanticResponse
		err  error
	}, len(repos))
	k := q.k()
	parallel(len(repos), fanOut, func(i int) {
		if ctx.Err() != nil {
			out[i].err = ctx.Err()
			return
		}
		out[i].resp, out[i].err = s.Intel.Semantic(ctx, &ascv1.SemanticRequest{
			Scope: &ascv1.Scope{
				Repo:  &ascv1.RepoRef{Name: repos[i].Name, IdMode: ascv1.IdMode_ID_MODE_GIT},
				At:    &ascv1.Scope_Rev{Rev: repos[i].Rev},
				Paths: q.Paths,
				Langs: q.Langs,
			},
			Query: q.Query, K: uint32(k), Budget: &ascv1.Budget{MaxResults: uint32(k)}, Rerank: q.Rerank,
		})
	})
	var first error
	lists := make([][]Chunk, len(repos))
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
		res.Searched++
		res.EstTokens += o.resp.GetEstTokens()
		res.AnnExhausted = res.AnnExhausted || o.resp.GetAnnExhausted()
		if res.Model == "" {
			res.Model = Label(o.resp.GetModel())
		}
		for _, c := range o.resp.GetChunks() {
			if len(lists[i]) >= k {
				break
			}
			if ch, ok := ChunkOf(repos[i].Name, c); ok {
				lists[i] = append(lists[i], ch)
			}
		}
	}
	if res.Failed == len(repos) {
		return nil, first
	}
	// One repository keeps its own order (asc ranked it); several are fused by rank.
	merged := RRF(lists)
	if len(merged) > k {
		merged = merged[:k]
	}
	res.Chunks = merged
	return res, nil
}
