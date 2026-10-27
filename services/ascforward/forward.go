// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascforward is the reverse proxy from the hub to `asc serve` for the calls the asc command line makes.
//
// The hub is the only public address of an ASC Hub (docs/asc/OPERATIONS.md): /.well-known/asc announces it as the
// asc server, so `asc clone https://hub/owner/repo` sends its gRPC calls to the hub, to paths like
// POST /asc.v1.Repo/ListRefs. This package answers them by forwarding the call to asc serve, which decides, as it
// does for every client, with the credential that came with the call (it asks the hub's authz endpoint about it).
//
// # What is forwarded
//
// Exactly POST /asc.v1.<Service>/<Method> for a service and method of the allowlist (see Allowed and
// docs/asc/OPERATIONS.md, "The asc API through the hub"): the services a client of asc uses, minus the methods that
// are the hub's own to call (RepoAdmin, ImportGitPush) and the services that hold keys or are local to a machine.
// Every other path, every other method and every other content type is refused before asc is asked. The list is
// checked against the compiled protocol by a test, so a service added later is refused until someone allows it.
//
// # How
//
//   - The hub adds no credential. The Authorization header of the client goes on unchanged; the internal token of the
//     hub is never used, so the proxy cannot be used to act as the hub. A client cannot make the hub vouch for it:
//     the headers of the claims a hub signs (x-asc-forwarded-*, x-asc-hub-*) are removed from the request.
//   - Cookies, hop-by-hop headers, X-Forwarded-*, Forwarded, Origin and Referer are removed; X-Forwarded-For is set
//     to the address the hub saw. Set-Cookie and every CORS header are removed from the answer, and none is added:
//     a browser page of another origin cannot use this path.
//   - gRPC and gRPC-Web are streamed both ways with the trailers (a stream is flushed at once). The call is bounded
//     by a body size, a time and a number of calls in flight, per kind of method.
//   - A refusal or a failure that is the proxy's own is a gRPC answer (HTTP 200 with grpc-status) when the request
//     was gRPC, so a client sees a status and not a transport error. Nothing of the upstream address is revealed.
package ascforward

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "forgejo.org/modules/ascproto/ascv1" // registers the asc.v1 descriptors the allowlist is checked against
	"forgejo.org/modules/log"

	"golang.org/x/net/http2"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Named limits.
const (
	// MaxUnaryBody bounds the request of a call with one request message: asc takes a message of 17 MiB.
	MaxUnaryBody = 20 << 20
	// MaxStreamBody bounds the request of a call that streams messages up (an upload of objects).
	MaxStreamBody = int64(4) << 30
	// UnaryTimeout bounds a call with one request and one answer.
	UnaryTimeout = 10 * time.Minute
	// StreamTimeout bounds a streaming call from start to end.
	StreamTimeout = time.Hour
	// MaxInFlight bounds the calls forwarded at once.
	MaxInFlight = 512
	// MaxHeaderBytes bounds the headers of a forwarded request.
	MaxHeaderBytes = 64 << 10
	// MaxPathBytes bounds the path.
	MaxPathBytes = 256
)

// gRPC status codes the proxy answers with.
const (
	codeResourceExhausted = 8
	codeUnimplemented     = 12
	codeUnavailable       = 14
	codeDeadlineExceeded  = 4
)

// services lists the services of asc.v1 that are forwarded. The rest are the hub's alone or local to a machine:
//
//	Workspace   IPC between the asc client and its own daemon; it has no meaning on a server
//	Bridge      the hub's bridge to its git directories; called with the hub's service credential only
//	Vault       key material
//	Storage     storage targets of a user or an organisation: set on the hub's settings pages
//	AiProviders AI provider keys: set on the hub's settings pages
var services = map[string]bool{
	"Repo": true, "Changes": true, "Stacks": true, "Review": true, "Cloud": true, "Projects": true, "CodeIntel": true,
	"CrossSearch": true, "Inbox": true, "MergeQueue": true, "Policy": true, "Workflows": true, "Agents": true,
	"AgentTokens": true, "Ci": true, "CiInsights": true, "SecretScan": true, "Signing": true, "Audit": true, "DepsService": true,
}

// DeniedServices are the services that are never forwarded, and why.
var DeniedServices = map[string]string{
	"Workspace": "local to a machine", "Bridge": "the hub's own", "Vault": "key material",
	"Storage": "set on the hub's pages", "AiProviders": "set on the hub's pages",
}

// DeniedMethods are the methods of a forwarded service that are not forwarded, and why.
var DeniedMethods = map[string]string{
	"asc.v1.Repo/RepoAdmin":     "repairs and renames are the hub's to ask for, with a claim only the hub can sign",
	"asc.v1.Repo/ImportGitPush": "names directories of the machine asc serve runs on; the hub's pre-receive hook calls it",
}

// Allowed reports whether POST /asc.v1.<service>/<method> is forwarded.
func Allowed(service, method string) bool {
	if !services[service] {
		return false
	}
	if _, denied := DeniedMethods["asc.v1."+service+"/"+method]; denied {
		return false
	}
	_, ok := lookup(service, method)
	return ok
}

func lookup(service, method string) (protoreflect.MethodDescriptor, bool) {
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName("asc.v1." + service))
	if err != nil {
		return nil, false
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, false
	}
	md := sd.Methods().ByName(protoreflect.Name(method))
	return md, md != nil
}

// Forwarder is the handler.
type Forwarder struct {
	upstream *url.URL
	proxy    *httputil.ReverseProxy
	sem      chan struct{}
}

