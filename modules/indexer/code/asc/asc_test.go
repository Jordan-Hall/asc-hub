// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"forgejo.org/models/db"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/indexer/code/internal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeStream struct {
	grpc.ClientStream
	msgs []*ascv1.SearchResponse
	err  error
}

func (s *fakeStream) Recv() (*ascv1.SearchResponse, error) {
	if len(s.msgs) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	m := s.msgs[0]
	s.msgs = s.msgs[1:]
	return m, nil
}

type fakeAPI struct {
	ascv1.CodeIntelClient
	reqs  []*ascv1.SearchRequest
	byRep map[string]*fakeStream
}

func (f *fakeAPI) Search(_ context.Context, in *ascv1.SearchRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.SearchResponse], error) {
	f.reqs = append(f.reqs, in)
	s, ok := f.byRep[in.GetScope().GetRepo().GetName()]
	if !ok {
		return nil, status.Error(codes.NotFound, "no such repository")
	}
	return s, nil
}

func resolver(_ context.Context, id int64) (string, string, error) {
	switch id {
	case 1:
		return "alice/one", "master", nil
	case 2:
		return "bob/two", "trunk", nil
	}
	return "", "", errors.New("unknown repo")
}

func hit(path string, line uint32, text string, cols ...uint32) *ascv1.FileHit {
	return &ascv1.FileHit{
		Path: []byte(path),
		Lang: "Go",
		Matches: []*ascv1.LineMatch{{
			Line:   line,
			Cols:   cols,
			Text:   []byte(text),
			Before: [][]byte{[]byte("before")},
			After:  [][]byte{[]byte("after")},
		}},
	}
}

func resp(files ...*ascv1.FileHit) *ascv1.SearchResponse { return &ascv1.SearchResponse{Files: files} }

func TestIndexAndDeleteAreNoOps(t *testing.T) {
	b := NewIndexer(&fakeAPI{}, resolver)
	require.NoError(t, b.Index(t.Context(), nil, "abc", nil))
	require.NoError(t, b.Delete(t.Context(), 1))
	existed, err := b.Init(t.Context())
	require.NoError(t, err)
	assert.True(t, existed)
	require.NoError(t, b.Ping(t.Context()))
	_, err = NewIndexer(nil, resolver).Init(t.Context())
	require.Error(t, err)
}

func TestSearchScopeAndMapping(t *testing.T) {
	api := &fakeAPI{byRep: map[string]*fakeStream{
		"alice/one": {msgs: []*ascv1.SearchResponse{resp(hit("a.go", 10, "func Foo()", 5))}},
		"bob/two":   {msgs: []*ascv1.SearchResponse{resp(hit("b/c.go", 3, "Foo()", 0), hit("zzz.go", 1, "Foo"))}},
	}}
	b := NewIndexer(api, resolver)
	opts := &internal.SearchOptions{RepoIDs: []int64{1, 2}, Keyword: "Foo", Language: "Go", Filename: "c.go"}
	total, results, langs, err := b.Search(t.Context(), opts)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total) // the filename filter drops a.go and zzz.go
	require.Len(t, results, 1)
	assert.Equal(t, "b/c.go", results[0].Filename)
	assert.EqualValues(t, 2, results[0].RepoID)
	require.Len(t, langs, 1)
	assert.Equal(t, 1, langs[0].Count)

	require.Len(t, api.reqs, 2)
	assert.Equal(t, "master", api.reqs[0].GetScope().GetRev())
	assert.Equal(t, "alice/one", api.reqs[0].GetScope().GetRepo().GetName())
	assert.Equal(t, "trunk", api.reqs[1].GetScope().GetRev())
	assert.Equal(t, []string{"Go"}, api.reqs[0].GetScope().GetLangs())
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_LITERAL, api.reqs[0].GetMode())
}

