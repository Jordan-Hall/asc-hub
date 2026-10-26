// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestClean(t *testing.T) {
	assert.Equal(t, "a\tb[31m", Clean([]byte("a\tb\x1b[31m‮⁦\x00"), 0))
	assert.Equal(t, "�x", Clean([]byte("\xffx"), 0))
	assert.Equal(t, "abc", Clean([]byte("abcdef"), 3))
	assert.Equal(t, "line one\nline two", Block([]byte("line one\r\nline two\n\n  ")))
	assert.Equal(t, "ab", Block([]byte("a\x1bb")))
	long := strings.Repeat("x\n", 100)
	assert.Equal(t, maxChunkLines, strings.Count(Block([]byte(long)), "\n")+1)
	assert.Equal(t, "one two three", Doc([]byte("one\n  two\t three")))
	assert.True(t, strings.HasSuffix(Doc([]byte(strings.Repeat("w ", 1000))), "..."))
	assert.Equal(t, "0a0b", Hex([]byte{10, 11}))
	assert.Empty(t, Hex(make([]byte, 65)))
}

func TestChecks(t *testing.T) {
	assert.True(t, Plain("hello wörld", 100))
	assert.False(t, Plain("a\nb", 100))
	assert.False(t, Plain("a‮b", 100))
	assert.False(t, Plain("\xff", 100))
	assert.False(t, Plain("abcd", 3))

	for _, ok := range []string{"", "a", "a/b.go", "/a/b", "dir/sub dir/file"} {
		_, good := CheckPath(ok)
		assert.True(t, good, ok)
	}
	for _, bad := range []string{"a//b", "../x", "a/../b", "a/./b", "a\\b", "a\x00b", "a/‮", strings.Repeat("a", MaxPath+1)} {
		_, good := CheckPath(bad)
		assert.False(t, good, bad)
	}
	p, ok := CheckPath("/src/lib.rs")
	assert.True(t, ok)
	assert.Equal(t, "src/lib.rs", p)
	_, ok = Path([]byte(""))
	assert.False(t, ok, "asc never names the empty path")

	assert.True(t, CheckLang("C++"))
	assert.True(t, CheckLang("Objective-C"))
	assert.False(t, CheckLang(""))
	assert.False(t, CheckLang("Go;rm"))
	assert.False(t, CheckLang(strings.Repeat("a", MaxLang+1)))

	assert.True(t, CheckToken(""))
	assert.True(t, CheckToken("abc123-_="))
	assert.False(t, CheckToken("a b"))
	assert.False(t, CheckToken("a\x7f"))
	assert.False(t, CheckToken(strings.Repeat("a", maxTokenBytes+1)))
	assert.Empty(t, OutToken("bad token"))
	assert.Equal(t, "ok", OutToken("ok"))
}

func TestFileHitOf(t *testing.T) {
	const key = "AKIAIOSFODNN7EXAMPLE"
	h, ok := FileHitOf(&ascv1.FileHit{
		Path: []byte("a/b.go"), Lang: "Go\x1b", Redacted: true,
		AlsoAt: [][]byte{[]byte("c.go"), []byte("../bad")},
		Matches: []*ascv1.LineMatch{{
			Line: 7, Cols: []uint32{3}, Text: []byte("key = " + key), Before: [][]byte{[]byte(key)}, After: [][]byte{[]byte(key)},
		}},
	})
	assert.True(t, ok)
	assert.Equal(t, []string{"c.go"}, h.AlsoAt, "a bad also-at path is dropped")
	assert.Equal(t, "Go", h.Lang)
	if assert.Len(t, h.Matches, 1) {
		m := h.Matches[0]
		assert.EqualValues(t, 7, m.Line, "a redacted hit keeps its line numbers")
		assert.Empty(t, m.Text)
		assert.Empty(t, m.Cols)
		assert.Empty(t, m.Before)
		assert.Empty(t, m.After)
	}

	h, ok = FileHitOf(&ascv1.FileHit{Path: []byte("x.go"), Matches: []*ascv1.LineMatch{{
		Line: 1, Text: []byte("t‮"), Before: [][]byte{[]byte("b1"), []byte("b2"), []byte("b3"), []byte("b4"), []byte("b5"), []byte("b6")},
	}}})
	assert.True(t, ok)
	assert.Equal(t, "t", h.Matches[0].Text)
	assert.Len(t, h.Matches[0].Before, MaxContext)

	for _, p := range []string{"", "../x", "a\x00"} {
		_, ok = FileHitOf(&ascv1.FileHit{Path: []byte(p)})
		assert.False(t, ok, p)
	}
	many := &ascv1.FileHit{Path: []byte("m.go")}
	for range 80 {
		many.Matches = append(many.Matches, &ascv1.LineMatch{Line: 1})
	}
	h, _ = FileHitOf(many)
	assert.Len(t, h.Matches, maxMatchesPerFile)
}

