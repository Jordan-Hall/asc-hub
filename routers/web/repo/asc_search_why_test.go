// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscSearchWhyIsDrawn(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{
				{
					Path:    []byte("src/lib.rs"),
					Matches: []*ascv1.LineMatch{{Line: 4, Cols: []uint32{1}, Text: []byte("needle")}},
					Reason: &ascv1.MatchReason{
						Kind: ascv1.MatchKind_MATCH_KIND_TEXT, Score: 0.7, Terms: []string{"<needle>"},
						Spans: []*ascv1.MatchSpan{{Line: 4, StartCol: 1, EndCol: 7, Term: "needle"}},
					},
				},
				{Path: []byte("src/old.rs"), Matches: []*ascv1.LineMatch{{Line: 1, Cols: []uint32{1}, Text: []byte("needle")}}},
			}}}, nil
		},
	})
	page := getAscSearch(t, "?q=needle")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="search-why" data-kind="text"`)
	assert.Contains(t, page.body, "asc_rp.s_why_text <code>&lt;needle&gt;</code>", "the term is escaped")
	assert.Contains(t, page.body, "asc_rp.s_why_lines_1")
	assert.Equal(t, 1, strings.Count(page.body, `data-testid="search-why"`), "a hit without a reason draws no line")
}

func TestAscSearchWhyOfChunks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	startFakeSearch(t, &fakeSearchServer{
		semantic: func(*ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			return &ascv1.SemanticResponse{Chunks: []*ascv1.Chunk{
				{
					Path: []byte("src/auth.rs"), StartLine: 1, EndLine: 3, Score: 0.9, Text: []byte("fn check() {}"),
					Reason: &ascv1.MatchReason{Kind: ascv1.MatchKind_MATCH_KIND_MEANING, Terms: []string{"auth"}, Spans: []*ascv1.MatchSpan{{Line: 1}}},
				},
				{
					Path: []byte("config/.env"), StartLine: 1, EndLine: 1, Score: 0.5, Redacted: true,
					Reason: &ascv1.MatchReason{Kind: ascv1.MatchKind_MATCH_KIND_MEANING},
				},
			}}, nil
		},
	})
	page := getAscSearch(t, "?mode=semantic&q=where+is+auth")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, "asc_rp.s_why_meaning_terms <code>auth</code>")
	assert.Contains(t, page.body, "asc_rp.s_why_meaning</p>", "a hidden chunk says only that it is close in meaning")
	assert.NotContains(t, page.body, "asc_rp.s_why_lines", "a meaning line does not count lines")
}
