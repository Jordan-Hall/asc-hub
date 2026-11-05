// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/services/ascdocs"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscDocsDrawer checks the docs menu of a guide on a phone (board Mobile-Docs): a button before the breadcrumb
// opens a dialog with every guide by section, the open guide marked.
func TestAscDocsDrawer(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/-/docs/install"), http.StatusOK).Body)
	btn := page.Find(".site-docs-crumbs > a.site-docs-menu[data-ds-open='site-docs-drawer']")
	require.Equal(t, 1, btn.Length())
	assert.Equal(t, "Docs menu", btn.AttrOr("aria-label", ""))
	assert.Equal(t, "dialog", btn.AttrOr("aria-haspopup", ""))
	assert.Equal(t, "#site-docs-drawer", btn.AttrOr("href", ""), "without the script the drawer opens through :target")

	drawer := page.Find("#site-docs-drawer.ds-drawer[role=dialog][aria-modal=true]")
	require.Equal(t, 1, drawer.Length())
	assert.Equal(t, "Documentation", strings.TrimSpace(drawer.Find("#site-docs-drawer-title").Text()))
	assert.Equal(t, 1, drawer.Find("[data-ds-close][aria-label=Close]").Length())

	var labels []string
	drawer.Find(".ds-mi-label").Each(func(_ int, s *goquery.Selection) { labels = append(labels, s.Text()) })
	var want []string
	for _, s := range ascdocs.Sections() {
		want = append(want, s.Name)
	}
	assert.Equal(t, want, labels)
	assert.Equal(t, len(ascdocs.Pages()), drawer.Find("a.ds-mi").Length())
	current := drawer.Find("a.ds-mi[aria-current=page]")
	require.Equal(t, 1, current.Length())
	assert.Equal(t, "/-/docs/install", current.AttrOr("href", ""))
}
