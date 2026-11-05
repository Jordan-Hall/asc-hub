// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net"
	"net/url"
	"slices"

	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// AscAboutCookie is one cookie the hub sets, as the Cookies section of the About page lists it.
type AscAboutCookie struct {
	Name    string
	Purpose string // locale key
	Life    string // locale key
}

// AscAboutService is one outside service this hub hands data to, read from its configuration.
type AscAboutService struct {
	Kind  string // locale key
	Where string // host name, or empty for "on this server"
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

// storageHost is the host of an S3-compatible store, or "" for storage on this server.
func storageHost(s *setting.Storage) string {
	if s == nil || s.Type != setting.MinioStorageType {
		return ""
	}
	host := s.MinioConfig.Endpoint
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

// ascAboutServices lists the outside services of this hub: the object stores and the mail server it is configured
// with. AI providers are listed apart: only the ones a person adds a key for are used.
func ascAboutServices() []AscAboutService {
	var hosts []string
	for _, s := range []*setting.Storage{setting.Attachment.Storage, setting.LFS.Storage, setting.Packages.Storage, setting.Actions.LogStorage, setting.Actions.ArtifactStorage, setting.RepoArchive.Storage} {
		if h := storageHost(s); h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	var out []AscAboutService
	for _, h := range hosts {
		out = append(out, AscAboutService{Kind: "asc_site.about_sp_storage", Where: h})
	}
	if len(hosts) == 0 {
		out = append(out, AscAboutService{Kind: "asc_site.about_sp_storage"})
	}
	if m := setting.MailService; m != nil {
		where := m.SMTPAddr
		if m.Protocol == "sendmail" || m.Protocol == "dummy" {
			where = ""
		}
		out = append(out, AscAboutService{Kind: "asc_site.about_sp_mail", Where: where})
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
