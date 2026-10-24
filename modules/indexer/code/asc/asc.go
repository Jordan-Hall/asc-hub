// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package asc is the Forgejo code-indexer backend `asc` (SPEC section 23.8, PLAN unit I23), selected by
// `[indexer] REPO_INDEXER_TYPE = asc`.
//
// The asc server indexes on ingest, so Index and Delete do nothing. Search calls CodeIntel.Search at the
// default branch of every repository asked about, with the token of the user who searches: the context
// of the call must carry it (ascclient.WithUserToken). The backend never switches to the internal token,
// so a search without a user token fails instead of widening into a system call.
//
// Result text is whatever the server rendered. The server redacts secrets (SPEC section 23.7), and this
// package never rebuilds a line from anything else; it only drops invalid UTF-8 and control characters
// again as a second layer, caps what it reads, and lets the highlighter escape the HTML.
//
// TODO(I23-wire): modules/indexer/code/indexer.go must accept REPO_INDEXER_TYPE = asc and build
// NewIndexer(ascclient.CodeIntel(), nil); that file belongs to another unit.
package asc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/indexer/code/internal"
	"forgejo.org/modules/timeutil"

	"github.com/go-enry/go-enry/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Limits on what one search reads from the server, so a hostile or buggy answer stays bounded.
const (
	// MaxRepos bounds the repositories of one search.
	MaxRepos = 256
	// maxFilesPerRepo bounds the file hits kept per repository.
	maxFilesPerRepo = 1000
	// maxMatchesPerFile bounds the matching lines kept per file.
	maxMatchesPerFile = 50
	// maxContextLines bounds the context lines kept per side of a match.
	maxContextLines = 3
	// maxLineBytes cuts one line of text.
	maxLineBytes = 4096
	// maxPathBytes bounds a path from the server.
	maxPathBytes = 4096
	// maxQueryBytes bounds the keyword.
	maxQueryBytes = 4096
	// maxResponses bounds the stream messages read per repository.
	maxResponses = 64
)

// ErrNoRepos is returned for a search that names no repository: the backend has no global index.
var ErrNoRepos = errors.New("asc indexer: a search needs at least one repository")

// RepoResolver maps a Forgejo repository id to the asc repository name ("owner/repo") and the branch to
// search.
type RepoResolver func(ctx context.Context, repoID int64) (name, branch string, err error)

// DBResolver resolves repositories from the database and searches the default branch.
func DBResolver(ctx context.Context, repoID int64) (string, string, error) {
	repo, err := repo_model.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return "", "", err
	}
	if repo.DefaultBranch == "" {
		return "", "", fmt.Errorf("repository %d has no default branch", repoID)
	}
	return repo.FullName(), repo.DefaultBranch, nil
}

// Indexer is the `asc` code indexer.
type Indexer struct {
	api     ascv1.CodeIntelClient
	resolve RepoResolver
}

var _ internal.Indexer = (*Indexer)(nil)

// NewIndexer binds an Indexer to a CodeIntel client (ascclient.Client.CodeIntel()). A nil resolver means
// DBResolver.
func NewIndexer(api ascv1.CodeIntelClient, resolve RepoResolver) *Indexer {
	if resolve == nil {
		resolve = DBResolver
	}
	return &Indexer{api: api, resolve: resolve}
}

// Init reports an existing index: the server owns the data, so Forgejo never populates it.
func (b *Indexer) Init(_ context.Context) (bool, error) {
	if b.api == nil {
		return false, errors.New("asc indexer: no asc client (is [asc] ENABLED?)")
	}
	return true, nil
}

// Ping checks that the indexer has a client.
func (b *Indexer) Ping(ctx context.Context) error {
	_, err := b.Init(ctx)
	return err
}

// Close does nothing; the client belongs to the process.
func (b *Indexer) Close() {}

// Index does nothing: the asc server indexes on ingest.
func (b *Indexer) Index(_ context.Context, _ *repo_model.Repository, _ string, _ *internal.RepoChanges) error {
	return nil
}

