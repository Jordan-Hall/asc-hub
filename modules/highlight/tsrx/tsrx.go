// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package tsrx is the syntax highlighter for TSRX (https://tsrx.dev/), the TypeScript extension for declarative
// UIs in .tsrx files. It is a chroma lexer of its own, not an alias of JSX or TSX, because the language has forms
// those lexers cannot see:
//
//   - the statement container `@{ ... }` that opens a component body (TypeScript setup, then one output node);
//   - `@if (cond) { ... }` and `@for (const x of xs) { ... }` as control flow of the template, whose blocks hold
//     TypeScript statements followed by markup;
//   - JSX-like elements and fragments, `{expression}` slots and text in which a lone `$` is text;
//   - a scoped `<style>` element whose body is CSS.
//
// Importing the package registers the lexer under the name TSRX, the alias tsrx and the file pattern *.tsrx, so
// highlight.File picks it by file name and a ```tsrx fence in Markdown picks it by alias.
package tsrx

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// styleOpen is the opening tag of a scoped style. The attributes are bounded so that a "<style" without its ">"
// costs a line, not the rest of the file.
const styleOpen = `(<)(style)((?:\s[^>
]{0,200})?>)`

const ident = `[A-Za-z_$][\w$]*`

// A "<" starts markup only where an expression can start: not after a word, ")" or "]" (a generic or a
// comparison), and only before a letter, "_" or ">" (a fragment).
const tagGuard = `(?<![\w$)\]])`

var keywords = []string{
	"abstract", "as", "async", "await", "break", "case", "catch", "continue", "debugger", "declare", "default",
	"delete", "do", "else", "export", "extends", "finally", "for", "from", "get", "if", "implements", "import",
	"in", "instanceof", "is", "keyof", "namespace", "new", "of", "private", "protected", "public", "readonly",
	"return", "satisfies", "set", "static", "super", "switch", "throw", "try", "typeof", "void", "while",
	"with", "yield", "infer", "override", "accessor", "using",
}

var declarations = []string{"const", "let", "var", "function", "class", "interface", "type", "enum"}

var constants = []string{"true", "false", "null", "undefined", "this", "NaN", "Infinity"}

var builtinTypes = []string{
	"string", "number", "boolean", "any", "unknown", "never", "object", "symbol", "bigint",
}

func codeRules() []chroma.Rule {
	return []chroma.Rule{
		{Pattern: `\s+`, Type: chroma.Text},
		{Pattern: `//[^\n]*`, Type: chroma.CommentSingle},
		{Pattern: `/\*[\s\S]*?\*/`, Type: chroma.CommentMultiline},
		// an unterminated comment runs to the end of the file; matching it here keeps the failed search for "*/" to one
		{Pattern: `/\*[\s\S]*`, Type: chroma.CommentMultiline},

		// The three TSRX forms that start with "@".
		{Pattern: `@\{`, Type: chroma.NameDecorator, Mutator: chroma.Push("braces")},
		{Pattern: `@(?:if|for)\b`, Type: chroma.KeywordReserved, Mutator: chroma.Push("control")},

		// Markup inside TypeScript: a scoped style, a fragment, an element.
		{Pattern: tagGuard + styleOpen, Type: chroma.ByGroups(chroma.Punctuation, chroma.NameTag, chroma.Punctuation), Mutator: chroma.Push("style")},
		{Pattern: tagGuard + `<>`, Type: chroma.Punctuation, Mutator: chroma.Push("children")},
		{Pattern: tagGuard + `<(?=[A-Za-z_])`, Type: chroma.Punctuation, Mutator: chroma.Push("tag")},

		{Pattern: "`", Type: chroma.LiteralStringBacktick, Mutator: chroma.Push("template")},
		{Pattern: `'(?:[^'\\\n]|\\.)*'`, Type: chroma.LiteralStringSingle},
		{Pattern: `"(?:[^"\\\n]|\\.)*"`, Type: chroma.LiteralStringDouble},

		{Pattern: `0[xX][\da-fA-F_]+n?`, Type: chroma.LiteralNumberHex},
		{Pattern: `0[bB][01_]+n?`, Type: chroma.LiteralNumberBin},
		{Pattern: `\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?\d+)?n?`, Type: chroma.LiteralNumber},

		{Pattern: `(function\*?)(\s+)(` + ident + `)`, Type: chroma.ByGroups(chroma.KeywordDeclaration, chroma.Text, chroma.NameFunction)},
		{Pattern: `(class|interface|type|enum|namespace)(\s+)(` + ident + `)`, Type: chroma.ByGroups(chroma.KeywordDeclaration, chroma.Text, chroma.NameClass)},
		{Pattern: chroma.Words(`\b`, `\b`, declarations...), Type: chroma.KeywordDeclaration},
		{Pattern: chroma.Words(`\b`, `\b`, constants...), Type: chroma.KeywordConstant},
		{Pattern: chroma.Words(`\b`, `\b`, builtinTypes...), Type: chroma.KeywordType},
		{Pattern: chroma.Words(`\b`, `\b`, keywords...), Type: chroma.Keyword},

		{Pattern: ident, Type: chroma.Name},
		{Pattern: `=>|\.\.\.|[-+*/%&|^!~=<>?:]+`, Type: chroma.Operator},
		{Pattern: `\{`, Type: chroma.Punctuation, Mutator: chroma.Push("braces")},
		{Pattern: `[()\[\];,.}]`, Type: chroma.Punctuation},
		{Pattern: `.`, Type: chroma.Text},
	}
}

