// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

// The ASC IDE OAuth2 public client (SPEC 24.6, P06).
//
// "asc-ide" is the pre-registered public client the ASC VS Code, Cursor,
// VSCodium and JetBrains plugins use to sign a user in. The generic OAuth2
// code treats it like any other application and calls the functions in this
// file at a few points (authorize, redirect check, code exchange, refresh and
// start-up). Everything the client may do is decided here and every check
// fails closed:
//
//   - public client, no secret, PKCE with S256 only (no "plain");
//   - redirect URIs are matched by this file, not by the stored list: three
//     exact custom-scheme URIs and http://127.0.0.1:<any port>[/path]
//     (RFC 8252 section 7.3); nothing else, in particular no "localhost",
//     no IPv6 loopback, no query, no fragment, no user info;
//   - a fixed scope allow-list, checked when the request is made and again
//     when the grant is turned into a token, so a tampered consent form
//     cannot widen the token (an empty scope would otherwise become "all");
//   - the "state" value is required.
//
// The client is not used for MCP (SPEC 22.7.3).

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/log"
	"forgejo.org/services/forms"
)

const (
	// AscIdeClientID is the fixed client_id of the ASC IDE public client.
	// Applications users create get UUID client IDs, so none can collide.
	AscIdeClientID = "asc-ide"

	ascIdeClientName = "ASC IDE"

	// Named limits. Every attacker-controlled string is bounded before use.
	ascIdeMaxRedirectURILen = 512
	ascIdeMaxRedirectPath   = 256
	ascIdeMaxStateLen       = 512
	ascIdeMaxScopeLen       = 256
	// An S256 code challenge is base64url(SHA-256), 43 characters (RFC 7636 4.2).
	ascIdeCodeChallengeLen = 43

	ascIdeLoopbackPrefix = "http://127.0.0.1:"
)

// ascIdeRedirectURIs are the custom-scheme redirects, matched exactly and
// case-sensitively. The IDE builds them as ${env.uriScheme}://asc.asc/auth.
var ascIdeRedirectURIs = []string{
	"vscode://asc.asc/auth",
	"cursor://asc.asc/auth",
	"vscodium://asc.asc/auth",
}

// ascIdeScopes is the allow-list of scopes (SPEC 24.6).
var ascIdeScopes = []string{
	"read:user",
	"read:repository",
	"write:repository",
	"read:issue",
	"write:issue",
}

// ascIdeStoredRedirectURIs is what the application row shows. The loopback
// entry is informational: ascIdeRedirectURIAllowed does the matching.
func ascIdeStoredRedirectURIs() []string {
	return append(slices.Clone(ascIdeRedirectURIs), "http://127.0.0.1")
}

func isAscIdeClient(app *auth.OAuth2Application) bool {
	return app != nil && app.ClientID == AscIdeClientID
}

// EnsureAscIdeClient registers the asc-ide application, or resets a stored
// row (edited by an administrator, say) to the fixed configuration. It runs
// at start-up and is idempotent.
func EnsureAscIdeClient(ctx context.Context) error {
	want := func(app *auth.OAuth2Application) {
		app.UID = 0 // instance-wide: not owned, listed, edited or deleted by a user
		app.Name = ascIdeClientName
		app.ClientID = AscIdeClientID
		app.ClientSecret = "" // public client: no secret exists
		app.ConfidentialClient = false
		app.RedirectURIs = ascIdeStoredRedirectURIs()
	}

	app, err := auth.GetOAuth2ApplicationByClientID(ctx, AscIdeClientID)
	if err != nil {
		if !auth.IsErrOauthClientIDInvalid(err) {
			return fmt.Errorf("load %s client: %w", AscIdeClientID, err)
		}
		app = &auth.OAuth2Application{}
		want(app)
		insertErr := db.Insert(ctx, app)
		if insertErr == nil {
			return nil
		}
		// Another instance may have inserted it first: read it back.
		if app, err = auth.GetOAuth2ApplicationByClientID(ctx, AscIdeClientID); err != nil {
			return fmt.Errorf("register %s client: %w", AscIdeClientID, insertErr)
		}
	}

	if app.UID == 0 && app.Name == ascIdeClientName && app.ClientSecret == "" && !app.ConfidentialClient &&
		slices.Equal(app.RedirectURIs, ascIdeStoredRedirectURIs()) {
		return nil
	}
	log.Warn("The %s OAuth2 client differs from its fixed configuration and was reset", AscIdeClientID)
	want(app)
	if _, err := db.GetEngine(ctx).ID(app.ID).
		Cols("uid", "name", "client_secret", "confidential_client", "redirect_uris").
		Update(app); err != nil {
		return fmt.Errorf("reset %s client: %w", AscIdeClientID, err)
	}
	return nil
}