// Delete does nothing: the asc server owns the index.
func (b *Indexer) Delete(_ context.Context, _ int64) error { return nil }

// Formatter renders the lines of a SearchResult built by Search.
func (b *Indexer) Formatter() internal.ResultFormatter { return formatter{} }

// pattern turns the keyword and mode into an asc pattern and mode.
func pattern(keyword string, mode internal.CodeSearchMode) (string, ascv1.SearchMode) {
	if mode == internal.CodeSearchModeUnion {
		words := strings.Fields(keyword)
		for i, w := range words {
			words[i] = regexp.QuoteMeta(w)
		}
		return strings.Join(words, "|"), ascv1.SearchMode_SEARCH_MODE_REGEX
	}
	return keyword, ascv1.SearchMode_SEARCH_MODE_LITERAL
}

// Search runs the keyword over the default branch of each repository in opts.RepoIDs. ctx must carry the
// searching user's token. A repository the user may not read, or that asc does not know, adds nothing.
func (b *Indexer) Search(ctx context.Context, opts *internal.SearchOptions) (int64, []*internal.SearchResult, []*internal.SearchResultLanguages, error) {
	if b.api == nil {
		return 0, nil, nil, errors.New("asc indexer: no asc client")
	}
	if opts == nil || opts.Keyword == "" {
		return 0, nil, nil, nil
	}
	if len(opts.Keyword) > maxQueryBytes {
		return 0, nil, nil, errors.New("asc indexer: the search text is too long")
	}
	if len(opts.RepoIDs) == 0 {
		return 0, nil, nil, ErrNoRepos
	}
	if len(opts.RepoIDs) > MaxRepos {
		return 0, nil, nil, fmt.Errorf("asc indexer: more than %d repositories", MaxRepos)
	}
	pat, mode := pattern(opts.Keyword, opts.Mode)

	var all []*internal.SearchResult
	for _, id := range opts.RepoIDs {
		if err := ctx.Err(); err != nil {
			return 0, nil, nil, err
		}
		name, branch, err := b.resolve(ctx, id)
		if err != nil {
			return 0, nil, nil, err
		}
		scope := &ascv1.Scope{
			Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
			At:   &ascv1.Scope_Rev{Rev: branch},
		}
		if opts.Language != "" {
			scope.Langs = []string{opts.Language}
		}
		hits, err := b.searchRepo(ctx, &ascv1.SearchRequest{
			Scope:         scope,
			Pattern:       pat,
			Mode:          mode,
			CaseSensitive: opts.Mode == internal.CodeSearchModeExact,
			ContextLines:  1,
			Budget:        &ascv1.Budget{MaxResults: maxFilesPerRepo},
		})
		if err != nil {
			if c := status.Code(err); c == codes.PermissionDenied || c == codes.NotFound {
				continue
			}
			return 0, nil, nil, err
		}
		for _, h := range hits {
			if opts.Filename != "" && !strings.Contains(strings.ToLower(string(h.GetPath())), strings.ToLower(opts.Filename)) {
				continue
			}
			if r := toResult(id, branch, h); r != nil {
				all = append(all, r)
			}
		}
	}

	langs := languageCounts(all)
	total := int64(len(all))
	skip, take := 0, 0
	if opts.Paginator != nil {
		skip, take = opts.GetSkipTake()
	}
	if skip >= len(all) {
		return total, nil, langs, nil
	}
	end := len(all)
	if take > 0 {
		end = min(end, skip+take)
	}
	return total, all[skip:end], langs, nil
}

// searchRepo reads the bounded stream of one request.
func (b *Indexer) searchRepo(ctx context.Context, req *ascv1.SearchRequest) ([]*ascv1.FileHit, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := b.api.Search(ctx, req)
	if err != nil {
		return nil, err
	}
	var hits []*ascv1.FileHit
	for range maxResponses {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return hits, nil
		}
		if err != nil {
			return nil, err
		}
		for _, f := range resp.GetFiles() {
			if len(hits) >= maxFilesPerRepo {
				return hits, nil
			}
			hits = append(hits, f)
		}
	}
	return hits, nil
}