func rules() chroma.Rules {
	return chroma.Rules{
		"root": codeRules(),

		// `{ ... }` in TypeScript, in an attribute value or text slot, and the body of `@{`, `@if` and `@for`.
		"braces": append([]chroma.Rule{
			{Pattern: `\}`, Type: chroma.Punctuation, Mutator: chroma.Pop(1)},
		}, codeRules()...),
		"parens": append([]chroma.Rule{
			{Pattern: `\)`, Type: chroma.Punctuation, Mutator: chroma.Pop(1)},
			{Pattern: `\(`, Type: chroma.Punctuation, Mutator: chroma.Push("parens")},
		}, codeRules()...),

		// After `@if` or `@for`: the condition in parentheses, then the block.
		"control": {
			{Pattern: `\s+`, Type: chroma.Text},
			{Pattern: `\(`, Type: chroma.Punctuation, Mutator: chroma.Push("parens")},
			{Pattern: `\{`, Type: chroma.Punctuation, Mutator: chroma.Mutators(chroma.Pop(1), chroma.Push("braces"))},
			chroma.Default(chroma.Pop(1)),
		},

		"template": {
			{Pattern: "`", Type: chroma.LiteralStringBacktick, Mutator: chroma.Pop(1)},
			{Pattern: `\\[\s\S]`, Type: chroma.LiteralStringEscape},
			{Pattern: `\$\{`, Type: chroma.LiteralStringInterpol, Mutator: chroma.Push("braces")},
			{Pattern: "[^`\\\\$]+", Type: chroma.LiteralStringBacktick},
			{Pattern: `\$`, Type: chroma.LiteralStringBacktick},
		},

		// The body of a scoped style is CSS up to the closing tag, or to the end of the file when there is none. The
		// search for the closing tag happens once per opener: when it fails, the second rule takes the rest.
		"style": {
			{Pattern: `([\s\S]*?)(</)(style)(\s*>)`, Type: chroma.ByGroups(chroma.Using("css"), chroma.Punctuation, chroma.NameTag, chroma.Punctuation), Mutator: chroma.Pop(1)},
			{Pattern: `[\s\S]+`, Type: chroma.Using("css"), Mutator: chroma.Pop(1)},
		},

		// The name of an element, right after its "<".
		"tag": {
			{Pattern: `[A-Za-z_][\w.:$-]*`, Type: chroma.NameTag, Mutator: chroma.Mutators(chroma.Pop(1), chroma.Push("attrs"))},
			chroma.Default(chroma.Pop(1)),
		},

		"attrs": {
			{Pattern: `\s+`, Type: chroma.Text},
			{Pattern: `/>`, Type: chroma.Punctuation, Mutator: chroma.Pop(1)},
			{Pattern: `>`, Type: chroma.Punctuation, Mutator: chroma.Mutators(chroma.Pop(1), chroma.Push("children"))},
			{Pattern: `=`, Type: chroma.Operator},
			{Pattern: `'[^']*'`, Type: chroma.LiteralStringSingle},
			{Pattern: `"[^"]*"`, Type: chroma.LiteralStringDouble},
			{Pattern: `\{`, Type: chroma.Punctuation, Mutator: chroma.Push("braces")},
			{Pattern: `[A-Za-z_][\w:.$-]*`, Type: chroma.NameAttribute},
			{Pattern: `.`, Type: chroma.Text},
		},

		// Between the tags of an element or fragment.
		"children": {
			{Pattern: `(</)([A-Za-z_][\w.:$-]*)?(\s*>)`, Type: chroma.ByGroups(chroma.Punctuation, chroma.NameTag, chroma.Punctuation), Mutator: chroma.Pop(1)},
			{Pattern: styleOpen, Type: chroma.ByGroups(chroma.Punctuation, chroma.NameTag, chroma.Punctuation), Mutator: chroma.Push("style")},
			{Pattern: `<>`, Type: chroma.Punctuation, Mutator: chroma.Push("children")},
			{Pattern: `<(?=[A-Za-z_])`, Type: chroma.Punctuation, Mutator: chroma.Push("tag")},
			{Pattern: `\{`, Type: chroma.Punctuation, Mutator: chroma.Push("braces")},
			{Pattern: `@(?:if|for)\b`, Type: chroma.KeywordReserved, Mutator: chroma.Push("control")},
			{Pattern: `[^<{@}]+`, Type: chroma.Text},
			{Pattern: `[<@}]`, Type: chroma.Text},
		},
	}
}

// Lexer is the TSRX lexer, registered with the global registry when the package loads.
var Lexer = lexers.Register(chroma.MustNewLexer(&chroma.Config{
	Name:      "TSRX",
	Aliases:   []string{"tsrx"},
	Filenames: []string{"*.tsrx"},
	MimeTypes: []string{"text/x-tsrx"},
	EnsureNL:  true,
	DotAll:    false,
}, rules))
