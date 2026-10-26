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

func chunk(path string, score float64, text string) *ascv1.Chunk {
	return &ascv1.Chunk{Path: []byte(path), StartLine: 1, EndLine: 4, Score: score, Text: []byte(text), Sources: []string{"vector"}}
}

func TestMeaning(t *testing.T) {
	const key = "AKIAIOSFODNN7EXAMPLE"
	f := &fakeAsc{semantic: func(req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
		switch req.GetScope().GetRepo().GetName() {
		case "o/r1":
			return &ascv1.SemanticResponse{Model: "m1\x1b", EstTokens: 4, Chunks: []*ascv1.Chunk{
				chunk("a.go", 0.9, "first\x1b"),
				chunk("b.go", 0.8, "second"),
				{Path: []byte("s.env"), Redacted: true, Text: []byte("K=" + key), Score: 0.7},
			}}, nil
		case "o/r2":
			return &ascv1.SemanticResponse{Chunks: []*ascv1.Chunk{chunk("c.go", 0.95, "third"), chunk("../bad", 0.9, "x")}, AnnExhausted: true}, nil
		case "o/r3":
			return nil, status.Error(codes.PermissionDenied, "no")
		}
		return nil, status.Error(codes.Internal, "boom")
	}}
	svc := startFake(t, f)

	res, err := svc.Meaning(sys(t), reposN(4), MeaningQuery{Query: "where do we stop retrying a refund?", K: 4, Rerank: true, Paths: []string{"src"}, Langs: []string{"Go"}})
	require.NoError(t, err)
	require.Len(t, f.sems, 4)
	for _, r := range f.sems {
		assert.EqualValues(t, 4, r.GetK())
		assert.True(t, r.GetRerank())
		assert.Equal(t, "refs/heads/main", r.GetScope().GetRev())
		assert.Equal(t, []string{"src"}, r.GetScope().GetPaths())
		assert.Equal(t, []string{"Go"}, r.GetScope().GetLangs())
	}
	assert.Equal(t, 2, res.Searched, "o/r3 was refused (no trace), o/r4 failed")
	assert.Equal(t, 1, res.Failed)
	assert.True(t, res.AnnExhausted)
	assert.EqualValues(t, 4, res.EstTokens)
	assert.Equal(t, "m1", res.Model)

	// Fusion by rank: each repository's best first, then the seconds; the bad path is dropped; k cuts the list.
	var got []string
	for _, c := range res.Chunks {
		got = append(got, c.Repo+":"+c.Path)
	}
	assert.Equal(t, []string{"o/r1:a.go", "o/r2:c.go", "o/r1:b.go", "o/r1:s.env"}, got)
	assert.Equal(t, "first", res.Chunks[0].Text, "control characters are gone")
	last := res.Chunks[3]
	assert.True(t, last.Redacted)
	assert.Empty(t, last.Text, "a redacted chunk has no text")
	assert.NotContains(t, strings.Join(got, ","), "bad")

	res, err = svc.Meaning(sys(t), reposN(1), MeaningQuery{Query: "x", K: 1})
	require.NoError(t, err)
	assert.Len(t, res.Chunks, 1)
}

func TestMeaningChecks(t *testing.T) {
	f := &fakeAsc{semantic: func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
		return nil, status.Error(codes.Internal, "boom")
	}}
	svc := startFake(t, f)
	for name, q := range map[string]MeaningQuery{
		"empty":   {},
		"long":    {Query: strings.Repeat("a", MaxSemantic+1)},
		"control": {Query: "a\x00"},
		"paths":   {Query: "a", Paths: make([]string, MaxPaths+1)},
		"path":    {Query: "a", Paths: []string{"../a"}},
		"lang":    {Query: "a", Langs: []string{"a;b"}},
	} {
		_, err := svc.Meaning(sys(t), reposN(1), q)
		require.ErrorIs(t, err, ErrInput, name)
	}
	_, err := svc.Meaning(sys(t), reposN(MaxSemanticRepos+1), MeaningQuery{Query: "a"})
	require.ErrorIs(t, err, ErrInput, "each repository costs an embedding")
	assert.Empty(t, f.sems)

	res, err := svc.Meaning(sys(t), nil, MeaningQuery{Query: "a"})
	require.NoError(t, err)
	assert.Empty(t, res.Chunks)

	_, err = svc.Meaning(sys(t), reposN(2), MeaningQuery{Query: "a"})
	assert.Equal(t, KindFailed, KindOf(err), "all failing is an error")
	assert.Equal(t, 10, (&MeaningQuery{}).k())
	assert.Equal(t, maxK, (&MeaningQuery{K: 999}).k())
}
