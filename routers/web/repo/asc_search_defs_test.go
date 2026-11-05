// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	gocontext "context"
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscSearchDefWords(t *testing.T) {
	lex := ascSearchQuery{mode: ascSearchModeLexical, match: ascSearchMatchLiteral}
	words := func(q ascSearchQuery, text string) []string {
		q.text = text
		return ascSearchDefWords(q)
	}
	assert.Equal(t, []string{"RetryPolicy", "retry"}, words(lex, "RetryPolicy retry RetryPolicy"))
	assert.Equal(t, []string{"ok"}, words(lex, "ok a.b <x> 9lives"), "only identifiers")
	assert.Nil(t, words(lex, "one two three four five"), "a sentence is not names")
	assert.Nil(t, words(ascSearchQuery{mode: ascSearchModeLexical, match: ascSearchMatchRegex}, "retry"))
	assert.Nil(t, words(ascSearchQuery{mode: ascSearchModeSemantic}, "retry"))
	assert.Nil(t, words(lex, strings.Repeat("a", 129)))
}

type fakeLastCommits struct {
	ascv1.UnimplementedRepoServer
	asked []*ascv1.LastCommitsRequest
}

func (f *fakeLastCommits) LastCommits(_ gocontext.Context, req *ascv1.LastCommitsRequest) (*ascv1.LastCommitsResponse, error) {
	f.asked = append(f.asked, req)
	out := map[string]*ascv1.LogEntry{}
	for _, n := range req.GetNames() {
		out[string(n)] = &ascv1.LogEntry{Git: bytes.Repeat([]byte{0xab}, 20), Committer: &ascv1.Signature{Time: time.Now().Add(-3 * time.Hour).Unix()}}
	}
	return &ascv1.LastCommitsResponse{ByName: out}, nil
}

func TestAscSearchSymbolsMatched(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	policy := ascSym("delay", "README.md", 3, true, "precise")
	policy.Container = "RetryPolicy"
	last := &fakeLastCommits{}
	fake := startFakeSearch(t, &fakeSearchServer{
		repo: last,
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{fileHit("README.md", "aa", "delay x"), fileHit("src/other.rs", "bb", "delay y")}}}, nil
		},
		symbols: func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			switch {
			case req.GetName() != "delay":
				return &ascv1.SymbolsResponse{}, nil
			case req.GetOp() == ascv1.SymbolOp_SYMBOL_OP_DEF:
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{policy, ascSym("delayed", "src/x.rs", 1, true, "precise")}}, nil
			default:
				return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
					ascSym("delay", "README.md", 9, false, "precise"), // in the declaring file: not elsewhere
					ascSym("delay", "src/other.rs", 2, false, "precise"),
					ascSym("delay", "src/third.rs", 5, false, "precise"),
				}}, nil
			}
		},
	})

	page := getAscSearch(t, "?q=delay+missing&ref=master")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="search-symbols-matched"`)
	assert.Contains(t, page.body, `asc_rp.s_symbols_matched <span class="tw-font-mono">delay</span></span>`, "only the word that names a declaration")
	assert.Contains(t, page.body, `>RetryPolicy::delay</a>`)
	assert.Contains(t, page.body, `href="/user2/repo1/asc/symbols?name=delay&amp;op=def&amp;ref=master"`)
	assert.Equal(t, 1, strings.Count(page.body, `class="asc-sym-chips"`), "only the declaring file has chips")
	// One DEF and one REFS call for the word that matched, one DEF for the other, and the FIND of the Symbol
	// search teaser, all at the searched version.
	var ops []string
	for _, f := range fake.finds {
		ops = append(ops, f.GetName()+" "+f.GetOp().String())
		assert.Equal(t, "refs/heads/master", f.GetScope().GetRev())
	}
	assert.ElementsMatch(t, []string{"delay SYMBOL_OP_DEF", "delay SYMBOL_OP_REFS", "missing SYMBOL_OP_DEF", "delay missing SYMBOL_OP_FIND"}, ops)
	// The last change of the file comes from the history at master.
	require.Len(t, last.asked, 1)
	assert.Empty(t, last.asked[0].GetDir())
	assert.Equal(t, [][]byte{[]byte("README.md")}, last.asked[0].GetNames())
	assert.Regexp(t, `^[0-9a-f]{40}$`, last.asked[0].GetRev(), "the branch is resolved to its commit")
	assert.Contains(t, page.body, `href="/user2/repo1/commit/`+strings.Repeat("ab", 20)+`"`)
	assert.Contains(t, page.body, "asc_rp.s_changed")

	t.Run("the symbol calls failing leave the results alone", func(t *testing.T) {
		fake.symbols = nil
		page := getAscSearch(t, "?q=delay")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.NotContains(t, page.body, "search-symbols-matched")
		assert.NotContains(t, page.body, "asc-sym-chips")
		assert.Contains(t, page.body, "README.md")
	})

	t.Run("an all-branches search has no chips", func(t *testing.T) {
		fake.mu.Lock()
		fake.finds = nil
		fake.mu.Unlock()
		page := getAscSearch(t, "?q=delay&scope=branches")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.Empty(t, fake.finds)
	})
}
