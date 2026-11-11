// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/web"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// seen is what the fake asc serve received.
type seen struct {
	mu   sync.Mutex
	reqs []*http.Request
	body [][]byte
}

func (s *seen) last() (*http.Request, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return nil, nil
	}
	return s.reqs[len(s.reqs)-1], s.body[len(s.body)-1]
}

func (s *seen) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// cacheRig is the hub (an HTTP/1.1 server around the cache forwarder, as behind a TLS proxy) and a fake asc serve
// that speaks h2c, as asc serve does on loopback.
func cacheRig(t *testing.T, gate func(*http.Request, bool) string) (string, *seen) {
	t.Helper()
	got := &seen{}
	asc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.mu.Lock()
		got.reqs = append(got.reqs, r)
		got.body = append(got.body, b)
		got.mu.Unlock()
		w.Header().Set("Set-Cookie", "s=1")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/zstd")
		if r.URL.Path == "/v8/asc/cas/missing" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"missing":["ab"]}`))
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			return
		}
		_, _ = w.Write([]byte("zstd bytes"))
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &http.Server{Handler: h2c.NewHandler(asc, &http2.Server{})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })

	fwd, err := NewCache("http://"+l.Addr().String(), nil)
	require.NoError(t, err)
	fwd.Gate = gate
	r := web.NewRoute()
	for _, p := range CachePatterns {
		r.Any(p, fwd.ServeHTTP)
	}
	rest := web.NewRoute()
	rest.Any("/*", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.Mount("/", rest)
	hub := httptest.NewServer(r)
	t.Cleanup(hub.Close)
	return hub.URL, got
}

func send(t *testing.T, method, url string, body []byte, header map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	require.NoError(t, err)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

const fileID = "1f0e4a9c2b7d3e5f60718293a4b5c6d7e8f90123456789abcdef0123456789ab"

func TestOneCacheFileGoesThroughTheHubWithItsQuery(t *testing.T) {
	url, got := cacheRig(t, nil)
	resp := send(t, http.MethodPut, url+"/v8/asc/cas/"+fileID+"?slug=acme.web", []byte("compressed"), map[string]string{
		"Authorization": "Bearer the-clients-token",
	})
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	req, body := got.last()
	require.NotNil(t, req)
	assert.Equal(t, "/v8/asc/cas/"+fileID, req.URL.Path)
	assert.Equal(t, "slug=acme.web", req.URL.RawQuery)
	assert.Equal(t, "Bearer the-clients-token", req.Header.Get("Authorization"), "the client's credential, unchanged")
	assert.Equal(t, []byte("compressed"), body)

	resp = send(t, http.MethodGet, url+"/v8/asc/cas/"+fileID+"?slug=acme.web", nil, nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "zstd bytes", string(b))
	assert.Empty(t, resp.Header.Get("Set-Cookie"))
	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))

	resp = send(t, http.MethodPost, url+"/v8/asc/cas/missing?slug=acme.web", []byte(`{"ids":["ab"]}`), nil)
	b, _ = io.ReadAll(resp.Body)
	assert.JSONEq(t, `{"missing":["ab"]}`, string(b))
	for _, p := range []string{
		"/v8/artifacts/status", "/v8/asc/build-revs/" + strings.Repeat("a", 40), "/v8/asc/ac/" + fileID,
		"/actions/v1/acme/web/cache/linux-%2Fdeps", "/actions/v1/acme/web/cache",
	} {
		assert.Equal(t, http.StatusOK, send(t, http.MethodGet, url+p, nil, nil).StatusCode, p)
	}
}

func TestOnlyTheCacheRoutesAndTheirMethodsAreForwarded(t *testing.T) {
	url, got := cacheRig(t, nil)
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPut, "/v8/asc/cas/missing", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/v8/asc/cas/" + fileID, http.StatusMethodNotAllowed},
		{http.MethodPost, "/v8/artifacts/status", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v8/teams", http.StatusNotFound},
		{http.MethodGet, "/v8/asc/cas/..", http.StatusNotFound},
		{http.MethodGet, "/v8/asc/cas/a/b", http.StatusNotFound},
		{http.MethodGet, "/actions/v1/acme/web", http.StatusNotFound},
		{http.MethodGet, "/v8/artifacts/x?" + strings.Repeat("q", MaxCacheQuery+1), http.StatusNotFound},
	} {
		assert.Equal(t, c.want, send(t, c.method, url+c.path, nil, nil).StatusCode, "%s %s", c.method, c.path)
	}
	assert.Equal(t, 0, got.count(), "nothing refused reaches asc")
	assert.Equal(t, http.StatusTeapot, send(t, http.MethodGet, url+"/user2/repo1", nil, nil).StatusCode, "the web interface keeps the rest")
}

func TestTheCacheRequestCarriesNoClaimAndNoCookie(t *testing.T) {
	url, got := cacheRig(t, nil)
	send(t, http.MethodGet, url+"/v8/asc/ac/"+fileID, nil, map[string]string{
		"Cookie":                "i_like_gitea=1",
		"X-Asc-Forwarded-Claim": "forged",
		"X-Asc-Hub-Subject":     "admin",
		"Origin":                "https://evil.example",
		"X-Forwarded-For":       "10.0.0.1",
	})
	req, _ := got.last()
	require.NotNil(t, req)
	for _, h := range []string{"Cookie", "X-Asc-Forwarded-Claim", "X-Asc-Hub-Subject", "Origin"} {
		assert.Empty(t, req.Header.Get(h), h)
	}
	assert.Equal(t, "127.0.0.1", req.Header.Get("X-Forwarded-For"), "the address the hub saw")
}

func TestACacheKeyIsOneNameOfSafeBytes(t *testing.T) {
	for _, ok := range []string{"abc", "a.b_c-d", "linux-%2Fdeps", strings.Repeat("a", 200)} {
		assert.True(t, segment(ok), ok)
	}
	for _, bad := range []string{"", ".", "..", "a%zz", "a%2", "a b", "a/b", "a;b", strings.Repeat("a", 201)} {
		assert.False(t, segment(bad), bad)
	}
}

func TestAnEscapedSlashStaysInTheKey(t *testing.T) {
	url, got := cacheRig(t, nil)
	assert.Equal(t, http.StatusOK, send(t, http.MethodGet, url+"/actions/v1/acme/web/cache/linux-%2Fdeps", nil, nil).StatusCode)
	req, _ := got.last()
	require.NotNil(t, req)
	assert.Equal(t, "/actions/v1/acme/web/cache/linux-%2Fdeps", req.URL.EscapedPath())
}

func TestATooLargeCacheRequestIsRefusedBeforeAsc(t *testing.T) {
	url, got := cacheRig(t, nil)
	big := make([]byte, MaxCacheRequest+1)
	resp := send(t, http.MethodPost, url+"/v8/asc/cas/missing", big, nil)
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	assert.Equal(t, 0, got.count())
}

func TestTheGateDecidesBeforeAscIsAsked(t *testing.T) {
	var writes []bool
	url, got := cacheRig(t, func(r *http.Request, write bool) string {
		writes = append(writes, write)
		if write {
			return "this token has no write:repository scope"
		}
		return ""
	})
	assert.Equal(t, http.StatusOK, send(t, http.MethodHead, url+"/v8/asc/cas/"+fileID, nil, nil).StatusCode)
	resp := send(t, http.MethodPut, url+"/v8/asc/cas/"+fileID, []byte("x"), nil)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(b), "write:repository")
	assert.Equal(t, []bool{false, true}, writes)
	assert.Equal(t, 1, got.count())
}

func TestAnUnreachableAscIsABadGatewayWithoutDetails(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	fwd, err := NewCache("http://"+addr, nil)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	fwd.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v8/asc/ac/"+fileID, nil))
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.NotContains(t, rec.Body.String(), addr)
}

func TestCacheScopes(t *testing.T) {
	read := auth_model.AccessTokenScope("read:repository")
	write := auth_model.AccessTokenScope("write:repository")
	assert.Empty(t, cacheScopeRefusal(read, false))
	assert.Equal(t, "this token has no write:repository scope", cacheScopeRefusal(read, true))
	assert.Empty(t, cacheScopeRefusal(write, true))
	assert.NotEmpty(t, cacheScopeRefusal(auth_model.AccessTokenScope("read:user"), false))
}