func TestSearchUnionIsEscapedRegex(t *testing.T) {
	api := &fakeAPI{byRep: map[string]*fakeStream{"alice/one": {}}}
	_, _, _, err := NewIndexer(api, resolver).Search(t.Context(), &internal.SearchOptions{
		RepoIDs: []int64{1}, Keyword: "a.b c(d", Mode: internal.CodeSearchModeUnion,
	})
	require.NoError(t, err)
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_REGEX, api.reqs[0].GetMode())
	assert.Equal(t, `a\.b|c\(d`, api.reqs[0].GetPattern())
}

func TestSearchInputBounds(t *testing.T) {
	b := NewIndexer(&fakeAPI{}, resolver)
	_, _, _, err := b.Search(t.Context(), &internal.SearchOptions{Keyword: "x"})
	assert.ErrorIs(t, err, ErrNoRepos)
	ids := make([]int64, MaxRepos+1)
	_, _, _, err = b.Search(t.Context(), &internal.SearchOptions{Keyword: "x", RepoIDs: ids})
	require.Error(t, err)
	_, _, _, err = b.Search(t.Context(), &internal.SearchOptions{Keyword: strings.Repeat("x", maxQueryBytes+1), RepoIDs: []int64{1}})
	require.Error(t, err)
	total, res, _, err := b.Search(t.Context(), &internal.SearchOptions{})
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, res)
}

