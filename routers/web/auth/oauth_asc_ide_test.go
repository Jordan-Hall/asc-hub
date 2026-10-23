// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/jwtx"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/forms"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ascIdeAllScopes = "read:user read:repository write:repository read:issue write:issue"

func ascIdeVerifier() string {
	return strings.Repeat("v", 64)
}

func ascIdeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func ascIdeApp(t *testing.T) *auth.OAuth2Application {
	t.Helper()
	require.NoError(t, EnsureAscIdeClient(db.DefaultContext))
	app, err := auth.GetOAuth2ApplicationByClientID(db.DefaultContext, AscIdeClientID)
	require.NoError(t, err)
	return app
}

func TestAscIdeClientRedirectURIs(t *testing.T) {
	allowed := []string{
		"vscode://asc.asc/auth",
		"cursor://asc.asc/auth",
		"vscodium://asc.asc/auth",
		"http://127.0.0.1:1",
		"http://127.0.0.1:63342",
		"http://127.0.0.1:65535",
		"http://127.0.0.1:63342/",
		"http://127.0.0.1:63342/api/asc/oauth",
		"http://127.0.0.1:50001/callback/",
		"http://127.0.0.1:50001/a-b_c.d~e",
	}
	for _, uri := range allowed {
		assert.True(t, ascIdeRedirectURIAllowed(uri), uri)
	}

	rejected := []string{
		"",
		"vscode://asc.asc/auth/",
		"vscode://asc.asc/auth?x=1",
		"vscode://asc.asc/auth#x",
		"vscode://asc.asc/other",
		"vscode://evil.asc/auth",
		"VSCODE://asc.asc/auth",
		"vscode://ASC.asc/auth",
		"vscode-insiders://asc.asc/auth",
		"vscode://user@asc.asc/auth",
		"https://asc.asc/auth",
		"http://asc.asc/auth",
		"javascript:alert(1)",
		// loopback variants
		"http://127.0.0.1",
		"http://127.0.0.1/",
		"http://127.0.0.1:/",
		"http://127.0.0.1:0",
		"http://127.0.0.1:00080",
		"http://127.0.0.1:080",
		"http://127.0.0.1:65536",
		"http://127.0.0.1:99999",
		"http://127.0.0.1:123456",
		"http://127.0.0.1:-1",
		"http://127.0.0.1:+80",
		"http://127.0.0.1:80a",
		"http://127.0.0.1:8080?x=1",
		"http://127.0.0.1:8080/cb?x=1",
		"http://127.0.0.1:8080/cb#x",
		"http://127.0.0.1:8080#x",
		"http://127.0.0.1:8080//cb",
		"http://127.0.0.1:8080/cb//x",
		"http://127.0.0.1:8080/../cb",
		"http://127.0.0.1:8080/a/./b",
		"http://127.0.0.1:8080/a/..",
		"http://127.0.0.1:8080/%2e%2e/cb",
		"http://127.0.0.1:8080/cb%2f",
		"http://127.0.0.1:8080\\cb",
		"http://127.0.0.1:8080/c\\b",
		"http://127.0.0.1:8080/c b",
		"http://127.0.0.1:8080/c\nb",
		"http://127.0.0.1:8080/@evil.example",
		"http://127.0.0.1:8080@evil.example/",
		"http://127.0.0.1@evil.example:80/",
		"http://127.0.0.1.evil.example:80/",
		"http://127.0.0.1:80:80/",
		"http://127.0.0.2:8080/",
		"http://localhost:8080/",
		"http://[::1]:8080/",
		"https://127.0.0.1:8080/",
		"HTTP://127.0.0.1:8080/",
		"http://user:pw@127.0.0.1:8080/",
		" http://127.0.0.1:8080/",
		"http://127.0.0.1:8080/ ",
		"http://127.0.0.1:8080/" + strings.Repeat("a", ascIdeMaxRedirectPath),
		"vscode://asc.asc/auth" + strings.Repeat("a", ascIdeMaxRedirectURILen),
	}
	for _, uri := range rejected {
		assert.False(t, ascIdeRedirectURIAllowed(uri), "%q", uri)
	}

	// the longest accepted path
	assert.True(t, ascIdeRedirectURIAllowed("http://127.0.0.1:8080/"+strings.Repeat("a", ascIdeMaxRedirectPath-1)))
}

