// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/url"

	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// AscAboutCookie is one cookie the hub sets, as the Cookies section of the About page lists it.
type AscAboutCookie struct {
	Name    string
	Purpose string // locale key
	Life    string // locale key
}

// AscAboutService is one kind of outside service this hub hands data to. Name and Purpose are the operator's
// words from [asc] SUBPROCESSORS; Kind is a locale key for what the configuration says, without any host.
type AscAboutService struct {
	Kind    string
	Here    bool
	Name    string
	Purpose string
}

// ascAboutCookies lists every cookie the hub sets, with the names this hub is configured with.
func ascAboutCookies() []AscAboutCookie {
	return []AscAboutCookie{
		{setting.SessionConfig.CookieName, "asc_site.about_cookie_session", "asc_site.about_cookie_life_session"},
		{setting.CookieRememberName, "asc_site.about_cookie_remember", "asc_site.about_cookie_life_remember"},
		{"lang", "asc_site.about_cookie_lang", "asc_site.about_cookie_life_lang"},
		{"redirect_to", "asc_site.about_cookie_redirect", "asc_site.about_cookie_life_session"},
		{"flash", "asc_site.about_cookie_flash", "asc_site.about_cookie_life_session"},
		{"asc_clone_hidden_<id>", "asc_site.about_cookie_clone", "asc_site.about_cookie_life_year"},
	}
}

// ascAboutServices lists what goes outside the hub: the services the operator named, then what the configuration
// says (object storage on this server or in an S3-compatible store, a mail server). Hosts and addresses of the
// configuration are never shown: the page is public. AI providers are listed apart.
func ascAboutServices() []AscAboutService {
	var out []AscAboutService
	for _, sp := range setting.Asc.Subprocessors {
		out = append(out, AscAboutService{Name: sp.Name, Purpose: sp.Purpose})
	}
	external := false
	for _, s := range []*setting.Storage{setting.Attachment.Storage, setting.LFS.Storage, setting.Packages.Storage, setting.Actions.LogStorage, setting.Actions.ArtifactStorage, setting.RepoArchive.Storage} {
		if s != nil && s.Type == setting.MinioStorageType {
			external = true
		}
	}
	out = append(out, AscAboutService{Kind: "asc_site.about_sp_storage", Here: !external})
	if m := setting.MailService; m != nil && m.Protocol != "dummy" {
		out = append(out, AscAboutService{Kind: "asc_site.about_sp_mail", Here: m.Protocol == "sendmail" || m.Protocol == "smtp+unix"})
	}
	return out
}

// ascAboutData fills the contact, security, legal, subprocessor and cookie sections of the About page.
func ascAboutData(ctx *context.Context) {
	ctx.Data["AboutContactEmail"] = setting.Asc.ContactEmail
	ctx.Data["AboutSecurityEmail"] = setting.Asc.SecurityEmail
	ctx.Data["AboutPoliciesURL"] = setting.Asc.PoliciesURL
	ctx.Data["AboutCookies"] = ascAboutCookies()
	ctx.Data["AboutServices"] = ascAboutServices()
	host := setting.AppURL
	if u, err := url.Parse(setting.AppURL); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	ctx.Data["AboutHost"] = host
}
