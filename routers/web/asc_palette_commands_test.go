// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaletteVerb(t *testing.T) {
	for q, want := range map[string]string{
		"undo": "undo", "UN": "undo", "rest": "restack", "merge it": "merge", "ask an agent": "ask",
		"u": "", "unknown": "", "": "", "fee undo": "", "me": "merge",
	} {
		assert.Equal(t, want, ascPaletteVerb(q), q)
	}
}

func TestPaletteAt(t *testing.T) {
	o, r, n, ok := ascPaletteAt("/user2/repo1/pulls/3/files")
	assert.True(t, ok)
	assert.Equal(t, []any{"user2", "repo1", int64(3)}, []any{o, r, n})
	_, _, n, ok = ascPaletteAt("/user2/repo1")
	assert.True(t, ok)
	assert.Zero(t, n)
	_, _, n, _ = ascPaletteAt("/user2/repo1/pulls/x")
	assert.Zero(t, n)
	for _, at := range []string{"/", "/user2", "/-/asc/inbox", ""} {
		_, _, _, ok := ascPaletteAt(at)
		assert.False(t, ok, at)
	}
}

type paletteAnswer struct {
	Groups []AscPaletteGroup `json:"groups"`
	Verb   bool              `json:"verb"`
}

func paletteAsk(t *testing.T, uid int64, q, at string) (paletteAnswer, []string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "GET /-/asc/palette")
	ctx.Req.Form = map[string][]string{"q": {q}, "at": {at}}
	if uid > 0 {
		contexttest.LoadUser(t, ctx, uid)
		ctx.IsSigned = true
	}
	AscPalette(ctx)
	require.Equal(t, http.StatusOK, resp.Code)
	var out paletteAnswer
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	var hrefs []string
	for _, g := range out.Groups {
		if g.Key == "commands" {
			for _, it := range g.Items {
				hrefs = append(hrefs, it.Href)
			}
		}
	}
	return out, hrefs
}

func TestPaletteCommands(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascPaletteUserLimiter = ascratelimit.New(1000, time.Second)
	ascPaletteLimiter = ascratelimit.New(1000, time.Second)

	t.Run("verb on an open change", func(t *testing.T) {
		out, hrefs := paletteAsk(t, 2, "restack", "/user2/repo1/pulls/3")
		assert.True(t, out.Verb)
		assert.Equal(t, []string{"/user2/repo1/pulls/3/asc/confirm?op=restack"}, hrefs)
		_, hrefs = paletteAsk(t, 2, "merge", "/user2/repo1/pulls/3")
		assert.Equal(t, []string{"/user2/repo1/pulls/3/asc/confirm?op=queue"}, hrefs)
		_, hrefs = paletteAsk(t, 2, "undo", "/user2/repo1/pulls/3")
		assert.Equal(t, []string{"/user2/repo1/asc/ops/undo/last"}, hrefs)
	})
	t.Run("a merged change has no restack or merge", func(t *testing.T) {
		out, hrefs := paletteAsk(t, 2, "restack", "/user2/repo1/pulls/2")
		assert.True(t, out.Verb, "the verb still switches the list")
		assert.Empty(t, hrefs)
	})
	t.Run("words of a title", func(t *testing.T) {
		// the locale of unit tests answers with the key: the title of Undo last operation is asc_shell.palette_cmd_undo
		out, hrefs := paletteAsk(t, 2, "cmd_undo", "/user2/repo1")
		assert.False(t, out.Verb)
		assert.Equal(t, []string{"/user2/repo1/asc/ops/undo/last"}, hrefs)
	})
	t.Run("ask an agent needs agent features", func(t *testing.T) {
		_, hrefs := paletteAsk(t, 2, "ask", "/user2/repo1/pulls/3")
		assert.Empty(t, hrefs)
		doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		require.NoError(t, ascagentui.SetFeatures(db.DefaultContext, doer, true))
		_, hrefs = paletteAsk(t, 2, "ask", "/user2/repo1/pulls/3")
		assert.Equal(t, []string{"/user2/repo1/asc/agents/new?target=branch2"}, hrefs)
		_, hrefs = paletteAsk(t, 2, "ask", "/user2/repo1")
		assert.Equal(t, []string{"/user2/repo1/asc/agents/new"}, hrefs)
	})
	t.Run("only for a writer of the code", func(t *testing.T) {
		out, hrefs := paletteAsk(t, 4, "undo", "/user2/repo1/pulls/3")
		assert.True(t, out.Verb)
		assert.Empty(t, hrefs, "a reader")
		_, hrefs = paletteAsk(t, 0, "undo", "/user2/repo1")
		assert.Empty(t, hrefs, "a visitor")
		_, hrefs = paletteAsk(t, 2, "undo", "/user2/no-such-repo")
		assert.Empty(t, hrefs)
		_, hrefs = paletteAsk(t, 2, "undo", "/explore/repos")
		assert.Empty(t, hrefs)
	})
	t.Run("off with ASC", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		_, hrefs := paletteAsk(t, 2, "undo", "/user2/repo1")
		assert.Empty(t, hrefs)
	})
}
