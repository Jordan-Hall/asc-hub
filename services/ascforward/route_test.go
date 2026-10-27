// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"forgejo.org/modules/web"

	"github.com/stretchr/testify/assert"
)

// The pattern the hub registers (routers/init.go) matches the paths of the protocol and nothing else, next to a
// catch-all mount that stands for the web interface.
func TestTheRoutePatternOfTheHub(t *testing.T) {
	forwarded := 0
	r := web.NewRoute()
	r.Post("/asc.v1.{service}/{method}", func(w http.ResponseWriter, _ *http.Request) { forwarded++; w.WriteHeader(http.StatusAccepted) })
	rest := web.NewRoute()
	rest.Get("/*", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rest.Post("/*", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.Mount("/", rest)

	do := func(method, path string) int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec.Code
	}
	assert.Equal(t, http.StatusAccepted, do(http.MethodPost, "/asc.v1.Repo/ListRefs"))
	assert.Equal(t, http.StatusAccepted, do(http.MethodPost, "/asc.v1.Changes/RestackChanges"))
	assert.Equal(t, 1+1, forwarded)
	assert.Equal(t, http.StatusTeapot, do(http.MethodGet, "/asc.v1.Repo/ListRefs"), "a GET is the web interface's")
	assert.Equal(t, http.StatusTeapot, do(http.MethodPost, "/asc.v1.Repo"), "no method, not a call")
	assert.Equal(t, http.StatusTeapot, do(http.MethodPost, "/user2/repo1/asc.v1.x/y"))
	assert.Equal(t, 2, forwarded)
}