func TestAscIdeClientScopes(t *testing.T) {
	assert.Empty(t, ascIdeScopeError(ascIdeAllScopes))
	assert.Empty(t, ascIdeScopeError("read:user"))
	assert.Empty(t, ascIdeScopeError("write:issue read:user"))

	for _, scope := range []string{
		"",
		" ",
		"all",
		"openid",
		"read:user openid",
		"read:user  read:issue",
		" read:user",
		"read:user ",
		"read:user,read:issue",
		"read:user\tread:issue",
		"read:user read:user",
		"read:organization",
		"write:user",
		"write:admin",
		"public-only",
		"READ:USER",
		strings.Repeat("read:user ", 40),
	} {
		assert.NotEmpty(t, ascIdeScopeError(scope), "%q", scope)
	}
}

func TestAscIdeClientRegistration(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	_, err := auth.GetOAuth2ApplicationByClientID(db.DefaultContext, AscIdeClientID)
	require.Error(t, err)

	app := ascIdeApp(t)
	assert.Equal(t, "asc-ide", app.ClientID)
	assert.Equal(t, "ASC IDE", app.Name)
	assert.Zero(t, app.UID)
	assert.False(t, app.ConfidentialClient)
	assert.Empty(t, app.ClientSecret)
	assert.Equal(t, ascIdeStoredRedirectURIs(), app.RedirectURIs)
	id := app.ID

	// idempotent
	require.NoError(t, EnsureAscIdeClient(db.DefaultContext))
	n, err := db.GetEngine(db.DefaultContext).Where("client_id = ?", AscIdeClientID).Count(new(auth.OAuth2Application))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	app = ascIdeApp(t)
	assert.Equal(t, id, app.ID)

	// a row changed behind our back is reset: confidential, owned, with a secret and a wide redirect
	tampered := &auth.OAuth2Application{
		ID:                 id,
		UID:                2,
		Name:               "x",
		ClientSecret:       "secret-hash",
		ConfidentialClient: true,
		RedirectURIs:       []string{"https://evil.example/"},
	}
	_, err = db.GetEngine(db.DefaultContext).ID(id).
		Cols("uid", "name", "client_secret", "confidential_client", "redirect_uris").Update(tampered)
	require.NoError(t, err)
	app = ascIdeApp(t)
	assert.Equal(t, id, app.ID)
	assert.Zero(t, app.UID)
	assert.Equal(t, "ASC IDE", app.Name)
	assert.Empty(t, app.ClientSecret)
	assert.False(t, app.ConfidentialClient)
	assert.Equal(t, ascIdeStoredRedirectURIs(), app.RedirectURIs)
}

func TestAscIdeClientRedirectURIAllowedByClient(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	app := ascIdeApp(t)

	assert.True(t, redirectURIAllowed(app, "vscode://asc.asc/auth"))
	assert.True(t, redirectURIAllowed(app, "http://127.0.0.1:4321/cb"))
	assert.False(t, redirectURIAllowed(app, "https://example.com/xyzzy"))
	assert.False(t, redirectURIAllowed(app, "http://127.0.0.1"))

	// a confidential asc-ide row accepts nothing
	confidential := *app
	confidential.ConfidentialClient = true
	assert.False(t, redirectURIAllowed(&confidential, "vscode://asc.asc/auth"))

	// other clients keep the generic matching, and cannot use the asc-ide redirects
	generic := unittest.AssertExistsAndLoadBean(t, &auth.OAuth2Application{ID: 2})
	assert.True(t, redirectURIAllowed(generic, "b"))
	assert.True(t, redirectURIAllowed(generic, "http://127.0.0.1:9999"))
	assert.False(t, redirectURIAllowed(generic, "vscode://asc.asc/auth"))
	assert.False(t, redirectURIAllowed(generic, "http://127.0.0.1:9999/cb"))
	webApp := unittest.AssertExistsAndLoadBean(t, &auth.OAuth2Application{ID: 1})
	assert.True(t, redirectURIAllowed(webApp, "https://example.com/xyzzy"))
	assert.False(t, redirectURIAllowed(webApp, "vscode://asc.asc/auth"))
}

