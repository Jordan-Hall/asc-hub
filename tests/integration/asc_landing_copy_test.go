// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// The landing page carries the copy of the Main board (desktop) and the Mobile-Landing board (phone), and the
// footer carries the board's link groups and the status line.
func TestAscLandingCarriesTheBoardCopy(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
	desktop := doc.Find(".ds-land-boards").Text()
	for _, s := range []string{
		"Works with the editors and agents your team already uses",
		"Agents write faster than people can read. Source control has to change.",
		"Review 80 lines at a time",
		"Works with the editors and agents your team uses",
	} {
		assert.Contains(t, desktop, s)
	}
	footer := doc.Find("footer").Text()
	for _, s := range []string{"Stacks and changes", "CLI reference", "MCP server", "Acceptable use", "Subprocessors", "Cookies"} {
		assert.Contains(t, footer, s)
	}
	assert.Equal(t, 1, doc.Find(`footer [data-testid="footer-status"]`).Length())
}