func TestSearchSkipsRepositoriesTheUserCannotRead(t *testing.T) {
	api := &fakeAPI{byRep: map[string]*fakeStream{
		"alice/one": {err: status.Error(codes.PermissionDenied, "no")},
		"bob/two":   {msgs: []*ascv1.SearchResponse{resp(hit("b.go", 1, "Foo", 0))}},
	}}
	total, res, _, err := NewIndexer(api, resolver).Search(t.Context(), &internal.SearchOptions{RepoIDs: []int64{1, 2}, Keyword: "Foo"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, res, 1)
	assert.EqualValues(t, 2, res[0].RepoID)

	// Any other error is surfaced.
	api.byRep["alice/one"].err = status.Error(codes.Unavailable, "down")
	_, _, _, err = NewIndexer(api, resolver).Search(t.Context(), &internal.SearchOptions{RepoIDs: []int64{1}, Keyword: "Foo"})
	require.Error(t, err)
}

// A search without the user's token must fail, even when the client holds an internal token: the
// backend never widens a user search into a system call.
func TestSearchWithoutUserTokenIsRefused(t *testing.T) {
	client, err := ascclient.New(ascclient.Config{Target: "https://127.0.0.1:1", InternalToken: "internal-secret-token"})
	require.NoError(t, err)
	defer client.Close()
	b := NewIndexer(client.CodeIntel(), resolver)

	_, _, _, err = b.Search(t.Context(), &internal.SearchOptions{RepoIDs: []int64{1}, Keyword: "Foo"})
	require.ErrorIs(t, err, ascclient.ErrNoCredential)
}

func renderLines(t *testing.T, b *Indexer, h *ascv1.FileHit) string {
	t.Helper()
	r := toResult(1, "master", h)
	require.NotNil(t, r)
	out, err := b.Formatter().Format(r)
	require.NoError(t, err)
	var sb strings.Builder
	for _, l := range out.Lines {
		sb.WriteString(string(l.FormattedContent))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Only the server-rendered text is shown: a redacted line has no text, so the secret is absent.
func TestRedactedLineShowsNoText(t *testing.T) {
	h := &ascv1.FileHit{
		Path:     []byte("conf.go"),
		Redacted: true,
		Matches:  []*ascv1.LineMatch{{Line: 7}}, // text and cols removed by the server
	}
	html := renderLines(t, NewIndexer(&fakeAPI{}, resolver), h)
	assert.NotContains(t, html, "AKIA")
	r := toResult(1, "master", h)
	assert.Equal(t, []int{7}, r.LineNumbers)
	assert.Empty(t, r.Matches)
}

func TestServerTextIsEscapedAndCleaned(t *testing.T) {
	cr, bidi := string(rune(13)), string(rune(0x202e))
	h := hit("x.go", 4, "x := \"<script>alert(1)</script>\""+string([]byte{27})+"[31m"+string([]byte{0, 255}), 0)
	h.Matches[0].Before = [][]byte{[]byte("a" + cr + "b" + bidi)}
	html := renderLines(t, NewIndexer(&fakeAPI{}, resolver), h)
	assert.NotContains(t, html, "<script>")
	assert.Contains(t, html, "&lt;script&gt;")
	assert.NotContains(t, html, bidi)
	r := toResult(1, "master", h)
	for _, bad := range []string{string(rune(27)), string(rune(0)), cr, bidi} {
		assert.NotContains(t, r.Content, bad)
	}
	assert.True(t, utf8.ValidString(r.Content))
}

func TestLineNumbersAndBlocks(t *testing.T) {
	h := &ascv1.FileHit{Path: []byte("f.go"), Lang: "Go", Matches: []*ascv1.LineMatch{
		{Line: 5, Cols: []uint32{0}, Text: []byte("alpha"), Before: [][]byte{[]byte("b4")}, After: [][]byte{[]byte("b6")}},
		{Line: 6, Cols: []uint32{0}, Text: []byte("beta")},
		{Line: 40, Cols: []uint32{1}, Text: []byte("gamma")},
	}}
	r := toResult(1, "master", h)
	assert.Equal(t, []int{4, 5, 6, 40}, r.LineNumbers)
	out, err := formatter{}.Format(r)
	require.NoError(t, err)
	require.Len(t, out.Lines, 4)
	assert.Equal(t, 40, out.Lines[3].Num)
	assert.Contains(t, string(out.Lines[1].FormattedContent), "search-highlight")
}

func TestBoundsOnHostileAnswers(t *testing.T) {
	var ctxLines [][]byte
	for range 100 {
		ctxLines = append(ctxLines, []byte("c"))
	}
	m := &ascv1.LineMatch{Line: 500, Text: []byte(strings.Repeat("y", maxLineBytes*3)), Before: ctxLines, After: ctxLines, Cols: []uint32{1 << 30}}
	r := toResult(1, "master", &ascv1.FileHit{Path: []byte("f"), Matches: []*ascv1.LineMatch{m}})
	assert.Len(t, r.LineNumbers, 2*maxContextLines+1)
	assert.Empty(t, r.Matches)
	assert.LessOrEqual(t, len(r.Content), (2*maxContextLines+1)*(maxLineBytes+1))

	assert.Nil(t, toResult(1, "master", &ascv1.FileHit{Path: nil}))
	assert.Nil(t, toResult(1, "master", &ascv1.FileHit{Path: make([]byte, maxPathBytes+1)}))

	many := make([]*ascv1.FileHit, maxFilesPerRepo+50)
	for i := range many {
		many[i] = &ascv1.FileHit{Path: []byte("f")}
	}
	api := &fakeAPI{byRep: map[string]*fakeStream{"alice/one": {msgs: []*ascv1.SearchResponse{resp(many...)}}}}
	total, _, _, err := NewIndexer(api, resolver).Search(t.Context(), &internal.SearchOptions{RepoIDs: []int64{1}, Keyword: "f"})
	require.NoError(t, err)
	assert.EqualValues(t, maxFilesPerRepo, total)
}

func TestSearchPaginates(t *testing.T) {
	files := []*ascv1.FileHit{{Path: []byte("a")}, {Path: []byte("b")}, {Path: []byte("c")}}
	api := &fakeAPI{byRep: map[string]*fakeStream{"alice/one": {msgs: []*ascv1.SearchResponse{resp(files...)}}}}
	total, res, _, err := NewIndexer(api, resolver).Search(t.Context(), &internal.SearchOptions{
		RepoIDs: []int64{1}, Keyword: "f", Paginator: &db.ListOptions{Page: 2, PageSize: 2},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, res, 1)
	assert.Equal(t, "c", res[0].Filename)
}