// clean makes server text safe to show: valid UTF-8, no control characters except tab, capped.
func clean(b []byte) string {
	if len(b) > maxLineBytes {
		b = b[:maxLineBytes]
	}
	s := strings.ToValidUTF8(string(b), "�")
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if unicode.IsControl(r) || isBidiOrSeparator(r) {
			return -1
		}
		return r
	}, s)
}

type shownLine struct {
	text string
	hit  bool
	cols []uint32
}

// toResult builds a SearchResult from a hit: the lines shown go into Content one per line, with their real
// numbers in LineNumbers, so the formatter can number them. A redacted line arrives with no text and no
// cols, and stays visible empty.
func toResult(repoID int64, branch string, h *ascv1.FileHit) *internal.SearchResult {
	if len(h.GetPath()) == 0 || len(h.GetPath()) > maxPathBytes {
		return nil
	}
	byNum := map[int]*shownLine{}
	add := func(num int, text string, hit bool, cols []uint32) {
		if num < 1 {
			return
		}
		if l, ok := byNum[num]; ok {
			if hit {
				l.text, l.hit, l.cols = text, true, cols
			}
			return
		}
		byNum[num] = &shownLine{text: text, hit: hit, cols: cols}
	}
	for i, m := range h.GetMatches() {
		if i >= maxMatchesPerFile {
			break
		}
		n := int(m.GetLine())
		before, after := m.GetBefore(), m.GetAfter()
		if len(before) > maxContextLines {
			before = before[len(before)-maxContextLines:]
		}
		after = after[:min(len(after), maxContextLines)]
		for j, t := range before {
			add(n-len(before)+j, clean(t), false, nil)
		}
		add(n, clean(m.GetText()), true, m.GetCols())
		for j, t := range after {
			add(n+1+j, clean(t), false, nil)
		}
	}
	nums := make([]int, 0, len(byNum))
	for n := range byNum {
		nums = append(nums, n)
	}
	slices.Sort(nums)

	res := &internal.SearchResult{
		RepoID:      repoID,
		Filename:    clean(h.GetPath()),
		CommitID:    branch,
		UpdatedUnix: timeutil.TimeStampNow(),
		Language:    h.GetLang(),
		Color:       enry.GetColor(h.GetLang()),
	}
	var sb strings.Builder
	for _, n := range nums {
		l := byNum[n]
		start := sb.Len()
		sb.WriteString(l.text)
		sb.WriteByte('\n')
		res.LineNumbers = append(res.LineNumbers, n)
		res.LineOffsets = append(res.LineOffsets, start)
		if !l.hit {
			continue
		}
		// cols are start columns in bytes; the width is not on the wire, so each starts a one-rune mark.
		for _, c := range l.cols {
			if int(c) < len(l.text) {
				_, w := utf8.DecodeRuneInString(l.text[c:])
				res.Matches = append(res.Matches, internal.Match{Start: start + int(c), End: start + int(c) + w, LineNumber: n})
			}
		}
	}
	res.Content = sb.String()
	if len(res.Matches) > 0 {
		res.StartIndex, res.EndIndex = res.Matches[0].Start, res.Matches[0].End
	}
	return res
}

func languageCounts(rs []*internal.SearchResult) []*internal.SearchResultLanguages {
	idx := map[string]*internal.SearchResultLanguages{}
	var out []*internal.SearchResultLanguages
	for _, r := range rs {
		if r.Language == "" {
			continue
		}
		l, ok := idx[r.Language]
		if !ok {
			l = &internal.SearchResultLanguages{Language: r.Language, Color: r.Color}
			idx[r.Language] = l
			out = append(out, l)
		}
		l.Count++
	}
	slices.SortFunc(out, func(a, b *internal.SearchResultLanguages) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Language, b.Language)
	})
	return out
}

// isBidiOrSeparator reports the characters that reorder or split displayed text: the bidirectional
// marks and overrides (which can disguise code, "Trojan Source") and the line and paragraph separators.
func isBidiOrSeparator(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F, r == 0x2028, r == 0x2029:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
