// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	htmlpkg "html"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"forgejo.org/models/asymkey"
	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/markup"
	"forgejo.org/modules/markup/markdown"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascdocs"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The public pages of the hub: about, documentation start page and the get started checklist.
const (
	tplAscAbout      = "asc/about"
	tplAscDocs       = "asc/docs"
	tplAscGetStarted = "asc/get_started"
	tplAscStatus     = "asc/status"
)

// AscAbout serves GET /-/about.
func AscAbout(ctx *context.Context) {
	prepareAscDownload(ctx)
	ascAboutData(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.about_title")
	ctx.Data["PageIsAscAbout"] = true
	ctx.HTML(http.StatusOK, tplAscAbout)
}

// AscStatus serves GET /-/status: whether each part of the hub answers, from the probe the footer uses, and the
// maintenance notice while the hub is in maintenance.
func AscStatus(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Locale.TrString("asc_status.page_title")
	ctx.Data["AscStatus"] = asctrust.SystemStatus()
	ctx.HTML(http.StatusOK, tplAscStatus)
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
	// the command line has signed in, so it is installed and connected: two steps; the third is a repository, the
	// fourth a team (an organization the person belongs to)
	if connected {
		done += 2
	}
	if hasRepo {
		done++
	}
	orgs, err := organization.GetUserOrgsList(ctx, doer)
	if err != nil {
		ctx.ServerError("GetUserOrgsList", err)
		return
	}
	hasOrg := len(orgs) > 0
	if hasOrg {
		done++
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_site.started_title")
	ctx.Data["PageIsAscGetStarted"] = true
	ctx.Data["GSName"] = doer.GetDisplayName()
	ctx.Data["GSConnected"] = connected
	ctx.Data["GSHasOrg"] = hasOrg
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

// docsInstallSteps marks where the install page shows the steps of this hub's release.
const docsInstallSteps = "<!-- asc-install-steps -->"

// renderDocs renders markdown of the documentation, wraps its tables so that a wide one scrolls and a keyboard can
// reach it, and lists its headings for "On this page".
func renderDocs(ctx *context.Context, md string) (template.HTML, []AscDocsTOCItem, error) {
	rendered, err := markdown.RenderString(&markup.RenderContext{Ctx: ctx, Type: markdown.MarkupName, Links: markup.Links{Base: setting.AppSubURL + "/-/docs"}}, md)
	if err != nil {
		return "", nil, err
	}
	html := string(rendered)
	var toc []AscDocsTOCItem
	for _, m := range docsHeading.FindAllStringSubmatch(html, 40) {
		toc = append(toc, AscDocsTOCItem{ID: m[1], Text: htmlpkg.UnescapeString(docsTag.ReplaceAllString(m[2], ""))})
	}
	wrapped := strings.ReplaceAll(html, "<table", `<div class="site-table" role="region" tabindex="0" aria-label="`+htmlpkg.EscapeString(ctx.Locale.TrString("asc_site.docs_table"))+`"><table`)
	wrapped = strings.ReplaceAll(wrapped, "</table>", "</table></div>")
	return template.HTML(wrapped), toc, nil
}

// AscDocsPage serves GET /-/docs/{slug}: one page of the documentation. The install page also shows the install,
// verify and connect steps of the release this hub offers, with the commands of each system.
func AscDocsPage(ctx *context.Context) {
	page, md, ok := ascdocs.Get(ctx.PathParamRaw("slug"))
	if !ok {
		ctx.NotFound("docs page", nil)
		return
	}
	// the page title is the heading of the article; the source's own first heading would be a second one
	if first, rest, found := strings.Cut(md, "\n"); found && strings.HasPrefix(first, "# ") {
		md = rest
	}
	before, after, steps := strings.Cut(md, docsInstallSteps)
	steps = steps && page.Slug == "install"
	if !steps {
		before, after = md, ""
	}
	html, toc, err := renderDocs(ctx, before)
	if err != nil {
		ctx.ServerError("RenderString", err)
		return
	}
	if steps {
		toc = append(toc, ascDocsInstall(ctx)...)
		var rest []AscDocsTOCItem
		if ctx.Data["DocsHTMLAfter"], rest, err = renderDocs(ctx, after); err != nil {
			ctx.ServerError("RenderString", err)
			return
		}
		toc = append(toc, rest...)
	}
	ctx.Data["Title"] = page.Title + " - " + ctx.Locale.TrString("asc_site.docs_title")
	ctx.Data["PageIsAscDocs"] = true
	ctx.Data["DocsPage"] = page
	ctx.Data["DocsEditURL"] = ascdocs.EditURL(page)
	ctx.Data["DocsHTML"] = html
	ctx.Data["DocsTOC"] = toc
	pages := ascdocs.Pages()
	ctx.Data["DocsPages"] = pages
	ctx.Data["DocsSections"] = ascdocs.Sections()
	for i, p := range pages {
		if p.Slug != page.Slug {
			continue
		}
		if i+1 < len(pages) {
			ctx.Data["DocsNext"] = pages[i+1]
		}
		break
	}
	ctx.HTML(http.StatusOK, "asc/docs_page")
}

// ascDocsInstall puts the steps of the install page into the page data: the release of the download page, or how to
// build asc from source when the hub offers none. It returns their entries of "On this page".
func ascDocsInstall(ctx *context.Context) []AscDocsTOCItem {
	ctx.Data["DocsInstall"] = true
	ctx.Data["AscSignInCommand"] = ascdownload.SignInCommand(setting.AppURL)
	first := "asc_site.docs_install_step_install"
	if !prepareAscDownload(ctx) {
		ctx.Data["AscNoRelease"] = true
		first = "asc_site.docs_install_step_source"
	}
	toc := []AscDocsTOCItem{{ID: "install-step-1", Text: "1. " + ctx.Locale.TrString(first)}}
	n := 2
	if view, ok := ctx.Data["AscDownload"].(*ascDownloadView); ok {
		for _, os := range view.Offers.OSes {
			if len(os.PackageCommands) > 0 {
				ctx.Data["AscHasPackages"] = true
			}
		}
		if view.Offers.VerifyCommand != "" {
			toc = append(toc, AscDocsTOCItem{ID: "install-step-verify", Text: "2. " + ctx.Locale.TrString("asc_site.docs_install_step_verify")})
			n = 3
		}
	}
	ctx.Data["DocsConnectStep"] = n
	return append(toc, AscDocsTOCItem{ID: "install-step-connect", Text: strconv.Itoa(n) + ". " + ctx.Locale.TrString("asc_site.docs_install_step_connect")})
}

// ascServiceWorker is the script that shows the notifications of the hub and opens the page a click is for. It reads
// the message the hub sent (title, body, a path of the hub), and opens only a path on this site.
const ascServiceWorker = `"use strict";
const CACHE = "asc-offline-v1";
const OFFLINE = new URL("-/offline", self.registration.scope).href;

// The offline page and its style sheets are kept when the worker is installed, so a hub that cannot be reached shows
// a page that looks like the hub and not an error of the browser.
async function precache() {
  const cache = await caches.open(CACHE);
  const res = await fetch(OFFLINE, {credentials: "omit"});
  if (!res.ok) return;
  const html = await res.clone().text();
  await cache.put(OFFLINE, res);
  const hrefs = [];
  for (const tag of html.match(/<link[^>]*>/g) || []) {
    if (/rel=["']stylesheet["']/.test(tag)) {
      const m = tag.match(/href=["']([^"']+)["']/);
      if (m) hrefs.push(new URL(m[1], OFFLINE).href);
    }
  }
  await Promise.all(hrefs.filter((h) => new URL(h).origin === self.location.origin).map((h) => cache.add(h).catch(() => {})));
}

self.addEventListener("install", (event) => {
  event.waitUntil(precache().catch(() => {}).then(() => self.skipWaiting()));
});
self.addEventListener("activate", (event) => {
  event.waitUntil(caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});
self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  if (req.mode === "navigate") {
    event.respondWith(fetch(req).catch(() => caches.match(OFFLINE).then((hit) => hit || Response.error())));
    return;
  }
  if (new URL(req.url).origin === self.location.origin && new URL(req.url).pathname.includes("/assets/")) {
    event.respondWith(fetch(req).catch(() => caches.match(req).then((hit) => hit || Response.error())));
  }
});
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
