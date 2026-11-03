// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"forgejo.org/modules/hostmatcher"
	"forgejo.org/modules/setting"
)

// ErrPayloadURLNotHTTPS is returned for a payload address that is not https and is not a loopback target the instance
// opted into.
var ErrPayloadURLNotHTTPS = errors.New("the payload address must be https; http is accepted only for localhost when [webhook] ALLOW_LOCAL_HTTP is on and ALLOWED_HOST_LIST allows loopback")

// CheckPayloadURL applies the transport rule of an ASC hub to the address of a webhook: https always, http only for
// a loopback address (localhost, 127.0.0.0/8, ::1) when the instance turned on [webhook] ALLOW_LOCAL_HTTP and its
// ALLOWED_HOST_LIST lets webhooks reach it. The rule is on when [asc] is enabled; an instance without asc keeps what
// Forgejo accepts. Delivery checks the list again when it connects, so this check only decides what a person may save.
func CheckPayloadURL(raw string) error {
	if !setting.Asc.Enabled {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil // the form's own URL validation answers for an address that is not one
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" || !setting.Webhook.AllowLocalHTTP {
		return ErrPayloadURLNotHTTPS
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host == "localhost" {
		ip = net.IPv4(127, 0, 0, 1) // the name resolves to loopback; the list is checked as for that address
	}
	if ip == nil || !ip.IsLoopback() {
		return ErrPayloadURLNotHTTPS
	}
	list := setting.Webhook.AllowedHostList
	if list == "" {
		list = hostmatcher.MatchBuiltinExternal
	}
	if !hostmatcher.ParseHostMatchList("webhook.ALLOWED_HOST_LIST", list).MatchHostOrIP(host, ip) {
		return ErrPayloadURLNotHTTPS
	}
	return nil
}
