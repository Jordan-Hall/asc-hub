// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// ErrInput marks a request that fails a check made here, before anything is sent to asc.
var ErrInput = errors.New("ascsearch: bad request")

func input(msg string) error { return errors.Join(ErrInput, errors.New(msg)) }

const (
	// reposPerCall is the most repositories one CrossSearch call takes (CrossSearchTarget.repos).
	reposPerCall = 64
	// DefaultLimit and MaxLimit bound the file hits of a text search.
	DefaultLimit = 50
	MaxLimit     = 200
	// perRepoFiles bounds the files one repository adds to a fallback search.
	perRepoFiles = 50
)

// TextQuery is a text search.
type TextQuery struct {
	Pattern       string
	Mode          ascv1.SearchMode
	CaseSensitive bool
	Context       uint32
	Paths         []string
	Exclude       []string
	Langs         []string
	// Limit bounds the file hits over all repositories; zero means DefaultLimit.
	Limit int
	// PageToken continues an earlier search over the same single call of up to 64 repositories.
	PageToken string
}

// TextResult is the answer to a text search.
type TextResult struct {
	// Repos lists the repositories that have hits, in the order of the request.
	Repos []RepoResult
	// Files counts the file hits over all repositories.
	Files     int
	Truncated bool
	// NextPageToken is set only when the whole search was one call.
	NextPageToken string
	// Searched counts readable repositories searched; Unindexed those searched without an index (slower).
	Searched  int
	Unindexed int
	EstTokens uint32
	// Fallback says asc lacks the cross-repository service and each repository was searched on its own.
	Fallback bool
	// Failed counts repositories whose search failed for a reason other than "asc refuses or does not know it".
	Failed int
}

