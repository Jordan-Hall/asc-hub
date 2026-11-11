// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/log"
)

// The build cache of asc speaks plain HTTP, not gRPC: the turbo store under /v8 (artefacts, revision lists and the
// file-level routes that move one output file at a time) and the CI cache manifests under /actions/v1. The asc
// command line sends them to the same origin as its gRPC calls, which is the hub, so the hub forwards them as well,
// with the same rules: no credential of the hub, no claim headers from the client, no cookie and no CORS.

// Named limits of the cache routes.
const (
	// MaxCacheUpload bounds the body of an upload: a turbo artefact is at most 2 GiB on asc serve.
	MaxCacheUpload = int64(2)<<30 + 1<<20
	// MaxCacheRequest bounds the body of every other request (JSON lists and manifests; asc takes 32 MiB).
	MaxCacheRequest = int64(33) << 20
	// CacheTimeout bounds one request.
	CacheTimeout = 30 * time.Minute
	// MaxCacheQuery bounds the query string (the turbo routes take ?slug= and ?teamId=).
	MaxCacheQuery = 1024
)

// cacheRoute is one route of the cache: its segments ("*" is one segment of the given kind) and its methods.
type cacheRoute struct {
	segs    []string
	methods string
	upload  bool // a PUT of this route carries an artefact or a file
}

var cacheRoutes = []cacheRoute{
	{segs: []string{"v8", "artifacts", "status"}, methods: "GET"},
	{segs: []string{"v8", "artifacts", "events"}, methods: "POST"},
	{segs: []string{"v8", "artifacts"}, methods: "POST"},
	{segs: []string{"v8", "artifacts", "*"}, methods: "GET HEAD PUT", upload: true},
	{segs: []string{"v8", "asc", "artifact-paths"}, methods: "POST"},
	{segs: []string{"v8", "asc", "build-revs", "*"}, methods: "GET POST"},
	{segs: []string{"v8", "asc", "cas", "missing"}, methods: "POST"},
	{segs: []string{"v8", "asc", "cas", "*"}, methods: "GET HEAD PUT", upload: true},
	{segs: []string{"v8", "asc", "ac", "*"}, methods: "GET HEAD PUT"},
	{segs: []string{"actions", "v1", "*", "*", "*", "*"}, methods: "GET PUT"},
	{segs: []string{"actions", "v1", "*", "*", "*"}, methods: "GET"},
}

// matchCache finds the route of a path: whether it is one, whether the method is one of its, and whether a PUT of it
// is an upload. The first route whose segments match decides, so a literal route listed before a parameter of the
// same place wins (PUT /v8/asc/cas/missing is refused, not taken for a file). A parameter is a name: letters, digits,
// '.', '_', '-', and the percent escapes of a cache key.
func matchCache(method, path string) (known, allowed, upload bool) {
	if len(path) > MaxPathBytes || !strings.HasPrefix(path, "/") {
		return false, false, false
	}
	segs := strings.Split(path[1:], "/")
	for _, rt := range cacheRoutes {
		if !rt.matches(segs) {
			continue
		}
		for _, m := range strings.Fields(rt.methods) {
			if m == method {
				return true, true, rt.upload && method == http.MethodPut
			}
		}
		return true, false, false
	}
	return false, false, false
}

func (rt cacheRoute) matches(segs []string) bool {
	if len(rt.segs) != len(segs) {
		return false
	}
	for i, s := range rt.segs {
		if s == "*" && !segment(segs[i]) || s != "*" && s != segs[i] {
			return false
		}
	}
	return true
}

func segment(s string) bool {
	if s == "" || len(s) > 200 || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		case c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]):
			i += 2
		default:
			return false
		}
	}
	return true
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// CacheForwarder forwards the build cache routes to asc serve.
type CacheForwarder struct {
	// Gate, when set, may refuse a request before it is forwarded: it returns the reason, or "".
	Gate  func(r *http.Request, write bool) string
	proxy *httputil.ReverseProxy
	sem   chan struct{}
}

// NewCache returns a forwarder of the cache routes to the asc serve at upstream (see New).
func NewCache(upstream string, tr http.RoundTripper) (*CacheForwarder, error) {
	base, err := New(upstream, tr)
	if err != nil {
		return nil, err
	}
	f := &CacheForwarder{sem: make(chan struct{}, MaxInFlight)}
	f.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			base.rewrite(r)
			// A cache key may hold an escaped '/' (%2F), so the path goes as the client wrote it. The cache routes
			// read their query (?slug=); its length was checked in ServeHTTP.
			r.Out.URL.RawPath = r.In.URL.RawPath
			r.Out.URL.RawQuery = r.In.URL.RawQuery
		},
		Transport:     base.proxy.Transport,
		FlushInterval: -1,
		ErrorHandler:  cacheUpstreamError,
		ModifyResponse: func(resp *http.Response) error {
			stripResponse(resp.Header)
			return nil
		},
	}
	return f, nil
}

// CachePatterns are the route patterns the hub registers for the cache, each to ServeHTTP for every method.
var CachePatterns = []string{"/v8/*", "/actions/v1/*"}

// ServeHTTP implements http.Handler.
func (f *CacheForwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	known, allowed, upload := matchCache(r.Method, r.URL.EscapedPath())
	if !known || len(r.URL.RawQuery) > MaxCacheQuery {
		http.NotFound(w, r)
		return
	}
	if !allowed {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if headerBytes(r.Header) > MaxHeaderBytes {
		http.Error(w, "the request headers are too large", http.StatusRequestHeaderFieldsTooLarge)
		return
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if f.Gate != nil {
		if reason := f.Gate(r, write); reason != "" {
			http.Error(w, reason, http.StatusForbidden)
			return
		}
	}
	limit := MaxCacheRequest
	if upload {
		limit = MaxCacheUpload
	}
	if r.ContentLength > limit {
		http.Error(w, "the request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	default:
		w.Header().Set("Retry-After", "5")
		http.Error(w, "the hub is busy; try again", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), CacheTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	f.proxy.ServeHTTP(w, r)
}

func cacheUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(w, "the request took too long", http.StatusGatewayTimeout)
	case errors.Is(err, context.Canceled):
		// the client left
	default:
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "the request is too large", http.StatusRequestEntityTooLarge)
			return
		}
		log.Warn("asc cache forward: %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, "the asc server cannot be reached", http.StatusBadGateway)
	}
}

// CacheTokenGate holds a personal access token to the repository scope before a cache request is forwarded: read for
// GET and HEAD, write for the rest. Any other credential is left to asc, which asks the hub.
func CacheTokenGate(r *http.Request, write bool) string {
	token := bearerToken(r)
	if token == "" {
		return ""
	}
	t, err := auth_model.GetAccessTokenBySHA(r.Context(), token)
	if err != nil {
		if !auth_model.IsErrAccessTokenNotExist(err) && !auth_model.IsErrAccessTokenEmpty(err) {
			log.Error("asc cache forward: token lookup: %v", err)
			return "the hub could not check the token; try again"
		}
		return ""
	}
	return cacheScopeRefusal(t.Scope, write)
}

func cacheScopeRefusal(scope auth_model.AccessTokenScope, write bool) string {
	level := auth_model.Read
	if write {
		level = auth_model.Write
	}
	need := auth_model.GetRequiredScopes(level, auth_model.AccessTokenScopeCategoryRepository)
	ok, err := scope.HasScope(need...)
	if err != nil || !ok {
		return "this token has no " + string(need[0]) + " scope"
	}
	return ""
}
