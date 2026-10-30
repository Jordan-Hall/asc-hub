// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// indexOfLiteral finds `index X 0` (also 1, 2): the call fails for a list that is too short, so an empty list
// (no repositories, no runs, a new account) makes the page a 500. Explore did exactly that with `index .Repos 0`.
var indexOfLiteral = regexp.MustCompile(`\bindex\s+(\$?[.\w]+)\s+([0-9]+)\b`)

// guardWords say that the list was looked at before: a condition, a range, a with or a length test.
var guardWords = regexp.MustCompile(`\{\{-?\s*(if|else if|with|range|and)\b|\blen\b`)

// Calls that are safe for a reason the text cannot show. Each entry is a file and the expressions that always hold an
// element where the template runs.
var indexGuardAllowed = map[string][]string{
	// the comments of a conversation are rendered only for a conversation, which has at least its first comment; the
	// diff of a code comment is only taken when CommentMustAsDiff gave one with a file (the line before tests it)
	"repo/diff/conversation.tmpl":               {".comments"},
	"repo/issue/view_content/conversation.tmpl": {".comments", "$diff.Files"},
	// the dashboard feed items always carry the issue infos their sentence needs (models/activities)
	"user/dashboard/feed_sentence.tmpl": {".GetIssueInfos"},
	"user/dashboard/feeds.tmpl":         {".GetIssueInfos"},
	// the work-in-progress prefixes come from the settings, which always hold at least one
	"repo/issue/new_form.tmpl":                      {".PullRequestWorkInProgressPrefixes"},
	"repo/issue/view_content/sidebar/pull_wip.tmpl": {".PullRequestWorkInProgressPrefixes"},
	// a forced push comment holds the old and the new commit
	"repo/issue/view_content/comments.tmpl": {".Commits"},
	// inside a range over the same list
	"repo/home_dir.tmpl": {"CodeSearchOptions"},
	// Avatar is true only when the row has at least one person (routers/web/repo/asc_stack_list.go)
	"repo/stack_row.tmpl": {".People"},
}

// TestNoUnguardedIndexOfLiteral fails for a template that takes an element of a list by number without looking at the
// list first, on the same line or the three before it. A page must render with nothing to show.
func TestNoUnguardedIndexOfLiteral(t *testing.T) {
	root := filepath.Join("..", "..", "templates")
	_, err := os.Stat(root)
	require.NoError(t, err)
	var bad []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".tmpl") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "swagger/") || strings.HasPrefix(rel, "mail/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			for _, loc := range indexOfLiteral.FindAllStringSubmatchIndex(line, -1) {
				expr := line[loc[2]:loc[3]]
				if slices.ContainsFunc(indexGuardAllowed[rel], func(a string) bool { return strings.Contains(expr, a) }) {
					continue
				}
				// what the template said before the call: the three lines above and this line up to the call
				before := strings.Join(lines[max(0, i-3):i], "\n") + "\n" + line[:loc[0]]
				if !guardedBefore(before, expr) {
					bad = append(bad, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line[loc[0]:loc[1]]))
				}
			}
		}
		return nil
	}))
	assert.Empty(t, bad, "index of a list by number with no look at the list before; guard it with {{if .List}} or {{with .List}}")
}

// guardedBefore reports whether a condition, with, range or length test that names expr stands in the text.
func guardedBefore(before, expr string) bool {
	for _, g := range guardWords.FindAllStringIndex(before, -1) {
		if strings.Contains(before[g[0]:], expr) {
			return true
		}
	}
	return false
}
