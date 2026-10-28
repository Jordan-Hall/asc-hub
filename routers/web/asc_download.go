// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"forgejo.org/modules/ascdownload"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascdownloadsync"
	"forgejo.org/services/context"
)

const tplAscDownload = "download"

// ascDownloadView is what the landing page and /download render.
type ascDownloadView struct {
	Offers *ascdownload.Offers
	// ActiveOS is the operating system tab shown first: the visitor's, or the first one the release has.
	ActiveOS string
	// ActiveArch is the processor of that system the visitor most likely has.
	ActiveArch string
	// Detected is true when ActiveOS came from the visitor's browser rather than from the release.
	Detected bool
	// ActiveOSOffer and ActiveArchOffer are the offer for ActiveOS and ActiveArch.
	ActiveOSOffer   *ascdownload.OSOffer
	ActiveArchOffer *ascdownload.ArchOffer
	// HubURL is this hub's root URL without the trailing slash.
	HubURL string
}

// ascDownloadBase is where the download links point: [asc] DOWNLOAD_BASE_URL, or this hub's own mount.
func ascDownloadBase() string {
	if setting.Asc.DownloadBaseURL != "" {
		return setting.Asc.DownloadBaseURL
	}
	return setting.AppURL + "-/download/"
}

var (
	ascDownloadLogMu  sync.Mutex
	ascDownloadLogged string
)

// logAscDownloadError says once per distinct problem that the manifest is unusable.
func logAscDownloadError(err error) {
	if err == nil || ascdownload.IsNoRelease(err) {
		return
	}
	ascDownloadLogMu.Lock()
	defer ascDownloadLogMu.Unlock()
	if ascDownloadLogged != err.Error() {
		ascDownloadLogged = err.Error()
		log.Warn("asc download: the release manifest is not usable and the download section is hidden: %v", err)
	}
}

// prepareAscDownload puts the download section into the page data. A visitor gets it only when a valid
// release is published; an administrator who gets nothing is told why, in plain words, and nobody else is.
// It reports whether there is a section to show.
func prepareAscDownload(ctx *context.Context) bool {
	// the hub's own address, for commands that name it; it is never a fixed host
	ctx.Data["AscHubURL"] = setting.AppURL[:len(setting.AppURL)-1]
	m, err := ascdownload.Cached(setting.Asc.DownloadDir, setting.Asc.DownloadBaseURL == "")
	isAdmin := ctx.IsSigned && ctx.Doer != nil && ctx.Doer.IsAdmin
	if err != nil {
		logAscDownloadError(err)
		if isAdmin {
			note := err.Error()
			if ascdownload.IsNoRelease(err) {
				note += ". Set [asc] DOWNLOAD_DIR to a directory with a manifest.json and the release files, or [asc] DOWNLOAD_SOURCE_URL to fetch a signed release into it, to offer the asc download here."
			}
			if _, syncErr, configured := ascdownloadsync.Status(); configured && syncErr != nil {
				note += " The last fetch from [asc] DOWNLOAD_SOURCE_URL failed: " + syncErr.Error()
			}
			ctx.Data["AscDownloadAdminNote"] = note
		}
		return false
	}
	hub := setting.AppURL
	offers := ascdownload.BuildOffers(m, ascDownloadBase(), hub)
	if len(offers.OSes) == 0 {
		return false
	}
	platform := ascdownload.DetectPlatform(ctx.Req.UserAgent())
	view := &ascDownloadView{Offers: offers, HubURL: hub[:len(hub)-1]}
	osOffer, archOffer, ok := offers.Pick(platform)
	if !ok {
		osOffer, archOffer = &offers.OSes[0], &offers.OSes[0].Arches[0]
	}
	view.ActiveOS, view.ActiveArch, view.Detected = osOffer.OS, archOffer.Arch, ok
	view.ActiveOSOffer, view.ActiveArchOffer = osOffer, archOffer
	ctx.Data["AscDownload"] = view
	ctx.Data["AscInstallMethods"] = ascInstallMethods(offers)
	return true
}

// Download renders /download. Without a usable release it says so, in plain words, and shows how to build asc from
// source and how to connect it to this hub; an administrator also sees what to configure. The landing page and the
// header offer no link to it then, and no file under /-/download/ is served.
func Download(ctx *context.Context) {
	shown := prepareAscDownload(ctx)
	if !shown {
		ctx.Data["AscNoRelease"] = true
		ctx.Data["AscConnectCommand"] = ascdownload.ConnectCommand(setting.AppURL)
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_download.title")
	ctx.Data["PageIsAscDownload"] = true
	ctx.Data["OpenGraphDescription"] = ctx.Locale.TrString("asc_download.lead")
	ctx.HTML(http.StatusOK, tplAscDownload)
}

// DownloadFile serves one file of the download directory under /-/download/. Only the manifest, the files
// it lists and their signature and checksum files are reachable, and the directory is opened as a root so
// that no link inside it leads out. Every installer is sent as an attachment.
func DownloadFile(ctx *context.Context) {
	name := ctx.Params("*")
	m, err := ascdownload.Cached(setting.Asc.DownloadDir, setting.Asc.DownloadBaseURL == "")
	if err != nil || !m.ServedNames()[name] {
		ctx.NotFound("ascdownload", nil)
		return
	}
	root, err := os.OpenRoot(setting.Asc.DownloadDir)
	if err != nil {
		ctx.NotFound("ascdownload", nil)
		return
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		ctx.NotFound("ascdownload", nil)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		ctx.NotFound("ascdownload", nil)
		return
	}
	opts := &context.ServeHeaderOptions{
		Filename:      name,
		LastModified:  st.ModTime(),
		CacheDuration: 5 * time.Minute,
	}
	if name == ascdownload.ManifestName {
		opts.ContentType = "application/json"
		opts.ContentTypeCharset = "utf-8"
		opts.Disposition = "inline"
	}
	ctx.ServeContent(f, opts)
}

// ascInstallMethod is one way to install asc that the landing page offers as a pill: a package manager of the
// manifest, the install script, or the PowerShell command of the Windows build.
type ascInstallMethod struct {
	Key     string
	Label   string
	Command string
}

// ascInstallMethods lists the install methods the release really has, in the order of the landing page: package
// managers first, then the script, then PowerShell. A method the manifest does not carry is not listed.
func ascInstallMethods(o *ascdownload.Offers) []ascInstallMethod {
	var out []ascInstallMethod
	seen := map[string]bool{}
	add := func(key, label, command string) {
		if command != "" && !seen[key] {
			seen[key] = true
			out = append(out, ascInstallMethod{Key: key, Label: label, Command: command})
		}
	}
	titled := func(title string) (string, bool) {
		for _, os := range o.OSes {
			for _, c := range os.PackageCommands {
				if c.Title == title {
					return c.Text, true
				}
			}
		}
		return "", false
	}
	for _, title := range []string{"Homebrew", "Scoop", "winget"} {
		if text, ok := titled(title); ok {
			add(strings.ToLower(title), title, text)
		}
	}
	for _, os := range o.OSes {
		for _, arch := range os.Arches {
			for _, f := range arch.Files {
				if f.Kind == ascdownload.KindScript && os.OS != ascdownload.OSWindows {
					add("curl", "curl", "curl -fsSL "+f.URL+" | sh")
				}
			}
			if os.OS == ascdownload.OSWindows && len(arch.Commands) > 0 {
				add("powershell", "PowerShell", arch.Commands[0].Text)
			}
		}
	}
	return out
}
