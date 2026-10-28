// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	htmlpkg "html"
	"net/http"
	"regexp"
	"strings"

	"forgejo.org/models/asymkey"
	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/markup"
	"forgejo.org/modules/markup/markdown"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascdocs"
	"forgejo.org/services/context"
)

// The public pages of the hub: about, documentation start page and the get started checklist.
const (
	tplAscAbout      = "asc/about"
	tplAscDocs       = "asc/docs"
	tplAscGetStarted = "asc/get_started"
)

// AscAbout serves GET /-/about.
func AscAbout(ctx *context.Context) {
	prepareAscDownload(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.about_title")
	ctx.Data["PageIsAscAbout"] = true
	ctx.HTML(http.StatusOK, tplAscAbout)
}

// AscOffline serves GET /-/offline: the page a browser shows from its cache when the hub cannot be reached.
func AscOffline(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Locale.TrString("asc_err.title_offline")
	ctx.HTML(http.StatusOK, "status/offline")
}

// AscGetStarted serves GET /-/get-started: the checklist of a person whose hub is new. It marks a step done only
// when the hub can see it is: a repository exists, an access token exists.
func AscGetStarted(ctx *context.Context) {
	doer := ctx.Doer
	tokens, err := db.Count[auth.AccessToken](ctx, auth.ListAccessTokensOptions{UserID: doer.ID})
	if err != nil {
		ctx.ServerError("CountAccessTokens", err)
		return
	}
	keys, err := db.Count[asymkey.PublicKey](ctx, asymkey.FindPublicKeyOptions{OwnerID: doer.ID})
	if err != nil {
		ctx.ServerError("CountPublicKeys", err)
		return
	}
	prepareAscDownload(ctx)
	done := 0
	connected := tokens > 0
	hasRepo := doer.NumRepos > 0
	if connected {
		done++
	}
	if hasRepo {
		done++
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.started_title")
	ctx.Data["PageIsAscGetStarted"] = true
	ctx.Data["GSConnected"] = connected
	ctx.Data["GSHasRepo"] = hasRepo
	ctx.Data["GSKeys"] = keys
	ctx.Data["GSDone"] = done
	ctx.Data["GSTotal"] = 4
	ctx.Data["GSPercent"] = done * 100 / 4
	ctx.Data["GSMigrations"] = !setting.Repository.DisableMigrations
	ctx.Data["GSConnectCommand"] = ascdownload.ConnectCommand(setting.AppURL)
	ctx.HTML(http.StatusOK, tplAscGetStarted)
}

// AscDocs serves GET /-/docs: the start page of the documentation that ships inside the hub, and its search (?q=).
func AscDocs(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.docs_title")
	ctx.Data["PageIsAscDocs"] = true
	ctx.Data["DocsPages"] = ascdocs.Pages()
	ctx.Data["DocsPopular"] = ascdocs.Popular()
	ctx.Data["DocsNew"] = ascdocs.Changelog()
	if q := strings.TrimSpace(ctx.FormString("q")); q != "" {
		if len(q) > 100 {
			q = q[:100]
		}
		ctx.Data["DocsQuery"] = q
		ctx.Data["DocsSearched"] = true
		ctx.Data["DocsHits"] = ascdocs.Search(q, 20)
	}
	ctx.HTML(http.StatusOK, tplAscDocs)
}

// AscDocsTOCItem is one heading of the "On this page" list.
type AscDocsTOCItem struct {
	ID   string
	Text string
}

var docsHeading = regexp.MustCompile(`<h[23] id="([^"]+)"[^>]*>(.*?)</h[23]>`)
var docsTag = regexp.MustCompile(`<[^>]*>`)

// AscDocsPage serves GET /-/docs/{slug}: one page of the documentation.
func AscDocsPage(ctx *context.Context) {
	page, md, ok := ascdocs.Get(ctx.PathParamRaw("slug"))
	if !ok {
		ctx.NotFound("docs page", nil)
		return
	}
	rendered, err := markdown.RenderString(&markup.RenderContext{Ctx: ctx, Type: markdown.MarkupName, Links: markup.Links{Base: setting.AppSubURL + "/-/docs"}}, md)
	if err != nil {
		ctx.ServerError("RenderString", err)
		return
	}
	html := string(rendered)
	var toc []AscDocsTOCItem
	for _, m := range docsHeading.FindAllStringSubmatch(html, 40) {
		toc = append(toc, AscDocsTOCItem{ID: m[1], Text: htmlpkg.UnescapeString(docsTag.ReplaceAllString(m[2], ""))})
	}
	ctx.Data["Title"] = page.Title + " - " + ctx.Locale.TrString("asc_site.docs_title")
	ctx.Data["PageIsAscDocs"] = true
	ctx.Data["DocsPage"] = page
	ctx.Data["DocsHTML"] = rendered
	ctx.Data["DocsTOC"] = toc
	ctx.Data["DocsPages"] = ascdocs.Pages()
	ctx.HTML(http.StatusOK, "asc/docs_page")
}
