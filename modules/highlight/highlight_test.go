// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package highlight

import (
	"html/template"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lines(s string) (out []template.HTML) {
	// "" => [], "a" => ["a"], "a\n" => ["a\n"], "a\nb" => ["a\n", "b"] (each line always includes EOL "\n" if it exists)
	out = make([]template.HTML, 0)
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "\n", ""), `\n`, "\n")
	for {
		if p := strings.IndexByte(s, '\n'); p != -1 {
			out = append(out, template.HTML(s[:p+1]))
			s = s[p+1:]
		} else {
			break
		}
	}
	if s != "" {
		out = append(out, template.HTML(s))
	}
	return out
}

func TestFile(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		want      []template.HTML
		lexerName string
	}{
		{
			name:      "empty.py",
			code:      "",
			want:      lines(""),
			lexerName: "Python",
		},
		{
			name:      "empty.js",
			code:      "",
			want:      lines(""),
			lexerName: "JavaScript",
		},
		{
			name:      "empty.yaml",
			code:      "",
			want:      lines(""),
			lexerName: "YAML",
		},
		{
			name:      "empty.sh",
			code:      "",
			want:      lines(""),
			lexerName: "Shell",
		},
		{
			name:      "tags.txt",
			code:      "<>",
			want:      lines("&lt;&gt;"),
			lexerName: "Text",
		},
		{
			name:      "tags.py",
			code:      "<>",
			want:      lines(`<span class="o">&lt;</span><span class="o">&gt;</span>`),
			lexerName: "Python",
		},
		{
			name:      "eol-no.py",
			code:      "a=1",
			want:      lines(`<span class="n">a</span><span class="o">=</span><span class="mi">1</span>`),
			lexerName: "Python",
		},
		{
			name:      "eol-newline1.py",
			code:      "a=1\n",
			want:      lines(`<span class="n">a</span><span class="o">=</span><span class="mi">1</span>\n`),
			lexerName: "Python",
		},
		{
			name: "eol-newline2.py",
			code: "a=1\n\n",
			want: lines(`
<span class="n">a</span><span class="o">=</span><span class="mi">1</span>\n
\n
			`,
			),
			lexerName: "Python",
		},
		{
			name: "empty-line-with-space.py",
			code: strings.ReplaceAll(strings.TrimSpace(`
def:
    a=1

b=''
{space}
c=2
			`), "{space}", "    "),
			want: lines(`
<span class="n">def</span><span class="p">:</span>\n
    <span class="n">a</span><span class="o">=</span><span class="mi">1</span>\n
\n
<span class="n">b</span><span class="o">=</span><span class="sa"></span><span class="s1">&#39;</span><span class="s1">&#39;</span>\n
    \n
<span class="n">c</span><span class="o">=</span><span class="mi">2</span>`,
			),
			lexerName: "Python",
		},
		{
			name:      "DOS.PAS",
			code:      "",
			want:      lines(""),
			lexerName: "ObjectPascal",
		},
		{
			name:      "test.fs",
			code:      "module Crypt = let generateCryptTable: array<uint32> =",
			want:      lines(`<span class="k">module</span> <span class="nn">Crypt</span> <span class="o">=</span> <span class="k">let</span> <span class="nv">generateCryptTable</span><span class="o">:</span> <span class="n">array</span><span class="o">&lt;</span><span class="kt">uint32</span><span class="o">&gt;</span> <span class="o">=</span>`),
			lexerName: "FSharp",
		},
		{
			name:      "test.gradle.kts",
			code:      "@file:Suppress(\"UnstableApiUsage\")",
			want:      lines("<span class=\"nd\">@file</span><span class=\"p\">:</span><span class=\"n\">Suppress</span><span class=\"p\">(</span><span class=\"s2\">&#34;</span><span class=\"s2\">UnstableApiUsage</span><span class=\"s2\">&#34;</span><span class=\"p\">)</span>"), // codespell:ignore
			lexerName: "Kotlin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, lexerName, err := File(tt.name, "", []byte(tt.code))
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
			assert.Equal(t, tt.lexerName, lexerName)
		})
	}
}