func validAscIdeAuthorizeForm() *forms.AuthorizationForm {
	return &forms.AuthorizationForm{
		ResponseType:        "code",
		ClientID:            AscIdeClientID,
		RedirectURI:         "vscode://asc.asc/auth",
		State:               "0123456789abcdef0123456789abcdef",
		Scope:               ascIdeAllScopes,
		CodeChallengeMethod: "S256",
		CodeChallenge:       ascIdeChallenge(ascIdeVerifier()),
	}
}

func TestAscIdeClientCheckAuthorize(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	app := ascIdeApp(t)

	require.Nil(t, ascIdeCheckAuthorize(app, validAscIdeAuthorizeForm()))

	mutate := func(f func(*forms.AuthorizationForm)) *forms.AuthorizationForm {
		form := validAscIdeAuthorizeForm()
		f(form)
		return form
	}
	cases := map[string]struct {
		form *forms.AuthorizationForm
		code AuthorizeErrorCode
	}{
		"no pkce": {mutate(func(f *forms.AuthorizationForm) { f.CodeChallengeMethod, f.CodeChallenge = "", "" }), ErrorCodeInvalidRequest},
		"plain": {mutate(func(f *forms.AuthorizationForm) {
			f.CodeChallengeMethod = "plain"
		}), ErrorCodeInvalidRequest},
		"s256 lowercase":  {mutate(func(f *forms.AuthorizationForm) { f.CodeChallengeMethod = "s256" }), ErrorCodeInvalidRequest},
		"short challenge": {mutate(func(f *forms.AuthorizationForm) { f.CodeChallenge = "abc" }), ErrorCodeInvalidRequest},
		"padded challenge": {mutate(func(f *forms.AuthorizationForm) {
			f.CodeChallenge = f.CodeChallenge[:42] + "="
		}), ErrorCodeInvalidRequest},
		"no state":     {mutate(func(f *forms.AuthorizationForm) { f.State = "" }), ErrorCodeInvalidRequest},
		"long state":   {mutate(func(f *forms.AuthorizationForm) { f.State = strings.Repeat("s", ascIdeMaxStateLen+1) }), ErrorCodeInvalidRequest},
		"no scope":     {mutate(func(f *forms.AuthorizationForm) { f.Scope = "" }), ErrorCodeInvalidScope},
		"scope all":    {mutate(func(f *forms.AuthorizationForm) { f.Scope = "all" }), ErrorCodeInvalidScope},
		"scope extra":  {mutate(func(f *forms.AuthorizationForm) { f.Scope += " read:organization" }), ErrorCodeInvalidScope},
		"scope openid": {mutate(func(f *forms.AuthorizationForm) { f.Scope = "openid" }), ErrorCodeInvalidScope},
	}
	for name, tc := range cases {
		authErr := ascIdeCheckAuthorize(app, tc.form)
		require.NotNil(t, authErr, name)
		assert.Equal(t, tc.code, authErr.ErrorCode, name)
		assert.Equal(t, tc.form.State, authErr.State, name)
	}

	confidential := *app
	confidential.ConfidentialClient = true
	authErr := ascIdeCheckAuthorize(&confidential, validAscIdeAuthorizeForm())
	require.NotNil(t, authErr)
	assert.Equal(t, ErrorCodeUnauthorizedClient, authErr.ErrorCode)

	// other clients are not touched
	generic := unittest.AssertExistsAndLoadBean(t, &auth.OAuth2Application{ID: 1})
	assert.Nil(t, ascIdeCheckAuthorize(generic, &forms.AuthorizationForm{Scope: "all"}))
}

// ascIdeSession is a map-backed session store for the handler tests. The
// embedded interface is nil: only the methods the handler uses are defined.
type ascIdeSession struct {
	session.Store
	values map[any]any
}

