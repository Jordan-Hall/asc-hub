// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"strings"

	"forgejo.org/modules/setting"
	"forgejo.org/services/ascforward"
	"forgejo.org/services/context"
)

// ascWellKnown is the discovery document of `GET /.well-known/asc`.
type ascWellKnown struct {
	Server string `json:"server"`
	Repos  string `json:"repos"`
	// Seeds is the base of the seed snapshots (<seeds>/<owner>/<repo>/latest), only when asc serve publishes them.
	Seeds string `json:"seeds,omitempty"`
}

// AscWellKnown tells asc clients where `asc serve` lives: [asc] PUBLIC_URL, or SERVER_URL when that is not
// set. It answers 404 unless `[asc] ENABLED` is true. The document is public and holds no secret. It names
// <server>/seeds when asc serve publishes seed snapshots: the hub forwards that route to asc, so a clone through the hub
// starts from a seed as one from asc serve does.
func AscWellKnown(ctx *context.Context) {
	server := setting.AscPublicURL()
	if !setting.Asc.Enabled || server == "" {
		ctx.Error(http.StatusNotFound)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "public, max-age=300")
	doc := ascWellKnown{Server: server, Repos: "*"}
	if ascforward.Upstream.Seeds(ctx) {
		doc.Seeds = strings.TrimRight(server, "/") + "/seeds"
	}
	ctx.JSON(http.StatusOK, doc)
}
