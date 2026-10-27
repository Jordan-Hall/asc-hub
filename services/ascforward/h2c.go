// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"net/http"
	"strings"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// PriorKnowledgeH2C lets the hub take the gRPC calls of the asc command line as HTTP/2 without TLS, which is what a
// TLS proxy in front of the hub sends when it passes them on. It is deliberately narrow:
//
//   - Only prior-knowledge HTTP/2 is taken. A request that asks for the HTTP/1.1 upgrade to h2c (Upgrade: h2c, or the
//     HTTP2-Settings header) is refused with 400, on every route. The upgrade is the way to smuggle requests past a
//     proxy that does not look inside the connection it upgraded; nothing here needs it.
//   - An HTTP/2 connection without TLS serves the asc API calls and nothing else: POST /asc.v1.* with a gRPC or
//     gRPC-Web content type. Any other request on it is answered 421, so the web interface, the API and git are
//     reached only over HTTP/1.1 or over TLS, where the proxy in front has looked at them.
//
// Everything on HTTP/1.1 reaches next as before.
func PriorKnowledgeH2C(next http.Handler) http.Handler {
	asc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && r.TLS == nil && !isAscCall(r) {
			http.Error(w, "this connection serves the asc API only", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
	h2 := h2c.NewHandler(asc, &http2.Server{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if asksForUpgrade(r) {
			http.Error(w, "the upgrade to HTTP/2 is not supported", http.StatusBadRequest)
			return
		}
		h2.ServeHTTP(w, r)
	})
}

func isAscCall(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/asc.v1.") && isGRPC(r.Header.Get("Content-Type"))
}

// asksForUpgrade reports whether the request tries the HTTP/1.1 upgrade to h2c, in any spelling of the headers.
func asksForUpgrade(r *http.Request) bool {
	if len(r.Header.Values("Http2-Settings")) > 0 {
		return true
	}
	for _, v := range r.Header.Values("Upgrade") {
		if strings.Contains(strings.ToLower(v), "h2c") {
			return true
		}
	}
	return false
}