// New returns a forwarder to the asc serve at upstream: an http address (HTTP/2 without TLS, which is what asc serve
// speaks on loopback) or an https one. client is the HTTP/2 transport; nil makes one.
func New(upstream string, tr http.RoundTripper) (*Forwarder, error) {
	u, err := url.Parse(upstream)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("ascforward: the asc server address must be an http or https address without credentials, query or fragment")
	}
	if tr == nil {
		tr = newTransport(u)
	}
	f := &Forwarder{upstream: u, sem: make(chan struct{}, MaxInFlight)}
	f.proxy = &httputil.ReverseProxy{
		Rewrite:       f.rewrite,
		Transport:     tr,
		FlushInterval: -1,
		ErrorHandler:  f.upstreamError,
		ModifyResponse: func(resp *http.Response) error {
			stripResponse(resp.Header)
			return nil
		},
	}
	return f, nil
}

func newTransport(u *url.URL) http.RoundTripper {
	t := &http2.Transport{
		ReadIdleTimeout:    30 * time.Second,
		PingTimeout:        15 * time.Second,
		DisableCompression: true, // gRPC compresses its messages itself; the bytes go through as they are
	}
	if u.Scheme == "http" {
		t.AllowHTTP = true
		t.DialTLSContext = func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}
	}
	return t
}

var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Trailer",
	"Transfer-Encoding", "Upgrade",
}

// removedFromRequest are the headers that never reach asc serve, beside the hop-by-hop ones.
var removedFromRequest = []string{
	"Cookie", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port",
	"X-Forwarded-Server", "X-Real-Ip", "Origin", "Referer", "X-Original-Forwarded-For",
}

// removedPrefixes are the families of headers a client cannot send: the claims that only the hub signs.
var removedPrefixes = []string{"X-Asc-Forwarded-", "X-Asc-Hub-"}

func (f *Forwarder) rewrite(r *httputil.ProxyRequest) {
	r.SetURL(f.upstream)
	// SetURL joins the paths; the call goes to the same path on asc serve, which is the whole path of the request.
	r.Out.URL.Path, r.Out.URL.RawPath, r.Out.URL.RawQuery = r.In.URL.Path, "", ""
	r.Out.Host = f.upstream.Host
	h := r.Out.Header
	for _, k := range hopByHop {
		h.Del(k)
	}
	for _, k := range removedFromRequest {
		h.Del(k)
	}
	for k := range h {
		for _, p := range removedPrefixes {
			if strings.HasPrefix(http.CanonicalHeaderKey(k), p) {
				delete(h, k)
			}
		}
	}
	// gRPC needs "te: trailers"; ReverseProxy keeps it when the client sent it.
	if host, _, err := net.SplitHostPort(r.In.RemoteAddr); err == nil {
		h.Set("X-Forwarded-For", host)
	}
	r.Out.Header.Set("X-Forwarded-Proto", proto(r.In))
}

func proto(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func stripResponse(h http.Header) {
	h.Del("Set-Cookie")
	for k := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(k), "Access-Control-") {
			delete(h, k)
		}
	}
}

// ServeHTTP implements http.Handler.
func (f *Forwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	grpc := isGRPC(r.Header.Get("Content-Type"))
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	service, method, ok := splitPath(r.URL.Path)
	if !ok || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	if !grpc {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	if headerBytes(r.Header) > MaxHeaderBytes {
		refuse(w, codeResourceExhausted, "the request headers are too large")
		return
	}
	if !Allowed(service, method) {
		refuse(w, codeUnimplemented, "this call is not available through the hub")
		return
	}
	md, _ := lookup(service, method)
	streaming := md.IsStreamingClient() || md.IsStreamingServer()
	limit, timeout := int64(MaxUnaryBody), UnaryTimeout
	if md.IsStreamingClient() {
		limit = MaxStreamBody
	}
	if streaming {
		timeout = StreamTimeout
	}
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	default:
		w.Header().Set("Retry-After", "5")
		refuse(w, codeUnavailable, "the hub is busy; try again")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	f.proxy.ServeHTTP(w, r)
}

// splitPath reads "/asc.v1.<Service>/<Method>" and nothing else.
func splitPath(p string) (service, method string, ok bool) {
	if len(p) > MaxPathBytes || !strings.HasPrefix(p, "/asc.v1.") {
		return "", "", false
	}
	rest := strings.TrimPrefix(p, "/asc.v1.")
	service, method, found := strings.Cut(rest, "/")
	if !found || !identifier(service) || !identifier(method) {
		return "", "", false
	}
	return service, method, true
}

func identifier(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func isGRPC(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	switch ct {
	case "application/grpc", "application/grpc+proto", "application/grpc-web", "application/grpc-web+proto", "application/grpc-web-text", "application/grpc-web-text+proto":
		return true
	}
	return false
}

func headerBytes(h http.Header) int {
	n := 0
	for k, vv := range h {
		for _, v := range vv {
			n += len(k) + len(v) + 4
		}
	}
	return n
}

// refuse answers a gRPC call with a status and no message, as a "trailers only" response.
func refuse(w http.ResponseWriter, code int, message string) {
	h := w.Header()
	h.Set("Content-Type", "application/grpc")
	h.Set("Grpc-Status", strconv.Itoa(code))
	h.Set("Grpc-Message", grpcMessage(message))
	w.WriteHeader(http.StatusOK)
}

// grpcMessage percent-encodes a status message as gRPC wants.
func grpcMessage(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if c >= 0x20 && c <= 0x7e && c != '%' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func (f *Forwarder) upstreamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		refuse(w, codeDeadlineExceeded, "the call took too long")
	case errors.Is(err, context.Canceled):
		// the client left
	default:
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			refuse(w, codeResourceExhausted, "the request is too large")
			return
		}
		log.Warn("asc forward: %s %s: %v", r.Method, r.URL.Path, err)
		refuse(w, codeUnavailable, "the asc server cannot be reached")
	}
}