func (s *ascIdeSession) Get(k any) any      { return s.values[k] }
func (s *ascIdeSession) Set(k, v any) error { s.values[k] = v; return nil }
func (s *ascIdeSession) Delete(k any) error { delete(s.values, k); return nil }
func (s *ascIdeSession) Release() error     { return nil }

func newAscIdeAuthorizeRequest(t *testing.T, form *forms.AuthorizationForm) (*httptest.ResponseRecorder, map[any]any) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "GET /login/oauth/authorize")
	ctx.Doer = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	store := &ascIdeSession{values: map[any]any{}}
	ctx.Session = store
	ctx.AppendContextValue(context.WebContextKey, ctx) // the form validation looks the context up
	web.SetForm(ctx, form)
	AuthorizeOAuth(ctx)
	return resp, store.values
}

func TestAscIdeClientAuthorizeHandler(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ascIdeApp(t)

	// a valid request reaches the consent page (public clients always ask) and stores the S256 challenge
	form := validAscIdeAuthorizeForm()
	resp, values := newAscIdeAuthorizeRequest(t, form)
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "S256", values["CodeChallengeMethod"])
	assert.Equal(t, form.CodeChallenge, values["CodeChallenge"])

	// loopback on any port, with a path
	form = validAscIdeAuthorizeForm()
	form.RedirectURI = "http://127.0.0.1:41234/api/asc/oauth"
	resp, _ = newAscIdeAuthorizeRequest(t, form)
	assert.Equal(t, http.StatusOK, resp.Code)

	// a bad redirect never redirects back to it: it renders the error page
	for _, uri := range []string{"https://evil.example/cb", "http://localhost:4000/cb", "vscode://asc.asc/auth?x=1", "http://127.0.0.1"} {
		form = validAscIdeAuthorizeForm()
		form.RedirectURI = uri
		resp, values = newAscIdeAuthorizeRequest(t, form)
		assert.Equal(t, http.StatusBadRequest, resp.Code, uri)
		assert.Empty(t, resp.Header().Get("Location"), uri)
		assert.Empty(t, values, uri)
	}

	// policy failures go back to the (valid) redirect URI as an error and store nothing
	for name, mutate := range map[string]func(*forms.AuthorizationForm){
		"plain":    func(f *forms.AuthorizationForm) { f.CodeChallengeMethod = "plain" },
		"no pkce":  func(f *forms.AuthorizationForm) { f.CodeChallengeMethod, f.CodeChallenge = "", "" },
		"scope":    func(f *forms.AuthorizationForm) { f.Scope = "all" },
		"no state": func(f *forms.AuthorizationForm) { f.State = "" },
	} {
		form = validAscIdeAuthorizeForm()
		mutate(form)
		resp, values = newAscIdeAuthorizeRequest(t, form)
		assert.Equal(t, http.StatusSeeOther, resp.Code, name)
		location, err := url.Parse(resp.Header().Get("Location"))
		require.NoError(t, err, name)
		assert.Equal(t, "vscode", location.Scheme, name)
		assert.NotEmpty(t, location.Query().Get("error"), name)
		assert.Empty(t, location.Query().Get("code"), name)
		assert.Empty(t, values, name)
	}
}

// ascIdeIssueCode creates a grant for the asc-ide client and a code on it.
func ascIdeIssueCode(t *testing.T, app *auth.OAuth2Application, scope, challenge, method string) *auth.OAuth2AuthorizationCode {
	t.Helper()
	// one grant per user and client: reuse it and set its scope as the test needs
	grant, err := app.GetGrantByUserID(db.DefaultContext, 2)
	require.NoError(t, err)
	if grant == nil {
		grant, err = app.CreateGrant(db.DefaultContext, 2, scope)
		require.NoError(t, err)
	}
	grant.Scope = scope
	_, err = db.GetEngine(db.DefaultContext).ID(grant.ID).Cols("scope").Update(grant)
	require.NoError(t, err)
	code, err := grant.GenerateNewAuthorizationCode(db.DefaultContext, "vscode://asc.asc/auth", challenge, method)
	require.NoError(t, err)
	return code
}