// ascIdeLoopbackRedirectAllowed accepts http://127.0.0.1:<port> followed by
// nothing or a plain path. It does not use net/url so no parser difference
// can turn another host into a match.
func ascIdeLoopbackRedirectAllowed(uri string) bool {
	rest, ok := strings.CutPrefix(uri, ascIdeLoopbackPrefix)
	if !ok {
		return false
	}
	portStr, pathTail, _ := strings.Cut(rest, "/")
	hasPath := len(rest) > len(portStr)
	if portStr == "" || len(portStr) > 5 || portStr[0] == '0' {
		return false
	}
	for i := range len(portStr) {
		if portStr[i] < '0' || portStr[i] > '9' {
			return false
		}
	}
	if port, err := strconv.Atoi(portStr); err != nil || port < 1 || port > 65535 {
		return false
	}
	if !hasPath {
		return true
	}
	path := "/" + pathTail
	if len(path) > ascIdeMaxRedirectPath {
		return false
	}
	// Plain path characters only: no query, fragment, escapes or backslashes.
	for i := range len(path) {
		c := path[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '/', c == '-', c == '_', c == '.', c == '~':
		default:
			return false
		}
	}
	// No empty or dot segments (one trailing slash is allowed).
	if trimmed := strings.TrimSuffix(path[1:], "/"); trimmed != "" {
		for seg := range strings.SplitSeq(trimmed, "/") {
			if seg == "" || seg == "." || seg == ".." {
				return false
			}
		}
	}
	return true
}

func ascIdeRedirectURIAllowed(uri string) bool {
	if uri == "" || len(uri) > ascIdeMaxRedirectURILen {
		return false
	}
	return slices.Contains(ascIdeRedirectURIs, uri) || ascIdeLoopbackRedirectAllowed(uri)
}

// redirectURIAllowed replaces app.ContainsRedirectURI in the OAuth2 handlers.
func redirectURIAllowed(app *auth.OAuth2Application, uri string) bool {
	if isAscIdeClient(app) {
		return !app.ConfidentialClient && ascIdeRedirectURIAllowed(uri)
	}
	return app.ContainsRedirectURI(uri)
}

// ascIdeScopeError returns a description when scope is not a single-spaced
// list of allowed scopes without repeats, or "" when it is acceptable.
func ascIdeScopeError(scope string) string {
	if scope == "" || len(scope) > ascIdeMaxScopeLen {
		return "scope is required and must be short"
	}
	items := strings.Split(scope, " ") // exactly the split the token code uses
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !slices.Contains(ascIdeScopes, item) {
			return "scope is not allowed for this client"
		}
		if _, dup := seen[item]; dup {
			return "scope lists a value twice"
		}
		seen[item] = struct{}{}
	}
	return ""
}

func ascIdeCodeChallengeValid(challenge string) bool {
	if len(challenge) != ascIdeCodeChallengeLen {
		return false
	}
	for i := range len(challenge) {
		c := challenge[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// ascIdeCheckAuthorize vets an authorization request after the redirect URI
// was accepted. It returns nil for any other client.
func ascIdeCheckAuthorize(app *auth.OAuth2Application, form *forms.AuthorizationForm) *AuthorizeError {
	if !isAscIdeClient(app) {
		return nil
	}
	fail := func(code AuthorizeErrorCode, desc string) *AuthorizeError {
		return &AuthorizeError{ErrorCode: code, ErrorDescription: desc, State: form.State}
	}
	if app.ConfidentialClient {
		return fail(ErrorCodeUnauthorizedClient, "client is not a public client")
	}
	if form.CodeChallengeMethod != "S256" || !ascIdeCodeChallengeValid(form.CodeChallenge) {
		return fail(ErrorCodeInvalidRequest, "PKCE with code_challenge_method S256 is required")
	}
	if form.State == "" || len(form.State) > ascIdeMaxStateLen {
		return fail(ErrorCodeInvalidRequest, "state is required")
	}
	if desc := ascIdeScopeError(form.Scope); desc != "" {
		return fail(ErrorCodeInvalidScope, desc)
	}
	return nil
}

// ascIdeGrantAllowed is the last check before a grant becomes a token, for
// both the code exchange and the refresh. It returns true for any other client.
func ascIdeGrantAllowed(app *auth.OAuth2Application, grant *auth.OAuth2Grant) bool {
	if !isAscIdeClient(app) {
		return true
	}
	return !app.ConfidentialClient && grant != nil && ascIdeScopeError(grant.Scope) == ""
}

// ascIdeCodeAllowed vets an authorization code at the token endpoint: the code
// must carry an S256 challenge and its grant must hold allowed scopes only.
func ascIdeCodeAllowed(app *auth.OAuth2Application, code *auth.OAuth2AuthorizationCode) bool {
	if !isAscIdeClient(app) {
		return true
	}
	if code == nil || code.CodeChallengeMethod != "S256" || !ascIdeCodeChallengeValid(code.CodeChallenge) {
		return false
	}
	return ascIdeGrantAllowed(app, code.Grant)
}
