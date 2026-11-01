// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/auth"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/markbates/goth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSMTP is an SMTP server that takes AUTH PLAIN for one name and password.
func fakeSMTP(t *testing.T, login, password string) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })
	want := base64.StdEncoding.EncodeToString([]byte("\x00" + login + "\x00" + password))
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				fmt.Fprint(c, "220 fake ESMTP\r\n")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimSpace(line)
					switch {
					case strings.HasPrefix(line, "EHLO"):
						fmt.Fprint(c, "250-fake\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(line, "AUTH PLAIN "):
						if strings.TrimPrefix(line, "AUTH PLAIN ") == want {
							fmt.Fprint(c, "235 ok\r\n")
						} else {
							fmt.Fprint(c, "535 no\r\n")
						}
					case line == "QUIT":
						fmt.Fprint(c, "221 bye\r\n")
						return
					default:
						fmt.Fprint(c, "250 ok\r\n")
					}
				}
			}(c)
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

// The Test sign-in of a source shows what it says about a person and signs nobody in.
func TestAuthSourceTestSignIn(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	port := fakeSMTP(t, "probe@example.com", "s3cret")
	smtpSource := addAuthSource(t, map[string]string{
		"type": fmt.Sprintf("%d", auth.SMTP), "name": "probe-smtp", "is_active": "on",
		"smtp_auth": "PLAIN", "smtp_host": "127.0.0.1", "smtp_port": fmt.Sprint(port), "helo_hostname": "hub",
	})
	admin := loginUser(t, "user1")
	link := fmt.Sprintf("/admin/auths/%d", smtpSource.ID)

	doc := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="auth-test-form"]`).Length())

	// a good password: the claims, and no account made
	resp := admin.MakeRequest(t, NewRequestWithValues(t, "POST", link+"/test", map[string]string{"login": "probe@example.com", "password": "s3cret"}), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	claims := doc.Find(`[data-testid="auth-test-claims"] tbody`).Text()
	assert.Contains(t, claims, "probe@example.com")
	assert.Contains(t, claims, "username")
	unittest.AssertNotExistsBean(t, &user_model.User{LowerName: "probe"})

	// a bad one: refused
	resp = admin.MakeRequest(t, NewRequestWithValues(t, "POST", link+"/test", map[string]string{"login": "probe@example.com", "password": "wrong"}), http.StatusOK)
	doc = NewHTMLParser(t, resp.Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="auth-test-error"]`).Length())
	assert.Equal(t, 0, doc.Find(`[data-testid="auth-test-claims"]`).Length())

	// only admins
	loginUser(t, "user2").MakeRequest(t, NewRequestWithValues(t, "POST", link+"/test", map[string]string{"login": "probe@example.com", "password": "s3cret"}), http.StatusForbidden)

	// an OAuth source: the admin goes to the provider and comes back to the claims
	oauthSource := addAuthSource(t, authSourcePayloadGitLab("probe-oauth"))
	oauthLink := fmt.Sprintf("/admin/auths/%d", oauthSource.ID)
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", oauthLink), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`[data-testid="auth-test-oauth"]`).Length())
	resp = admin.MakeRequest(t, NewRequest(t, "GET", oauthLink+"/test-oauth"), http.StatusSeeOther)
	assert.Equal(t, "/user/oauth2/probe-oauth", resp.Header().Get("Location"))

	defer mockCompleteUserAuth(func(res http.ResponseWriter, req *http.Request) (goth.User, error) {
		return goth.User{Provider: "gitlab", UserID: "4242", Email: "probe-oauth@example.com", NickName: "probeoauth", RawData: map[string]any{"groups": []string{"dev"}}}, nil
	})()
	resp = admin.MakeRequest(t, NewRequest(t, "GET", "/user/oauth2/probe-oauth/callback?code=x&state=y"), http.StatusSeeOther)
	assert.Equal(t, oauthLink+"#auth-test", resp.Header().Get("Location"))
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", oauthLink), http.StatusOK).Body)
	claims = doc.Find(`[data-testid="auth-test-claims"] tbody`).Text()
	assert.Contains(t, claims, "probe-oauth@example.com")
	assert.Contains(t, claims, "raw.groups")
	unittest.AssertNotExistsBean(t, &user_model.User{LowerName: "probeoauth"})
	unittest.AssertCount(t, &user_model.ExternalLoginUser{ExternalID: "4242"}, 0)

	// shown once
	doc = NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", oauthLink), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(`[data-testid="auth-test-claims"]`).Length())
}