func exchangeAscIdeCode(t *testing.T, code *auth.OAuth2AuthorizationCode, verifier, redirectURI string) *httptest.ResponseRecorder {
	t.Helper()
	signingKey, err := jwtx.CreateSigningKey("HS256", make([]byte, 32))
	require.NoError(t, err)
	ctx, resp := contexttest.MockContext(t, "POST /login/oauth/access_token")
	handleAuthorizationCode(ctx, forms.AccessTokenForm{
		GrantType:    "authorization_code",
		ClientID:     AscIdeClientID,
		Code:         code.Code,
		RedirectURI:  redirectURI,
		CodeVerifier: verifier,
	}, signingKey, signingKey)
	return resp
}

func TestAscIdeClientTokenExchange(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	app := ascIdeApp(t)
	const redirect = "vscode://asc.asc/auth"

	// the happy path: public client, no secret
	code := ascIdeIssueCode(t, app, ascIdeAllScopes, ascIdeChallenge(ascIdeVerifier()), "S256")
	resp := exchangeAscIdeCode(t, code, ascIdeVerifier(), redirect)
	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), "access_token")

	// a wrong or missing verifier fails
	code = ascIdeIssueCode(t, app, ascIdeAllScopes, ascIdeChallenge(ascIdeVerifier()), "S256")
	assert.Equal(t, http.StatusBadRequest, exchangeAscIdeCode(t, code, strings.Repeat("w", 64), redirect).Code)
	assert.Equal(t, http.StatusBadRequest, exchangeAscIdeCode(t, code, "", redirect).Code)

	// a different redirect URI fails, even another allowed one
	assert.Equal(t, http.StatusBadRequest, exchangeAscIdeCode(t, code, ascIdeVerifier(), "cursor://asc.asc/auth").Code)
	assert.Equal(t, http.StatusBadRequest, exchangeAscIdeCode(t, code, ascIdeVerifier(), "https://evil.example/").Code)
	assert.Equal(t, http.StatusBadRequest, exchangeAscIdeCode(t, code, ascIdeVerifier(), "").Code)

	// a code issued without PKCE is refused although the generic path would accept it
	for name, tc := range map[string]struct{ challenge, method string }{
		"none":  {"", ""},
		"plain": {ascIdeVerifier(), "plain"},
	} {
		code = ascIdeIssueCode(t, app, ascIdeAllScopes, tc.challenge, tc.method)
		verifier := ""
		if tc.method == "plain" {
			verifier = ascIdeVerifier()
		}
		resp = exchangeAscIdeCode(t, code, verifier, redirect)
		assert.Equal(t, http.StatusBadRequest, resp.Code, name)
		assert.NotContains(t, resp.Body.String(), "access_token", name)
	}

	// a grant with a widened or empty scope (a tampered consent form) is refused
	for _, scope := range []string{"", "all", ascIdeAllScopes + " read:organization", "openid"} {
		code = ascIdeIssueCode(t, app, scope, ascIdeChallenge(ascIdeVerifier()), "S256")
		resp = exchangeAscIdeCode(t, code, ascIdeVerifier(), redirect)
		assert.Equal(t, http.StatusBadRequest, resp.Code, scope)
		assert.NotContains(t, resp.Body.String(), "access_token", scope)
	}
}

func TestAscIdeClientRefreshGrant(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	app := ascIdeApp(t)

	good, err := app.CreateGrant(db.DefaultContext, 2, ascIdeAllScopes)
	require.NoError(t, err)
	assert.True(t, ascIdeGrantAllowed(app, good))

	bad, err := app.CreateGrant(db.DefaultContext, 3, "all")
	require.NoError(t, err)
	assert.False(t, ascIdeGrantAllowed(app, bad))
	assert.False(t, ascIdeGrantAllowed(app, nil))

	generic := unittest.AssertExistsAndLoadBean(t, &auth.OAuth2Application{ID: 1})
	assert.True(t, ascIdeGrantAllowed(generic, &auth.OAuth2Grant{Scope: "all"}))
	assert.True(t, ascIdeCodeAllowed(generic, &auth.OAuth2AuthorizationCode{}))
}