func TestPlainText(t *testing.T) {
	tests := []struct {
		name string
		code string
		want []template.HTML
	}{
		{
			name: "empty.py",
			code: "",
			want: lines(""),
		},
		{
			name: "tags.py",
			code: "<>",
			want: lines("&lt;&gt;"),
		},
		{
			name: "eol-no.py",
			code: "a=1",
			want: lines(`a=1`),
		},
		{
			name: "eol-newline1.py",
			code: "a=1\n",
			want: lines(`a=1\n`),
		},
		{
			name: "eol-newline2.py",
			code: "a=1\n\n",
			want: lines(`
a=1\n
\n
			`),
		},
		{
			name: "empty-line-with-space.py",
			code: strings.ReplaceAll(strings.TrimSpace(`
def:
    a=1

b=''
{space}
c=2
			`), "{space}", "    "),
			want: lines(`
def:\n
    a=1\n
\n
b=&#39;&#39;\n
    \n
c=2`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := PlainText([]byte(tt.code))
			assert.Equal(t, tt.want, out)
		})
	}
}

func TestTSRX(t *testing.T) {
	src := "function A() @{\n  @if (ok) {\n    <p class=\"x\">{y}</p>\n  }\n}\n"

	// by file name, by the language the file view passes (the detected "TSRX") and by a Markdown fence "tsrx"
	for _, c := range []struct{ file, lang string }{{"a.tsrx", ""}, {"a", "TSRX"}, {"", "tsrx"}} {
		out, lexerName, err := File(c.file, c.lang, []byte(src))
		require.NoError(t, err, "%+v", c)
		assert.Equal(t, "TSRX", lexerName, "%+v", c)
		joined := ""
		for _, l := range out {
			joined += string(l)
		}
		assert.Contains(t, joined, `>@if</span>`, "%+v: the control flow keyword is a token of its own", c)
		assert.Contains(t, joined, `>p</span>`, "%+v: the tag name is a token of its own", c)
	}

	_, lexerName := Code("", "tsrx", src)
	assert.Equal(t, "TSRX", lexerName)
}

func TestFileBudgetedCachesAndSkips(t *testing.T) {
	src := []byte("package main\n\nfunc main() {}\n")
	first, err := FileBudgeted("main.go", "", src)
	require.NoError(t, err)
	assert.False(t, first.Cached)
	assert.False(t, first.Skipped)
	assert.Equal(t, "Go", first.Lexer)

	second, err := FileBudgeted("main.go", "", src)
	require.NoError(t, err)
	assert.True(t, second.Cached, "the same content and lexer is served from the cache")
	assert.Equal(t, first.Lines, second.Lines)

	other, err := FileBudgeted("main.go", "", append([]byte("// x\n"), src...))
	require.NoError(t, err)
	assert.False(t, other.Cached, "other content is another key")

	big := []byte(strings.Repeat("x := 1\n", highlightMaxBytes/7+10))
	res, err := FileBudgeted("big.go", "", big)
	require.NoError(t, err)
	assert.True(t, res.Skipped, "a file over the size limit is plain text")
	assert.Positive(t, res.Window, "the start of a large file is still highlighted")
	assert.NotEmpty(t, res.Lines)
}

func TestPersistentCacheRoundTrip(t *testing.T) {
	store := map[string]string{}
	oldGet, oldPut := persistentGet, persistentPut
	t.Cleanup(func() { persistentGet, persistentPut = oldGet, oldPut })
	persistentGet = func(k string) (string, bool) { v, ok := store[k]; return v, ok }
	persistentPut = func(k, v string) { store[k] = v }

	src := []byte(strings.Repeat("let a = 1;\n", persistentMinBytes/10))
	first, err := FileBudgeted("a.ts", "", src)
	require.NoError(t, err)
	require.Len(t, store, 1, "a file over the minimum is written to the persistent cache")

	resultCache = newByteLRU(memoryCacheBytes) // a fresh process: only the persistent tier knows the file
	second, err := FileBudgeted("a.ts", "", src)
	require.NoError(t, err)
	assert.True(t, second.Cached)
	assert.Equal(t, first.Lines, second.Lines)
	assert.Equal(t, first.Lexer, second.Lexer)
}

