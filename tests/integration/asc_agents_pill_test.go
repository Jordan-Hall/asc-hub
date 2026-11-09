// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The header and the phone top bar carry the "N agents running" pill, hidden until the page script has a count;
// the count route answers a signed-in person only.
func TestTheHeaderCarriesTheAgentsRunningPill(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	s := loginUser(t, "user2")

	t.Run("A signed-in page has the pill in both bars", func(t *testing.T) {
		doc := NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		for _, id := range []string{"hdr-agents", "topbar-agents"} {
			pill := doc.Find(`a.ds-agents-pill[data-testid="` + id + `"]`)
			require.Equal(t, 1, pill.Length(), id)
			assert.Equal(t, "/-/asc/agents", pill.AttrOr("href", ""))
			assert.Equal(t, "/-/asc/agents/count", pill.AttrOr("data-asc-agents-url", ""))
			assert.Equal(t, "{n} agent running", pill.AttrOr("data-word-one", ""))
			assert.Equal(t, "{n} agents running", pill.AttrOr("data-word-other", ""))
			assert.True(t, pill.HasClass("tw-hidden"), "hidden until the script knows a run is active")
			assert.Equal(t, 1, pill.Find(`.ds-agents-dot[aria-hidden="true"]`).Length())
		}
	})

	t.Run("On Your agents the pill is the current page", func(t *testing.T) {
		doc := NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/-/asc/agents"), http.StatusOK).Body)
		assert.Equal(t, "page", doc.Find(`a[data-testid="hdr-agents"]`).AttrOr("aria-current", ""))
	})

	t.Run("The count of a person without agent features is zero", func(t *testing.T) {
		resp := s.MakeRequest(t, NewRequest(t, "GET", "/-/asc/agents/count"), http.StatusOK)
		assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
		assert.JSONEq(t, `{"count":0}`, resp.Body.String())
	})

	t.Run("A visitor has no pill and no count", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(`.ds-agents-pill`).Length())
		resp := MakeRequest(t, NewRequest(t, "GET", "/-/asc/agents/count"), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")
	})

	t.Run("Without asc there is no pill", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		doc := NewHTMLParser(t, s.MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(`.ds-agents-pill`).Length())
	})
}
