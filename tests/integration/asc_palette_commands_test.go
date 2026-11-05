// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ascPaletteItemJSON struct {
	Title string `json:"title"`
	Href  string `json:"href"`
}

type ascPaletteJSON struct {
	Verb   bool `json:"verb"`
	Groups []struct {
		Key   string               `json:"key"`
		Label string               `json:"label"`
		Items []ascPaletteItemJSON `json:"items"`
	} `json:"groups"`
}

func ascPaletteCommandsOf(t *testing.T, s *TestSession, q, at string) (bool, []ascPaletteItemJSON) {
	t.Helper()
	resp := s.MakeRequest(t, NewRequest(t, "GET", "/-/asc/palette?"+url.Values{"q": {q}, "at": {at}}.Encode()), http.StatusOK)
	var out ascPaletteJSON
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	for _, g := range out.Groups {
		if g.Key == "commands" {
			assert.Equal(t, "Commands", g.Label)
			return out.Verb, g.Items
		}
	}
	return out.Verb, nil
}

// TestAscPaletteCommands checks the Commands group of the palette (board Shell): a command verb switches the list to
// the commands of the page, each command goes to a page that asks first, and only a writer of the code gets them.
func TestAscPaletteCommands(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	owner := loginUser(t, "user2")

	verb, items := ascPaletteCommandsOf(t, owner, "undo", "/user2/repo1/pulls/3")
	assert.True(t, verb)
	require.Len(t, items, 1)
	assert.Equal(t, "Undo last operation", items[0].Title)
	assert.Equal(t, "/user2/repo1/asc/ops/undo/last", items[0].Href)

	_, items = ascPaletteCommandsOf(t, owner, "restack", "/user2/repo1/pulls/3")
	require.Len(t, items, 1)
	assert.Equal(t, "Restack this stack", items[0].Title)
	assert.Equal(t, "/user2/repo1/pulls/3/asc/confirm?op=restack", items[0].Href)

	_, items = ascPaletteCommandsOf(t, owner, "merge", "/user2/repo1/pulls/3")
	require.Len(t, items, 1)
	assert.Equal(t, "Merge when ready", items[0].Title)

	verb, items = ascPaletteCommandsOf(t, owner, "last operation", "/user2/repo1")
	assert.False(t, verb)
	require.Len(t, items, 1, "a title is found by its words")

	reader := loginUser(t, "user5")
	verb, items = ascPaletteCommandsOf(t, reader, "undo", "/user2/repo1")
	assert.True(t, verb)
	assert.Empty(t, items, "a reader gets no commands")

	t.Run("UndoLastNeedsASignIn", func(t *testing.T) {
		resp := MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/asc/ops/undo/last"), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")
	})
}
