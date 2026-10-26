// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascsearch is the hub side of cross-repository search, the symbol panel and the index coverage
// note (BUILDOUT section 14). The explore page, the repository search page and the JSON API share it.
//
// Trust. This package never decides who may read a repository. The caller hands it the repositories
// (Resolve is the one place that asks Forgejo) and the context decides who asc thinks the caller is: a
// user token (ascclient.WithUserToken) for the API, the internal token (ascclient.WithSystem) for the web
// pages, whose browser session holds no asc token. With the internal token asc would answer for every
// repository, so every call names the repositories it may search, and everything asc answers is
// checked against that list: a repository nobody asked for is dropped, not shown. All text is untrusted
// file content: it is made valid UTF-8, stripped of control and bidirectional characters, bounded, and
// left to the template or the JSON encoder to escape. A hit or chunk asc marked redacted loses every text
// field here even if the message carried one. The text of an asc error never leaves this package.
package ascsearch

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bounds on what one request sends and what one answer is read back as.
const (
	// MinPattern and MaxPattern bound a cross-repository search pattern in bytes (CrossSearch).
	MinPattern = 3
	MaxPattern = 512
	// MaxName bounds a symbol name; MaxSemantic a question for the meaning search.
	MaxName     = 256
	MaxSemantic = 1000
	// MaxPath bounds one path or path prefix; MaxLang one language name.
	MaxPath = 1024
	MaxLang = 40
	// MaxPaths and MaxLangs bound the repeated scope fields.
	MaxPaths = 16
	MaxLangs = 8
	// MaxContext bounds the context lines of a text hit.
	MaxContext = 5

	maxLineBytes  = 4096
	maxChunkBytes = 8192
	maxChunkLines = 40
	maxLabelBytes = 64
	maxDocRunes   = 400
	maxTokenBytes = 8192
)

// IsBidi reports the characters that reorder or split displayed text ("Trojan Source").
func IsBidi(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F, r == 0x2028, r == 0x2029:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// Plain reports whether s is valid UTF-8 of at most limit bytes without control or bidirectional characters.
func Plain(s string, limit int) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || IsBidi(r) })
}

// Clean makes an untrusted text safe to show on one line: it is cut at limit bytes, made valid UTF-8, and
// loses control characters (a tab stays) and bidirectional characters.
func Clean(b []byte, limit int) string {
	if limit > 0 && len(b) > limit {
		b = b[:limit]
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return r
		case unicode.IsControl(r), IsBidi(r):
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(b), "�"))
}

// Line is Clean for a matching line.
func Line(b []byte) string { return Clean(b, maxLineBytes) }

// Label is Clean for a short name, kind or container, shortened to maxLabelBytes.
func Label(s string) string { return Clean([]byte(s), maxLabelBytes) }

// Block makes a multi-line untrusted text safe: at most maxChunkBytes and maxChunkLines lines, each line
// cleaned, line ends normalised to \n, trailing blank space removed.
func Block(b []byte) string {
	if len(b) > maxChunkBytes {
		b = b[:maxChunkBytes]
	}
	s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > maxChunkLines {
		lines = lines[:maxChunkLines]
	}
	for i, l := range lines {
		lines[i] = Clean([]byte(l), maxLineBytes)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n ")
}

// Doc makes a documentation comment one short line.
func Doc(b []byte) string {
	if len(b) > maxDocRunes*4 {
		b = b[:maxDocRunes*4]
	}
	spaced := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, string(b))
	s := strings.Join(strings.Fields(Clean([]byte(spaced), 0)), " ")
	if utf8.RuneCountInString(s) > maxDocRunes {
		s = string([]rune(s)[:maxDocRunes]) + "..."
	}
	return s
}

// Hex renders an id as lower-case hex; an id longer than 64 bytes is not an id and renders empty.
func Hex(b []byte) string {
	if len(b) > 64 {
		return ""
	}
	return hex.EncodeToString(b)
}

// Path checks a repository path coming from asc: relative, valid UTF-8, no dot segments, no control,
// bidirectional or backslash characters, at most MaxPath bytes. It returns false for the empty path.
func Path(b []byte) (string, bool) {
	p, ok := CheckPath(string(b))
	return p, ok && p != ""
}

// CheckPath checks a path coming from a request. The empty path is accepted and stays empty; a leading
// slash is dropped.
func CheckPath(p string) (string, bool) {
	p = strings.TrimPrefix(p, "/")
	if len(p) > MaxPath || !utf8.ValidString(p) || strings.ContainsFunc(p, func(r rune) bool {
		return unicode.IsControl(r) || r == '\\' || IsBidi(r)
	}) {
		return "", false
	}
	if p == "" {
		return "", true
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return p, true
}

// CheckLang accepts the name of a language: letters, digits, space and . + # - _, at most MaxLang bytes.
func CheckLang(s string) bool {
	if s == "" || len(s) > MaxLang {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" .+#-_", r) {
			return false
		}
	}
	return true
}

// CheckToken accepts an opaque page token: visible ASCII, at most 8 KiB. The empty token is accepted.
func CheckToken(t string) bool {
	if len(t) > maxTokenBytes {
		return false
	}
	for i := range len(t) {
		if t[i] < 0x21 || t[i] > 0x7e {
			return false
		}
	}
	return true
}

// OutToken drops a next-page token that is not acceptable, so a bad token from asc never reaches a link.
func OutToken(t string) string {
	if !CheckToken(t) {
		return ""
	}
	return t
}
