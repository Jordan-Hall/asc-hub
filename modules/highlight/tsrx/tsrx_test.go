// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package tsrx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokens renders the token stream of src as one "Type value" line per token, leaving out blank text.
func tokens(t *testing.T, src string) string {
	t.Helper()
	it, err := Lexer.Tokenise(nil, src)
	require.NoError(t, err)
	var sb strings.Builder
	for _, tok := range it.Tokens() {
		if tok.Value == "" || tok.Type == chroma.Text && strings.TrimSpace(tok.Value) == "" {
			continue
		}
		fmt.Fprintf(&sb, "%s %q\n", tok.Type, tok.Value)
	}
	return sb.String()
}

// TestGolden tokenizes every testdata/*.tsrx and compares with the .golden file next to it. Run with
// UPDATE_GOLDEN=1 to rewrite the files after a change of the lexer, and read the diff.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.tsrx")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			require.NoError(t, err)
			got := tokens(t, string(src))
			golden := strings.TrimSuffix(f, ".tsrx") + ".golden"
			if os.Getenv("UPDATE_GOLDEN") != "" {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			assert.Equal(t, strings.ReplaceAll(string(want), "\r\n", "\n"), got)
		})
	}
}

// has reports whether the stream of src holds a token with this type and value.
func has(t *testing.T, src string, typ chroma.TokenType, value string) bool {
	t.Helper()
	it, err := Lexer.Tokenise(nil, src)
	require.NoError(t, err)
	for _, tok := range it.Tokens() {
		if tok.Type == typ && tok.Value == value {
			return true
		}
	}
	return false
}

func TestRegistered(t *testing.T) {
	assert.Equal(t, "TSRX", lexers.Get("tsrx").Config().Name)
	assert.Equal(t, "TSRX", lexers.Get("TSRX").Config().Name)
	assert.Equal(t, "TSRX", lexers.Match("src/card.tsrx").Config().Name)
}

func TestContainerAndControlFlow(t *testing.T) {
	src := "function A() @{\n  @for (const u of us) {\n    @if (u.on) {\n      <b>{u.name}</b>\n    }\n  }\n}\n"
	assert.True(t, has(t, src, chroma.NameDecorator, "@{"))
	assert.True(t, has(t, src, chroma.KeywordReserved, "@for"))
	assert.True(t, has(t, src, chroma.KeywordReserved, "@if"))
	assert.True(t, has(t, src, chroma.NameTag, "b"))
	assert.True(t, has(t, src, chroma.KeywordDeclaration, "const"))
	assert.True(t, has(t, src, chroma.Keyword, "of"))
}

func TestStyleBodyIsCSS(t *testing.T) {
	src := "function A() @{\n  <>\n    <p>x</p>\n    <style>\n      .card { padding: 1rem; }\n    </style>\n  </>\n}\n"
	assert.True(t, has(t, src, chroma.NameClass, "card"), "the CSS lexer sees the selector")
	assert.True(t, has(t, src, chroma.NameTag, "style"))
	assert.False(t, has(t, src, chroma.Text, "padding"), "CSS is not left as text")
}

func TestMarkupVersusGenericsAndComparison(t *testing.T) {
	// A generic or a comparison is TypeScript, not an element.
	src := "const a = useState<string>('x');\nconst b = n < 3 && m > 1;\n"
	assert.False(t, has(t, src, chroma.NameTag, "string"))
	assert.True(t, has(t, src, chroma.LiteralStringSingle, "'x'"))
	// An element after "=" or "(" is markup.
	src = "const el = (<Foo.Bar a=\"1\" b={2} />);\n"
	assert.True(t, has(t, src, chroma.NameTag, "Foo.Bar"))
	assert.True(t, has(t, src, chroma.NameAttribute, "a"))
	assert.True(t, has(t, src, chroma.LiteralStringDouble, "\"1\""))
}

func TestDollarInTextIsText(t *testing.T) {
	src := "function C() @{\n  <p>Subtotal: ${subtotal}</p>\n}\n"
	assert.True(t, has(t, src, chroma.Text, "Subtotal: $"))
	assert.True(t, has(t, src, chroma.Name, "subtotal"))
}

func TestTemplateLiteralInterpolation(t *testing.T) {
	src := "const m = `Hello, ${name ? name : 'x'}!`;\n"
	assert.True(t, has(t, src, chroma.LiteralStringInterpol, "${"))
	assert.True(t, has(t, src, chroma.Name, "name"))
	assert.True(t, has(t, src, chroma.LiteralStringBacktick, "!"))
}

func TestStateIsBalanced(t *testing.T) {
	// After a complete component the lexer is back at top level: the next declaration is TypeScript again.
	src := "function A() @{\n  <p>x</p>\n}\nconst after = 1;\n"
	assert.True(t, has(t, src, chroma.KeywordDeclaration, "const"))
	assert.True(t, has(t, src, chroma.LiteralNumber, "1"))
}
