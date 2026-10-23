// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"net/http"
	"net/url"
	"strings"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

const (
	// ProtectedResourcePath is the route of the RFC 9728 protected resource metadata document.
	ProtectedResourcePath = "/.well-known/oauth-protected-resource/api/mcp"

	endpointPath = "api/mcp"
)

// supportedScopes are the token scopes the tools use. They are advertised so that a client asks for them.
var supportedScopes = []auth_model.AccessTokenScope{
	auth_model.AccessTokenScopeReadRepository,
	auth_model.AccessTokenScopeWriteRepository,
	auth_model.AccessTokenScopeReadIssue,
	auth_model.AccessTokenScopeWriteIssue,
	auth_model.AccessTokenScopeReadNotification,
	auth_model.AccessTokenScopeWriteNotification,
}

// resourceURL is the canonical URL of the endpoint: the OAuth resource indicator and the audience of its tokens.
func resourceURL() string {
	return setting.AppURL + endpointPath
}

func metadataURL() string {
	return setting.AppURL + strings.TrimPrefix(ProtectedResourcePath, "/")
}

// bearerChallenge builds a WWW-Authenticate value. Every part is generated here, never taken from the request.
func bearerChallenge(errCode string, scopes []auth_model.AccessTokenScope) string {
	var b strings.Builder
	b.WriteString("Bearer ")
	if errCode != "" {
		b.WriteString(`error="` + errCode + `", `)
	}
	if len(scopes) > 0 {
		names := make([]string, len(scopes))
		for i, s := range scopes {
			names[i] = string(s)
		}
		b.WriteString(`scope="` + strings.Join(names, " ") + `", `)
	}
	b.WriteString(`resource_metadata="` + metadataURL() + `"`)
	return b.String()
}

func unauthorized(errCode string) *rpcError {
	e := newRPCError(codeInvalidRequest, "authentication is required")
	e.status = http.StatusUnauthorized
	e.challenge = bearerChallenge(errCode, nil)
	return e
}

func forbidden(message string) *rpcError {
	e := newRPCError(codeInvalidRequest, message)
	e.status = http.StatusForbidden
	return e
}

func insufficientScope(missing []auth_model.AccessTokenScope) *rpcError {
	e := newRPCError(codeInvalidRequest, "the token lacks a required scope")
	e.status = http.StatusForbidden
	e.challenge = bearerChallenge("insufficient_scope", missing)
	return e
}

// ownOrigin is the origin of AppURL.
func ownOrigin() string {
	u, err := url.Parse(setting.AppURL)
	if err != nil {
		return ""
	}
	origin, err := setting.NormalizeMCPOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return ""
	}
	return origin
}

// originAllowed reports whether a browser origin may call the endpoint: the origin of AppURL or an entry of
// [mcp] ALLOWED_ORIGINS. "null" and anything that does not parse is refused.
func originAllowed(origin string) bool {
	norm, err := setting.NormalizeMCPOrigin(origin)
	if err != nil {
		return false
	}
	if own := ownOrigin(); own != "" && norm == own {
		return true
	}
	for _, allowed := range setting.MCP.AllowedOrigins {
		if norm == allowed {
			return true
		}
	}
	return false
}

// challengeWriter adds the WWW-Authenticate challenge to a 401 written further down the chain, such as the
// "invalid token" answer of the shared API authentication.
type challengeWriter struct {
	http.ResponseWriter
}

func (w challengeWriter) WriteHeader(code int) {
	if code == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
		w.Header().Set("WWW-Authenticate", bearerChallenge("invalid_token", nil))
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w challengeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func plainError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(http.StatusText(status) + "\n"))
}

// guard runs before the shared API authentication. It refuses, without touching the database:
//   - a disabled endpoint (404);
//   - an Origin that is not allowed (403), which keeps a web page from driving the endpoint;
//   - a token in the query string (400), because URLs end up in logs;
//   - any method but POST (405) and any body that is not JSON (415), before a form could carry a credential;
//   - a request without a Bearer credential (401 with the PRM challenge). Basic, "token" and cookie credentials
//     are not accepted here, so a browser session or a password never opens the endpoint.
//
// A preflight (OPTIONS) goes on to the shared CORS handler when CORS is enabled, after the Origin check.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !setting.MCP.Enabled {
			plainError(w, http.StatusNotFound)
			return
		}
		if origins := r.Header.Values("Origin"); len(origins) > 0 && (len(origins) > 1 || !originAllowed(origins[0])) {
			plainError(w, http.StatusForbidden)
			return
		}
		query := r.URL.Query()
		if query.Has("token") || query.Has("access_token") {
			plainError(w, http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodOptions && setting.CORSConfig.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			plainError(w, http.StatusMethodNotAllowed)
			return
		}
		if !isJSONContentType(r.Header.Get("Content-Type")) {
			plainError(w, http.StatusUnsupportedMediaType)
			return
		}
		if !hasBearer(r) {
			w.Header().Set("WWW-Authenticate", bearerChallenge("", nil))
			plainError(w, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(challengeWriter{w}, r)
	})
}

// hasBearer reports whether the request carries exactly one "Authorization: Bearer <token>" header.
func hasBearer(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	return found && strings.EqualFold(scheme, "Bearer") && strings.TrimSpace(token) != ""
}

// authenticate checks the principal that the shared API authentication produced. It accepts a Forgejo access
// token or an OAuth2 access token that carries a permission scope, and refuses everything else: a password, a
// reverse proxy header, an Actions task token or a credential without a scope.
func authenticate(ctx *context.APIContext) *rpcError {
	ar := ctx.Authentication()
	if !ctx.IsSigned() || ctx.Doer() == nil || ar == nil {
		return unauthorized("invalid_token")
	}
	if ar.IsPasswordAuthentication() || ar.IsReverseProxyAuthentication() {
		return unauthorized("invalid_token")
	}
	if hasTask, _ := ar.ActionsTaskID().Get(); hasTask {
		return forbidden("this credential cannot use the MCP endpoint")
	}
	hasScope, scope := ar.Scope().Get()
	if !hasScope || !scope.HasPermissionScope() {
		return forbidden("this credential cannot use the MCP endpoint")
	}
	return nil
}

// toolGranted reports whether the credential's scope covers the tool, and the scopes it lacks when it does not.
func toolGranted(ctx *context.APIContext, t *Tool) (bool, []auth_model.AccessTokenScope) {
	required := t.requiredScopes()
	ar := ctx.Authentication()
	if ar == nil {
		return false, required
	}
	hasScope, scope := ar.Scope().Get()
	if !hasScope {
		return false, required
	}
	granted, err := scope.HasScope(required...)
	if err != nil || !granted {
		return false, required
	}
	return true, nil
}

// protectedResourceMetadata is the RFC 9728 document.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceName           string   `json:"resource_name"`
}

// ProtectedResource serves the protected resource metadata document. It is public and holds no secret.
func ProtectedResource(w http.ResponseWriter, r *http.Request) {
	if !setting.MCP.Enabled {
		plainError(w, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		plainError(w, http.StatusMethodNotAllowed)
		return
	}
	scopes := make([]string, len(supportedScopes))
	for i, s := range supportedScopes {
		scopes[i] = string(s)
	}
	body, err := json.Marshal(protectedResourceMetadata{
		Resource:               resourceURL(),
		AuthorizationServers:   []string{strings.TrimSuffix(setting.AppURL, "/")},
		ScopesSupported:        scopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Forgejo MCP",
	})
	if err != nil {
		plainError(w, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}
