// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"

	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// ascWellKnown is the discovery document of `GET /.well-known/asc`.
type ascWellKnown struct {
	Server string `json:"server"`
	Repos  string `json:"repos"`
}

// AscWellKnown tells asc clients where `asc serve` lives: [asc] PUBLIC_URL, or SERVER_URL when that is not
// set. It answers 404 unless `[asc] ENABLED` is true. The document is public and holds no secret.
func AscWellKnown(ctx *context.Context) {
	server := setting.AscPublicURL()
	if !setting.Asc.Enabled || server == "" {
		ctx.Error(http.StatusNotFound)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "public, max-age=300")
	ctx.JSON(http.StatusOK, ascWellKnown{Server: server, Repos: "*"})
}
