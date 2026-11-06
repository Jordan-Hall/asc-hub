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

// The header search sends to /-/asc/search, the search of every repository.
func TestAscGlobalSearchRoute(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	MakeRequest(t, NewRequest(t, "GET", "/-/asc/search"), http.StatusNotFound)

	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/explore/repos"), http.StatusOK).Body)
	action := doc.doc.Find("form.ds-palette-bar").AttrOr("action", "")
	require.Equal(t, "/-/asc/search", action, "the header search sends to the search of every repository")

	doc = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", action), http.StatusOK).Body)
	assert.Equal(t, "/-/asc/search", doc.doc.Find("#asc-search-form").AttrOr("action", ""))

	// the old address still serves the page
	MakeRequest(t, NewRequest(t, "GET", "/explore/code"), http.StatusOK)
}
