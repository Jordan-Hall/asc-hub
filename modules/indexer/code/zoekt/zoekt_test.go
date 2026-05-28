// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package zoekt

import (
	"testing"

	"forgejo.org/modules/indexer/code/internal"

	"github.com/sourcegraph/zoekt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertZoektResult_BasicMatch(t *testing.T) {
	content := "hello world\nsecond line\n"

	files := []zoekt.FileMatch{
		{
			RepositoryID: 1,
			FileName:     "main.go",
			Version:      "commit123",
			Language:     "Go",
			Content:      []byte(content),
			LineMatches: []zoekt.LineMatch{
				{
					LineNumber: 1,
					LineFragments: []zoekt.LineFragmentMatch{
						{
							Offset:      6, // "world"
							MatchLength: 5,
						},
					},
				},
			},
		},
	}

	results := convertZoektResult(files)
	require.Len(t, results, 1)

	res := results[0]

	assert.Equal(t, int64(1), res.RepoID)
	assert.Equal(t, "main.go", res.Filename)
	assert.Equal(t, "commit123", res.CommitID)
	assert.Equal(t, "Go", res.Language)
	assert.Equal(t, content, res.Content)

	require.Len(t, res.Matches, 1)
	assert.Equal(t, 6, res.Matches[0].Start)
	assert.Equal(t, 11, res.Matches[0].End)
	assert.Equal(t, 1, res.Matches[0].LineNumber)

	assert.Equal(t, []int{1, 2, 3}, res.LineNumbers)
	assert.Equal(t, []int{0, 12, 24}, res.LineOffsets)
}

func TestConvertZoektResult_PlainTextFallback(t *testing.T) {
	files := []zoekt.FileMatch{
		{
			RepositoryID: 2,
			FileName:     "README",
			Version:      "commit456",
			Content:      []byte("just text"),
			LineMatches: []zoekt.LineMatch{
				{
					LineNumber: 1,
					LineFragments: []zoekt.LineFragmentMatch{
						{Offset: 0, MatchLength: 4},
					},
				},
			},
		},
	}

	results := convertZoektResult(files)
	require.Len(t, results, 1)

	assert.Equal(t, "Plain Text", results[0].Language)
}

func TestGetSearchResultLanguages(t *testing.T) {
	searchResult := &zoekt.SearchResult{
		Files: []zoekt.FileMatch{
			{Language: "Go"},
			{Language: "Go"},
			{Language: "Rust"},
			{Language: ""},
		},
	}

	stats := getSearchResultLanguages(searchResult)

	require.Len(t, stats, 3)

	assert.Equal(t, "Go", stats[0].Language)
	assert.Equal(t, 2, stats[0].Count)

	assert.Equal(t, "Plain Text", stats[1].Language)
	assert.Equal(t, 1, stats[1].Count)

	assert.Equal(t, "Rust", stats[2].Language)
	assert.Equal(t, 1, stats[2].Count)
}

func TestGenerateZoektQuery_Union(t *testing.T) {
	indexer := &Indexer{}

	opts := &internal.SearchOptions{
		Keyword: "foo bar",
		Mode:    internal.CodeSearchModeUnion,
		RepoIDs: []int64{1, 2},
	}

	q, err := indexer.generateZoektQuery(opts)
	require.NoError(t, err)
	require.NotNil(t, q)
}

func TestGenerateZoektQuery_SpecialCharacters(t *testing.T) {
	indexer := &Indexer{}

	opts := &internal.SearchOptions{
		Keyword: `foo.bar+baz[qux]`,
		Mode:    internal.CodeSearchModeExact,
	}

	q, err := indexer.generateZoektQuery(opts)
	require.NoError(t, err)
	require.NotNil(t, q)
}

func TestConvertZoektResult_IgnoresFilenameMatches(t *testing.T) {
	files := []zoekt.FileMatch{
		{
			RepositoryID: 1,
			FileName:     "foo.go",
			Version:      "commit123",
			Content:      []byte("package main\n"),
			LineMatches: []zoekt.LineMatch{
				{
					FileName: true, // filename-only match
				},
			},
		},
	}

	results := convertZoektResult(files)
	assert.Empty(t, results)
}
