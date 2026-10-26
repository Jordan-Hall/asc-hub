// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"strings"
	"unicode/utf8"
)

// Segment is a piece of a matching line; Hit pieces are drawn marked.
type Segment struct {
	Text string
	Hit  bool
}

// Highlight splits a cleaned matching line at the match columns (byte offsets of the starts of matches, as
// FileHit gives them). The width of a match is not on the wire: it is the length of pattern when the text
// there is that pattern (compared without regard to case), and one character otherwise; pass the empty
// pattern for a regular expression. A column that is not the start of a character, or that lies inside the
// previous match, is ignored. The pieces joined are the text.
func Highlight(text string, cols []uint32, pattern string) []Segment {
	width := func(at int) int {
		if n := len(pattern); n > 0 && at+n <= len(text) && strings.EqualFold(text[at:at+n], pattern) {
			return n
		}
		_, w := utf8.DecodeRuneInString(text[at:])
		return w
	}
	var segs []Segment
	pos := 0
	for _, c := range cols {
		at := int(c)
		if at < pos || at >= len(text) || !utf8.RuneStart(text[at]) {
			continue
		}
		end := min(at+width(at), len(text))
		if end < len(text) && !utf8.RuneStart(text[end]) {
			continue
		}
		if at > pos {
			segs = append(segs, Segment{Text: text[pos:at]})
		}
		segs = append(segs, Segment{Text: text[at:end], Hit: true})
		pos = end
	}
	if pos < len(text) || len(segs) == 0 {
		segs = append(segs, Segment{Text: text[pos:]})
	}
	return segs
}
