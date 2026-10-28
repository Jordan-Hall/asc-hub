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

// ascServiceWorker is the script that shows the notifications of the hub and opens the page a click is for. It reads
// the message the hub sent (title, body, a path of the hub), and opens only a path on this site.
const ascServiceWorker = `"use strict";
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
self.addEventListener("push", (event) => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch { data = {}; }
  const title = String(data.title || "ASC Hub").slice(0, 200);
  const options = {body: String(data.body || "").slice(0, 300), data: {path: typeof data.path === "string" ? data.path : ""}};
  if (typeof data.tag === "string" && data.tag) options.tag = data.tag.slice(0, 100);
  event.waitUntil(self.registration.showNotification(title, options));
});
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const path = event.notification.data && event.notification.data.path;
  const safe = typeof path === "string" && path.startsWith("/") && !path.startsWith("//") ? path : "/";
  const target = new URL(safe, self.location.origin).href;
  event.waitUntil(self.clients.matchAll({type: "window", includeUncontrolled: true}).then((list) => {
    for (const client of list) {
      if (client.url === target && "focus" in client) return client.focus();
    }
    return self.clients.openWindow(target);
  }));
});
`

// AscServiceWorker serves GET /-/asc-sw.js. It is served from here and not from the assets so that it may control the
// whole hub.
func AscServiceWorker(ctx *context.Context) {
	h := ctx.Resp.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Service-Worker-Allowed", setting.AppSubURL+"/")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	ctx.Resp.WriteHeader(http.StatusOK)
	_, _ = ctx.Resp.Write([]byte(ascServiceWorker))
}