func TestSymbolAndChunkOf(t *testing.T) {
	s, ok := SymbolOf(&ascv1.Symbol{Name: "Foo\x1b", Kind: "fn", Path: []byte("a.go"), Line: 3, EndLine: 9, Confidence: "precise", Doc: []byte("x\ny")})
	assert.True(t, ok)
	assert.Equal(t, "Foo", s.Name)
	assert.Equal(t, "x y", s.Doc)
	for _, bad := range []*ascv1.Symbol{
		{Name: "x", Path: []byte("../a")},
		{Name: "x", Path: []byte("a"), Line: 9, EndLine: 3},
		{Name: "x", Path: []byte("a"), Line: 1 << 30},
		{Name: "x", Path: []byte("a"), Confidence: "guess"},
	} {
		_, ok = SymbolOf(bad)
		assert.False(t, ok)
	}

	c, ok := ChunkOf("o/r", &ascv1.Chunk{Path: []byte("a.go"), StartLine: 2, EndLine: 5, Score: 0.5, Text: []byte("secret AKIAIOSFODNN7EXAMPLE"), Redacted: true})
	assert.True(t, ok)
	assert.Empty(t, c.Text, "a redacted chunk has no text")
	assert.True(t, c.Redacted)
	c, ok = ChunkOf("o/r", &ascv1.Chunk{Path: []byte("a.go"), Text: []byte("a\x1b\nb"), Sources: make([]string, 20)})
	assert.True(t, ok)
	assert.Equal(t, "a\nb", c.Text)
	assert.Len(t, c.Sources, maxSources)
	_, ok = ChunkOf("o/r", &ascv1.Chunk{Path: []byte("a.go"), StartLine: 5, EndLine: 2})
	assert.False(t, ok)

	assert.Zero(t, ScopeOf(&ascv1.ResolvedScope{Coverage: 7}).Coverage)
	assert.Nil(t, ScopeOf(nil))
}

func TestFacets(t *testing.T) {
	langs, dirs := Facets([]RepoResult{
		{Files: []FileHit{{Path: "src/a.rs", Lang: "Rust"}, {Path: "src/b.rs", Lang: "Rust"}, {Path: "README.md", Lang: "Markdown"}}},
		{Files: []FileHit{{Path: "docs/x.md", Lang: "Markdown"}, {Path: "src/c.ts", Lang: "TypeScript"}, {Path: "n.txt"}}},
	})
	assert.Equal(t, []Facet{{"Markdown", 2}, {"Rust", 2}, {"TypeScript", 1}}, langs)
	assert.Equal(t, []Facet{{"src/", 3}, {"(root)", 2}, {"docs/", 1}}, dirs)
	l, d := Facets(nil)
	assert.Empty(t, l)
	assert.Empty(t, d)
}

func TestRRF(t *testing.T) {
	got := RRF([][]string{{"a1", "a2", "a3"}, {"b1", "b2"}})
	assert.Equal(t, []string{"a1", "b1", "a2", "b2", "a3"}, got)
	assert.Equal(t, got, RRF([][]string{{"a1", "a2", "a3"}, {"b1", "b2"}}), "deterministic")
	assert.Empty(t, RRF[string](nil))
	assert.Equal(t, []string{"x"}, RRF([][]string{nil, {"x"}}))
}
