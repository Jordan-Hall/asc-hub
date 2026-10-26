// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func sym(name, path string, line uint32, def bool, conf string) *ascv1.Symbol {
	return &ascv1.Symbol{Name: name, Kind: "fn", IsDef: def, Path: []byte(path), Line: line, EndLine: line + 2, Confidence: conf}
}

func symNames(r *SymbolResult) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.Repo+":"+h.Symbol.Name)
	}
	return out
}

func TestSymbolsCross(t *testing.T) {
	f := &fakeAsc{lookup: func(req *ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
		return &ascv1.LookupSymbolResponse{
			Hits: []*ascv1.SymbolHit{
				{Repo: ref("o/r2"), Rev: "main", Symbol: sym("Retry", "a.go", 3, true, "precise"), Score: 0.5},
				{Repo: ref("o/r1"), Symbol: sym("Retry", "b.go", 9, true, "name_match"), Score: 0.9},
				{Repo: ref("hidden/repo"), Symbol: sym("Retry", "c.go", 1, true, "precise"), Score: 1},
				{Repo: ref("o/r1"), Symbol: sym("Retry", "../bad", 1, true, "precise"), Score: 1},
				{Repo: ref("o/r1"), Symbol: sym("Retry", "use.go", 4, false, "import_match"), Score: 0.3},
			},
			Truncated: true, NextPageToken: "n1",
		}, nil
	}}
	svc := startFake(t, f)

	res, err := svc.Symbols(sys(t), reposN(2), SymbolQuery{
		Name: "Retry", Match: ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX, Kinds: []string{"fn"}, Langs: []string{"Go"}, DefinitionsOnly: true, Limit: 10,
	})
	require.NoError(t, err)
	req := f.look[0]
	assert.Equal(t, []string{"o/r1", "o/r2"}, req.GetTarget().GetRepos())
	assert.Equal(t, "Retry", req.GetName())
	assert.Equal(t, ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX, req.GetMatch())
	assert.True(t, req.GetDefinitionsOnly())
	assert.EqualValues(t, 10, req.GetLimit())
	assert.Equal(t, []string{"o/r1:Retry", "o/r2:Retry"}, symNames(res), "best score first; a foreign repository, a bad path and a non-definition are dropped")
	assert.Equal(t, "b.go", res.Hits[0].Symbol.Path)
	assert.True(t, res.Truncated)
	assert.Equal(t, "n1", res.NextPageToken)
	assert.False(t, res.Fallback)
}

func TestSymbolsChecksAndErrors(t *testing.T) {
	f := &fakeAsc{lookup: func(*ascv1.LookupSymbolRequest) (*ascv1.LookupSymbolResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "no")
	}}
	svc := startFake(t, f)
	for name, q := range map[string]SymbolQuery{
		"empty":   {},
		"long":    {Name: strings.Repeat("a", MaxName+1)},
		"control": {Name: "a\x00"},
		"kinds":   {Name: "a", Kinds: make([]string, maxKinds+1)},
		"kind":    {Name: "a", Kinds: []string{""}},
		"lang":    {Name: "a", Langs: []string{"a;b"}},
		"token":   {Name: "a", PageToken: "a b"},
	} {
		_, err := svc.Symbols(sys(t), reposN(1), q)
		require.ErrorIs(t, err, ErrInput, name)
	}
	assert.Empty(t, f.look)

	res, err := svc.Symbols(sys(t), reposN(1), SymbolQuery{Name: "a"})
	require.NoError(t, err)
	assert.Empty(t, res.Hits, "a refusal adds nothing")

	res, err = svc.Symbols(sys(t), nil, SymbolQuery{Name: "a"})
	require.NoError(t, err)
	assert.Empty(t, res.Hits)

	_, err = svc.Symbols(sys(t), reposN(130), SymbolQuery{Name: "a", PageToken: "p"})
	require.ErrorIs(t, err, ErrInput)
	_, err = svc.Symbols(t.Context(), reposN(1), SymbolQuery{Name: "a"})
	assert.Equal(t, KindNoCredential, KindOf(err))
}

func TestSymbolsFallback(t *testing.T) {
	f := &fakeAsc{symbols: func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		switch req.GetScope().GetRepo().GetName() {
		case "o/r1":
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
				sym("RetryPolicy", "a.go", 1, true, "precise"),
				sym("retry", "a.go", 20, true, "precise"),
				sym("Retry", "b.go", 5, true, "name_match"),
				sym("subretry", "c.go", 5, false, "name_match"),
				sym("Other", "c.go", 9, true, "name_match"),
			}}, nil
		case "o/r2":
			return nil, status.Error(codes.NotFound, "x")
		}
		return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{sym("Retry", "z.go", 2, true, "precise")}}, nil
	}}
	svc := startFake(t, f)

	res, err := svc.Symbols(sys(t), reposN(3), SymbolQuery{Name: "Retry", Match: ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, Kinds: []string{"fn"}})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, []string{"o/r1:Retry", "o/r3:Retry", "o/r1:retry", "o/r1:RetryPolicy", "o/r1:subretry", "o/r1:Other"}, symNames(res),
		"exact, then case-insensitive, then prefix, then substring, then the rest; ties by repository order")
	for _, r := range f.syms {
		assert.Equal(t, ascv1.SymbolOp_SYMBOL_OP_FIND, r.GetOp())
		assert.Equal(t, []string{"fn"}, r.GetKinds())
	}

	res, err = svc.Symbols(sys(t), reposN(3), SymbolQuery{Name: "Retry", Match: ascv1.SymbolMatch_SYMBOL_MATCH_EXACT})
	require.NoError(t, err)
	assert.Equal(t, []string{"o/r1:Retry", "o/r3:Retry"}, symNames(res))

	res, err = svc.Symbols(sys(t), reposN(3), SymbolQuery{Name: "Retry", Match: ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX, DefinitionsOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"o/r1:Retry", "o/r3:Retry", "o/r1:retry", "o/r1:RetryPolicy"}, symNames(res))

	res, err = svc.Symbols(sys(t), reposN(3), SymbolQuery{Name: "Retry", Match: ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, Limit: 2})
	require.NoError(t, err)
	assert.Len(t, res.Hits, 2)
	assert.True(t, res.Truncated)

	_, err = svc.Symbols(sys(t), reposN(3), SymbolQuery{Name: "Retry", PageToken: "p"})
	require.ErrorIs(t, err, ErrInput)

	f.symbols = func(*ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		return nil, status.Error(codes.Internal, "x")
	}
	_, err = svc.Symbols(sys(t), reposN(2), SymbolQuery{Name: "Retry"})
	assert.Equal(t, KindFailed, KindOf(err))
}

func TestMatchScore(t *testing.T) {
	for _, c := range []struct {
		have, want string
		m          ascv1.SymbolMatch
		score      float32
		keep       bool
	}{
		{"Foo", "Foo", ascv1.SymbolMatch_SYMBOL_MATCH_EXACT, 1, true},
		{"foo", "Foo", ascv1.SymbolMatch_SYMBOL_MATCH_EXACT, 0, false},
		{"foo", "Foo", ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, 0.9, true},
		{"FooBar", "foo", ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX, 0.7, true},
		{"BarFoo", "foo", ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX, 0, false},
		{"BarFoo", "foo", ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, 0.4, true},
		{"xyz", "foo", ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, 0.2, true},
	} {
		s, keep := matchScore(c.have, c.want, c.m)
		assert.Equal(t, c.keep, keep, "%s/%s", c.have, c.want)
		if keep {
			assert.Equal(t, c.score, s, "%s/%s", c.have, c.want)
		}
	}
}