func TestByteLRUEvictsByBytes(t *testing.T) {
	c := newByteLRU(1000)
	line := template.HTML(strings.Repeat("a", 100))
	for i := 0; i < 20; i++ {
		c.put(strings.Repeat("k", i+1), []template.HTML{line, line}, "Go")
	}
	assert.LessOrEqual(t, c.size, 1000)
	_, newest := c.get(strings.Repeat("k", 20))
	_, oldest := c.get("k")
	assert.True(t, newest)
	assert.False(t, oldest)
	c.put("huge", []template.HTML{template.HTML(strings.Repeat("b", 500))}, "Go")
	_, ok := c.get("huge")
	assert.False(t, ok, "one file never takes a quarter of the cache")
}

func TestBudgetedHighlightTimeIsBounded(t *testing.T) {
	// 200 KB of Go at the largest size allowed: it is highlighted or skipped, in either case within the budget
	// plus the time to format.
	src := []byte(strings.Repeat("func f(a int) int { return a + 1 } // x\n", 200<<10/41))
	start := time.Now()
	res, err := FileBudgeted("f.go", "", src)
	require.NoError(t, err)
	assert.Less(t, time.Since(start), highlightBudget+2*time.Second)
	assert.NotEmpty(t, res.Lines)
}

// The cache turns a second view of a file into a lookup: this measures both and keeps the gap from closing.
func TestCachedViewIsMuchFasterThanTheFirst(t *testing.T) {
	src := []byte(strings.Repeat("func f(a int) int { return a + 1 } // x\n", 100<<10/41))
	resultCache = newByteLRU(memoryCacheBytes)

	start := time.Now()
	first, err := FileBudgeted("f.go", "", src)
	require.NoError(t, err)
	cold := time.Since(start)
	require.False(t, first.Cached)

	var warm time.Duration
	for i := 0; i < 20; i++ {
		start = time.Now()
		again, err := FileBudgeted("f.go", "", src)
		require.NoError(t, err)
		require.True(t, again.Cached)
		if d := time.Since(start); i == 0 || d < warm {
			warm = d
		}
	}
	t.Logf("100 KB of Go: first view %s, cached view %s", cold.Round(time.Microsecond), warm.Round(time.Microsecond))
	assert.Less(t, warm*10, cold, "a cached view must cost a tenth of the first one")
}

func TestLargeFileHighlightsItsStart(t *testing.T) {
	line := "func f(a int) int { return a + 1 }\n"
	big := []byte(strings.Repeat(line, highlightMaxBytes/len(line)+500)) // a little over the limit
	res, err := FileBudgeted("big.go", "", big)
	require.NoError(t, err)
	assert.True(t, res.Skipped, "the file is partly plain text")
	assert.Positive(t, res.Window, "its start is highlighted")
	assert.Equal(t, "Go", res.Lexer)
	assert.Equal(t, strings.Count(string(big), "\n"), len(res.Lines), "every line is there")
	assert.Contains(t, string(res.Lines[0]), "<span", "a line of the window has tokens")
	assert.NotContains(t, string(res.Lines[len(res.Lines)-1]), "<span", "a line after the window is plain")

	huge := []byte(strings.Repeat(line, highlightWindowMaxBytes/len(line)+10))
	res, err = FileBudgeted("huge.go", "", huge)
	require.NoError(t, err)
	assert.Zero(t, res.Window, "a file over the window limit is plain text")
	assert.True(t, res.Skipped)
}

func TestManyLinesGetAWindow(t *testing.T) {
	line := "let a = 1;\n"
	src := []byte(strings.Repeat(line, highlightFullLines+100)) // small in bytes, long in lines
	res, err := FileBudgeted("many.rs", "", src)
	require.NoError(t, err)
	assert.True(t, res.Skipped)
	assert.Equal(t, highlightWindowLines, res.Window, "the first lines are highlighted")
	assert.Equal(t, highlightFullLines+100, len(res.Lines))

	fit := []byte(strings.Repeat(line, highlightFullLines))
	res, err = FileBudgeted("fits.rs", "", fit)
	require.NoError(t, err)
	assert.False(t, res.Skipped, "a file of up to highlightFullLines lines is highlighted whole")
}

func TestWindowEnd(t *testing.T) {
	assert.Equal(t, -1, windowEnd([]byte("no newline")))
	assert.Equal(t, 1, windowEnd([]byte("a\nb")))
	big := []byte(strings.Repeat("x\n", highlightWindowLines+50))
	assert.Equal(t, highlightWindowLines*2-1, windowEnd(big), "the newline that ends line highlightWindowLines")
}
