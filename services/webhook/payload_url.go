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

// ErrPayloadURLNotHTTPS is returned for a payload address that is not https and is not a private or loopback target
// the instance lets webhooks reach.
var ErrPayloadURLNotHTTPS = errors.New("the payload address must be https; http is accepted only for a loopback or private address that [webhook] ALLOWED_HOST_LIST allows")

// CheckPayloadURL applies the transport rule of an ASC hub to the address of a webhook: https always, http only for
// a loopback or private literal address (or localhost) that the instance's ALLOWED_HOST_LIST lets webhooks reach.
// The rule is on when [asc] is enabled; an instance without asc keeps what Forgejo accepts. Delivery checks the list
// again when it connects, so this check only decides what a person may save.
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
	if u.Scheme != "http" {
		return ErrPayloadURLNotHTTPS
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	local := host == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsPrivate()))
	if !local {
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
