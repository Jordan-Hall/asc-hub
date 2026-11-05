// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/setting"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func docsTOC(page *HTMLDoc) []string {
	return page.doc.Find("aside.site-toc [data-toc] a").Map(func(_ int, a *goquery.Selection) string { return a.Text() })
}

// TestAscDocsInstall checks the install page of the documentation (board Docs): the release's commands behind one tab
// for each system, the visitor's system first, the signature check and the command that connects asc to this hub.
func TestAscDocsInstall(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	hub := strings.TrimSuffix(setting.AppURL, "/")

	t.Run("Release", func(t *testing.T) {
		useAscRelease(t, "")
		page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/-/docs/install").SetHeader("User-Agent", uaLinux), http.StatusOK).Body)
		assert.Equal(t, 1, page.doc.Find("h1").Length(), "the article has one heading")
		assert.Equal(t, "Install ASC", strings.TrimSpace(page.doc.Find("h1").Text()))

		list := page.doc.Find("[data-asc-docs-install] [role=tablist][data-doc-tabs]")
		assert.Equal(t, "asc-docs-os", list.AttrOr("data-doc-remember", ""), "the choice is kept")
		tabs := list.Find("[role=tab]")
		assert.Equal(t, 3, tabs.Length())
		assert.Equal(t, "true", list.Find("[data-doc-tab=linux]").AttrOr("aria-selected", ""), "the visitor's system first")
		assert.Equal(t, "-1", list.Find("[data-doc-tab=macos]").AttrOr("tabindex", ""))
		page.AssertElement(t, "#install-panel-linux[role=tabpanel]:not([hidden])", true)
		page.AssertElement(t, "#install-panel-macos[hidden]", true)
		page.AssertElement(t, "#install-panel-windows[hidden]", true)
		assert.Contains(t, page.doc.Find("#install-panel-macos").Text(), "brew install asc/tap/asc")
		assert.Contains(t, page.doc.Find("#install-panel-windows").Text(), "winget install --exact --id asc.asc")

		// Copy copies the commands without the prompt, which a screen reader does not read either
		for _, id := range []string{"#install-panel-macos", "#install-step-verify", "#install-step-connect"} {
			term := page.doc.Find(id).Closest(".site-install-b").Find(".ds-term").First()
			require.Equal(t, 1, term.Length(), id)
			text := term.Find(".ds-copy").AttrOr("data-clipboard-text", "")
			assert.NotEmpty(t, text, id)
			assert.False(t, strings.HasPrefix(text, "$"), id)
			assert.Equal(t, "true", term.Find(".ds-pr").First().AttrOr("aria-hidden", ""), id)
		}
		verify := page.doc.Find("#install-step-verify").Parent().Find(".ds-copy").AttrOr("data-clipboard-text", "")
		assert.True(t, strings.HasPrefix(verify, "minisign -Vm SHA256SUMS"), verify)
		assert.Contains(t, verify, "\nsha256sum -c --ignore-missing SHA256SUMS")
		connect := page.doc.Find("#install-step-connect").Parent().Find(".ds-copy").AttrOr("data-clipboard-text", "")
		assert.Equal(t, ascdownload.SignInCommand(setting.AppURL), connect)
		assert.Equal(t, "asc auth login --host "+hub, connect, "the browser sign-in names this hub")

		assert.Equal(t, []string{"Before you start", "1. Install", "2. Verify the signature", "3. Connect to your hub", "Update and sign out", "Troubleshooting"}, docsTOC(page))
		assert.Contains(t, page.doc.Find("article").Text(), "asc upgrade --check", "the text after the steps")
		assert.NotContains(t, page.doc.Find("article").Text(), "asc-install-steps")

		// a Windows visitor gets the Windows tab first
		page = NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/-/docs/install").SetHeader("User-Agent", uaWindows), http.StatusOK).Body)
		assert.Equal(t, "true", page.doc.Find("[data-doc-tab=windows]").AttrOr("aria-selected", ""))
	})

	t.Run("NoRelease", func(t *testing.T) {
		old, oldRepo := setting.Asc.DownloadDir, setting.Asc.ReleaseRepo
		setting.Asc.DownloadDir, setting.Asc.ReleaseRepo = "", ""
		ascdownload.ResetCache()
		defer func() { setting.Asc.DownloadDir, setting.Asc.ReleaseRepo = old, oldRepo; ascdownload.ResetCache() }()

		page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/-/docs/install"), http.StatusOK).Body)
		page.AssertElement(t, "[data-asc-docs-install] [role=tablist]", false)
		assert.Contains(t, page.doc.Find("[data-asc-docs-install]").Text(), "cargo build --release -p asc")
		assert.Equal(t, []string{"Before you start", "1. Build from source", "2. Connect to your hub", "Update and sign out", "Troubleshooting"}, docsTOC(page))
		assert.Equal(t, "asc auth login --host "+hub, page.doc.Find("#install-step-connect").Parent().Find(".ds-copy").AttrOr("data-clipboard-text", ""))
	})

	t.Run("OtherPages", func(t *testing.T) {
		page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/-/docs/why"), http.StatusOK).Body)
		assert.Equal(t, 1, page.doc.Find("h1").Length(), "the source's own first heading is not shown twice")
		page.AssertElement(t, "[data-asc-docs-install]", false)
		page.AssertElement(t, "aside.site-toc a[href='/-/docs/install']", true)
	})
}
