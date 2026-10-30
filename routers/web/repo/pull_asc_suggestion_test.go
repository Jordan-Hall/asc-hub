// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscSuggestionFailureAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		key    string
	}{
		"stale":       {&pull_service.AscSuggestionStale{Reason: "x"}, 0, "repo.pulls.asc_suggestion.stale"},
		"wrapped":     {fmt.Errorf("w: %w", &pull_service.AscSuggestionStale{Reason: "x"}), 0, "repo.pulls.asc_suggestion.stale"},
		"forbidden":   {pull_service.ErrAscRestoreForbidden, http.StatusForbidden, ""},
		"not stacked": {pull_service.ErrAscNotStacked, http.StatusNotFound, ""},
		"bad id":      {pull_service.ErrAscBadVersion, http.StatusNotFound, ""},
		"not open":    {pull_service.ErrAscRestoreNotOpen, 0, "repo.pulls.asc_versions.restore_not_open"},
		"unavailable": {pull_service.ErrAscUnavailable, 0, "repo.pulls.asc_suggestion.unavailable"},
		"bad answer":  {pull_service.ErrAscBadAnswer, 0, "repo.pulls.asc_suggestion.unavailable"},
	} {
		status, key, known := ascSuggestionFailure(tc.err)
		assert.True(t, known, name)
		assert.Equal(t, tc.status, status, name)
		assert.Equal(t, tc.key, key, name)
	}
	_, _, known := ascSuggestionFailure(errors.New("database is down"))
	assert.False(t, known, "anything else is a server error")
}

func TestAscSuggestionIDs(t *testing.T) {
	for in, ok := range map[string]bool{"1": true, "9007199254740991": true, "0": false, "": false, "-1": false, "x": false, "9007199254740992": false} {
		_, got := parseAscSuggestionID(in)
		assert.Equal(t, ok, got, in)
	}
}

func TestAscSuggestionsCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	list := []pull_service.AscSuggestion{
		{ThreadID: 11, CommentID: 22, Path: "src/a.rs", StartLine: 4, EndLine: 6, Author: "priya", Suggestion: "fn a() {}\n", Body: "tidy <b>up</b>", Version: 4},
		{ThreadID: 12, CommentID: 23, Path: "src/b.rs", StartLine: 1, EndLine: 1, Author: "bob", Suggestion: "x\n", Version: 2, Stale: true},
	}
	card := func(can bool, items []pull_service.AscSuggestion) string {
		return renderPart(t, "repo/pulls/asc_suggestions", func(d map[string]any) {
			d["AscSuggestions"] = items
			d["AscCanApplySuggestion"] = can
			d["AscSuggestionNext"] = uint32(5)
			d["CsrfTokenHtml"] = "<input type=\"hidden\" name=\"_csrf\" value=\"t\">"
		})
	}
	body := card(true, list)
	assert.Contains(t, body, `data-testid="asc-suggestions"`)
	assert.Equal(t, 2, countOf(body, `data-testid="asc-suggestion"`))
	assert.Equal(t, 1, countOf(body, `data-testid="asc-apply-suggestion"`), "a suggestion on an old version has no button")
	assert.Contains(t, body, `action="/user2/repo1/pulls/3/asc/apply_suggestion"`)
	assert.Contains(t, body, `name="thread" value="11"`)
	assert.Contains(t, body, `name="comment" value="22"`)
	assert.Contains(t, body, "repo.pulls.asc_suggestion.apply")
	assert.Contains(t, body, `data-testid="asc-suggestion-stale"`)
	assert.Contains(t, body, "tidy &lt;b&gt;up&lt;/b&gt;", "a comment is text, never markup")
	assert.Contains(t, body, "<code>src/a.rs</code>")

	assert.Equal(t, 0, countOf(card(false, list), `data-testid="asc-apply-suggestion"`), "a viewer who may not apply sees the suggestions only")
	assert.NotContains(t, card(true, nil), "asc-suggestions", "nothing to show, nothing drawn")
}

func countOf(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
