// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"slices"
	"strings"

	"forgejo.org/modules/indexer/code/internal"
)

type formatter struct{}

// Format groups the numbered lines of a hit into blocks (a gap in the numbers starts a new block) and
// highlights the matches. The highlighter escapes the text.
func (formatter) Format(r *internal.SearchResult) (*internal.Result, error) {
	var lines []internal.ResultLine
	n := len(r.LineNumbers)
	for i := 0; i < n; {
		j := i + 1
		for j < n && r.LineNumbers[j] == r.LineNumbers[j-1]+1 {
			j++
		}
		start := r.LineOffsets[i]
		end := len(r.Content)
		if j < n {
			end = r.LineOffsets[j]
		}
		var ranges [][3]int
		for _, m := range r.Matches {
			k := slices.Index(r.LineNumbers[i:j], m.LineNumber)
			if k < 0 {
				continue
			}
			off := r.LineOffsets[i+k]
			ranges = append(ranges, [3]int{k, m.Start - off, m.End - off})
		}
		block := strings.TrimSuffix(r.Content[start:end], "\n")
		lines = append(lines, internal.HighlightSearchResultCode(r.Filename, r.LineNumbers[i:j], ranges, block)...)
		i = j
	}
	return &internal.Result{
		RepoID:      r.RepoID,
		Filename:    r.Filename,
		CommitID:    r.CommitID,
		UpdatedUnix: r.UpdatedUnix,
		Language:    r.Language,
		Color:       r.Color,
		Lines:       lines,
	}, nil
}
