// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package tsrx

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The regression budget of syntax highlighting: tokenizing must take time linear in the size of the file. These
// tests time the lexers of the languages the hub shows most (Go, TypeScript, Markdown) next to TSRX at three sizes,
// report p50 and p95 per size in the test log (go test -v), and fail when the time per byte grows with the size
// (a quadratic scan) or one run is slower than a generous absolute budget.

var samples = map[string]string{
	"go": "package main\n\nimport \"fmt\"\n\n// Greet says hello.\nfunc Greet(name string) string {\n\tif name == \"\" {\n\t\treturn \"hello\"\n\t}\n\treturn fmt.Sprintf(\"hello, %s\", name)\n}\n\n",
	"typescript": "export function total(items: Item[]): number {\n  // sum the prices\n  const sum = items.reduce((s, i) => s + i.price, 0);\n  return sum > 100 ? sum * 0.9 : sum;\n}\n\n",
	"markdown": "# Title\n\nSome *emphasis*, a [link](https://example.org) and `code`.\n\n- one\n- two\n\n```go\nfmt.Println(1)\n```\n\n",
	"tsrx": "function Cart({ items }: { items: Item[] }) @{\n  const subtotal = items.reduce((s, i) => s + i.price, 0);\n  @for (const item of items) {\n    @if (item.price > 10) {\n      <li class=\"row\" onClick={() => pick(item)}>{item.name}: ${item.price}</li>\n    }\n  }\n  <style>\n    .row { padding: 1rem; }\n  </style>\n}\n\n",
}

func build(sample string, size int) string {
	return strings.Repeat(sample, size/len(sample)+1)[:size]
}

// timeIt returns the p50 and p95 of n runs of the tokenizer over src.
func timeIt(t *testing.T, name, src string, n int) (p50, p95 time.Duration) {
	t.Helper()
	lexer := lexers.Get(name)
	require.NotNil(t, lexer, name)
	runs := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		it, err := lexer.Tokenise(nil, src)
		require.NoError(t, err)
		_ = it.Tokens()
		runs = append(runs, time.Since(start))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
	return runs[len(runs)/2], runs[(len(runs)*95)/100]
}

func TestHighlightTimeIsLinearAndWithinBudget(t *testing.T) {
	sizes := []int{10 << 10, 100 << 10, 400 << 10}
	// 8 microseconds per byte is far above what the lexers need and far below what a quadratic scan costs at 400 KB
	const budgetPerByte = 8 * time.Microsecond
	for name, sample := range samples {
		t.Run(name, func(t *testing.T) {
			var perByte []float64
			for _, size := range sizes {
				p50, p95 := timeIt(t, name, build(sample, size), 5)
				t.Logf("%-10s %4d KB  p50 %8s  p95 %8s  %.2f us/KB", name, size>>10, p50.Round(time.Microsecond), p95.Round(time.Microsecond), float64(p50.Microseconds())/float64(size>>10))
				assert.Less(t, p95, budgetPerByte*time.Duration(size), "%s at %d KB", name, size>>10)
				perByte = append(perByte, float64(p50)/float64(size))
			}
			// the time per byte of the largest input is within 4 times that of the smallest: not quadratic
			assert.Less(t, perByte[len(perByte)-1], perByte[0]*4+1, "%s: time per byte grew with the size", name)
		})
	}
}

// Hostile inputs for a lexer with lookahead: many openers that never close must not cost the file size each.
func TestHostileInputsStayLinear(t *testing.T) {
	cases := map[string]string{
		"unclosed style":   strings.Repeat("<style>", 4000),
		"unclosed comment": strings.Repeat("/* ", 20000),
		"unclosed tags":    strings.Repeat("<div a=\"", 8000),
		"deep braces":      strings.Repeat("@{ @if (x) { ", 5000),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			it, err := Lexer.Tokenise(nil, src)
			require.NoError(t, err)
			_ = it.Tokens()
			assert.Less(t, time.Since(start), 3*time.Second, name)
		})
	}
}
