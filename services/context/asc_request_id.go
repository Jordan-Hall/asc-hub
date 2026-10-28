// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// setRequestID gives the request a short id that the person can quote: it is shown on the error pages, sent as
// X-Request-Id and written next to the error in the log. An id that a proxy in front of the hub already made (one of
// the headers of [log] REQUEST_ID_HEADERS) is kept, cut to a length that fits the page.
func (ctx *Context) setRequestID() {
	id := ""
	if parsed := parseRequestIDFromRequestHeader(ctx.Req); parsed != "-" && parsed != "" {
		id = sanitizeRequestID(parsed)
	}
	if id == "" {
		var b [4]byte
		_, _ = rand.Read(b[:])
		id = hex.EncodeToString(b[:])
	}
	ctx.Data["RequestID"] = id
	ctx.Data["RequestTime"] = time.Now().UTC().Format("2006-01-02 15:04")
	ctx.Resp.Header().Set("X-Request-Id", id)
}

// sanitizeRequestID keeps letters, digits and a few separators, at most 24 characters: the id is put in a page and a log.
func sanitizeRequestID(s string) string {
	out := make([]byte, 0, 24)
	for i := 0; i < len(s) && len(out) < 24; i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			out = append(out, c)
		}
	}
	return string(out)
}

// RequestIDForLog is the id of the request, for log lines; empty when the request has none.
func (ctx *Context) RequestIDForLog() string {
	if id, ok := ctx.Data["RequestID"].(string); ok {
		return id
	}
	return ""
}