func (q *TextQuery) check() error {
	if len(q.Pattern) < MinPattern || len(q.Pattern) > MaxPattern || !Plain(q.Pattern, MaxPattern) {
		return input("the pattern must be 3 to 512 bytes without control characters")
	}
	if q.Context > MaxContext {
		return input("too many context lines")
	}
	if len(q.Paths) > MaxPaths || len(q.Exclude) > MaxPaths || len(q.Langs) > MaxLangs {
		return input("too many scope values")
	}
	for _, p := range append(slices.Clone(q.Paths), q.Exclude...) {
		if c, ok := CheckPath(p); !ok || c == "" {
			return input("bad path")
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

func (q *TextQuery) limit() int {
	switch {
	case q.Limit <= 0:
		return DefaultLimit
	case q.Limit > MaxLimit:
		return MaxLimit
	}
	return q.Limit
}

// parallel runs fn for 0..n-1, at most limit at once, and waits.
func parallel(n, limit int, fn func(i int)) {
	sem := make(chan struct{}, max(limit, 1))
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}

// recvAll reads a bounded server stream.
func recvAll[T any](s grpc.ServerStreamingClient[T]) ([]*T, error) {
	var out []*T
	for range maxResponses {
		m, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// chunkResult is what one CrossSearch call returned.
type chunkResult struct {
	hits      map[string]*ascv1.RepoHits
	truncated bool
	next      string
	searched  uint32
	unindexed uint32
	est       uint32
	err       error
}

// Text searches the repositories. The caller has decided that the context's credential may read them;
// asc decides again when the credential is a user's.
func (s *Service) Text(ctx context.Context, repos []Repo, q TextQuery) (*TextResult, error) {
	if err := q.check(); err != nil {
		return nil, err
	}
	if len(repos) > MaxRepos {
		return nil, input("too many repositories")
	}
	res := &TextResult{}
	if len(repos) == 0 {
		return res, nil
	}
	chunks := slices.Collect(slices.Chunk(repos, reposPerCall))
	if q.PageToken != "" && len(chunks) > 1 {
		return nil, input("a page token continues a search of at most 64 repositories")
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	// The first chunk decides whether asc has the service at all.
	out := make([]chunkResult, len(chunks))
	out[0] = s.crossChunk(ctx, chunks[0], q)
	if unimplemented(out[0].err) {
		return s.textFallback(ctx, repos, q)
	}
	if len(chunks) > 1 {
		parallel(len(chunks)-1, 4, func(i int) { out[i+1] = s.crossChunk(ctx, chunks[i+1], q) })
	}

	byRepo := map[string]*ascv1.RepoHits{}
	var first error
	for i, c := range out {
		if errors.Is(c.err, ascclient.ErrNoCredential) {
			return nil, c.err
		}
		switch {
		case c.err == nil:
		case refused(c.err):
			continue // a chunk asc refuses adds nothing
		default:
			res.Failed += len(chunks[i])
			if first == nil {
				first = c.err
			}
			continue
		}
		res.Truncated = res.Truncated || c.truncated
		res.Searched += int(c.searched)
		res.Unindexed += int(c.unindexed)
		res.EstTokens += c.est
		for name, h := range c.hits {
			byRepo[name] = h
		}
		if len(chunks) == 1 {
			res.NextPageToken = OutToken(c.next)
		}
	}
	if res.Failed == len(repos) {
		return nil, first
	}
	res.Searched = min(res.Searched, len(repos))
	res.Unindexed = min(res.Unindexed, res.Searched)

	limit := q.limit()
	for _, r := range repos {
		h, ok := byRepo[r.Name]
		if !ok {
			continue
		}
		rr, n, cut := repoResultOf(r, h.GetRev(), h.GetScope(), h.GetFiles(), limit-res.Files)
		res.Truncated = res.Truncated || cut
		if n > 0 {
			res.Repos = append(res.Repos, rr)
			res.Files += n
		}
	}
	if res.Files >= limit {
		res.Truncated = true
	}
	return res, nil
}

// repoResultOf converts the hits of one repository, at most room of them. It returns the number of hits
// kept and whether some were left out.
func repoResultOf(r Repo, rev string, scope *ascv1.ResolvedScope, files []*ascv1.FileHit, room int) (RepoResult, int, bool) {
	rr := RepoResult{Repo: r.Name, Rev: Clean([]byte(rev), 255), Scope: ScopeOf(scope), Files: []FileHit{}}
	cut := false
	for _, f := range files {
		if len(rr.Files) >= maxFilesPerRepo || len(rr.Files) >= room {
			cut = true
			break
		}
		if h, ok := FileHitOf(f); ok {
			rr.Files = append(rr.Files, h)
		}
	}
	return rr, len(rr.Files), cut
}

func (s *Service) crossChunk(ctx context.Context, repos []Repo, q TextQuery) chunkResult {
	names := make([]string, len(repos))
	allowed := make(map[string]bool, len(repos))
	for i, r := range repos {
		names[i] = r.Name
		allowed[r.Name] = true
	}
	stream, err := s.Cross.Search(ctx, &ascv1.CrossSearchRequest{
		Target:        &ascv1.CrossSearchTarget{Repos: names},
		Pattern:       q.Pattern,
		Mode:          q.Mode,
		CaseSensitive: q.CaseSensitive,
		ContextLines:  q.Context,
		Paths:         q.Paths,
		ExcludePaths:  q.Exclude,
		Langs:         q.Langs,
		Budget:        &ascv1.Budget{MaxResults: uint32(q.limit())},
		PageToken:     q.PageToken,
	})
	if err != nil {
		return chunkResult{err: err}
	}
	msgs, err := recvAll(stream)
	if err != nil {
		return chunkResult{err: err}
	}
	c := chunkResult{hits: map[string]*ascv1.RepoHits{}}
	for _, m := range msgs {
		c.truncated = c.truncated || m.GetTruncated()
		c.next = m.GetNextPageToken()
		c.searched = max(c.searched, m.GetSearchedRepos())
		c.unindexed = max(c.unindexed, m.GetUnindexedRepos())
		c.est += m.GetEstTokens()
		for _, rh := range m.GetRepos() {
			name := rh.GetRepo().GetName()
			if !allowed[name] { // a repository nobody asked for is dropped, not shown
				continue
			}
			if have, ok := c.hits[name]; ok {
				have.Files = append(have.Files, rh.GetFiles()...)
				continue
			}
			c.hits[name] = &ascv1.RepoHits{Repo: rh.GetRepo(), Rev: rh.GetRev(), Scope: rh.GetScope(), Files: slices.Clone(rh.GetFiles())}
		}
	}
	return c
}

// textFallback searches each repository with CodeIntel.Search, for an asc that lacks CrossSearch.
func (s *Service) textFallback(ctx context.Context, repos []Repo, q TextQuery) (*TextResult, error) {
	if q.PageToken != "" {
		return nil, input("this asc server does not continue a search across repositories")
	}
	type one struct {
		files []*ascv1.FileHit
		scope *ascv1.ResolvedScope
		cut   bool
		est   uint32
		err   error
	}
	out := make([]one, len(repos))
	per := min(q.limit(), perRepoFiles)
	parallel(len(repos), fanOut, func(i int) {
		if ctx.Err() != nil {
			out[i].err = ctx.Err()
			return
		}
		stream, err := s.Intel.Search(ctx, &ascv1.SearchRequest{
			Scope: &ascv1.Scope{
				Repo:         &ascv1.RepoRef{Name: repos[i].Name, IdMode: ascv1.IdMode_ID_MODE_GIT},
				At:           &ascv1.Scope_Rev{Rev: repos[i].Rev},
				Paths:        q.Paths,
				ExcludePaths: q.Exclude,
				Langs:        q.Langs,
			},
			Pattern:       q.Pattern,
			Mode:          q.Mode,
			CaseSensitive: q.CaseSensitive,
			ContextLines:  q.Context,
			Budget:        &ascv1.Budget{MaxResults: uint32(per)},
		})
		if err != nil {
			out[i].err = err
			return
		}
		msgs, err := recvAll(stream)
		if err != nil {
			out[i].err = err
			return
		}
		for _, m := range msgs {
			out[i].files = append(out[i].files, m.GetFiles()...)
			if m.GetScope() != nil {
				out[i].scope = m.GetScope()
			}
			out[i].cut = out[i].cut || m.GetTruncated()
			out[i].est += m.GetEstTokens()
		}
	})

	res := &TextResult{Fallback: true}
	var first error
	for i := range out {
		if errors.Is(out[i].err, ascclient.ErrNoCredential) {
			return nil, out[i].err
		}
		switch {
		case out[i].err == nil:
		case refused(out[i].err):
			out[i].files = nil
			continue
		default:
			res.Failed++
			if first == nil {
				first = out[i].err
			}
			continue
		}
		res.Searched++
		if sc := out[i].scope; sc != nil && sc.GetCoverage() < 0.01 {
			res.Unindexed++
		}
		res.EstTokens += out[i].est
		res.Truncated = res.Truncated || out[i].cut
	}
	if res.Failed == len(repos) {
		return nil, first
	}
	limit := q.limit()
	for i, r := range repos {
		if out[i].err != nil || len(out[i].files) == 0 {
			continue
		}
		rr, n, cut := repoResultOf(r, r.Rev, out[i].scope, out[i].files, limit-res.Files)
		res.Truncated = res.Truncated || cut
		if n > 0 {
			res.Repos = append(res.Repos, rr)
			res.Files += n
		}
	}
	if res.Files >= limit {
		res.Truncated = true
	}
	return res, nil
}
