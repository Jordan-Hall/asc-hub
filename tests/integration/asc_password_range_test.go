// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/modules/auth/password"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// rangeAPI stands in for the range API of Have-I-Been-Pwned: it answers one range and fails every other.
type rangeAPI struct{ asked *[]string }

func (r rangeAPI) RoundTrip(req *http.Request) (*http.Response, error) {
	*r.asked = append(*r.asked, req.URL.Host+req.URL.Path)
	res := &http.Response{Request: req, Header: make(http.Header), StatusCode: http.StatusOK}
	if req.URL.Path == "/range/5BAA6" {
		res.Body = io.NopCloser(strings.NewReader("1E4C9B93F3F0682250B6CF8331B7EE68FD8:9545824\r\n0018A45C4D1DEF81644B54AB7F969B88D65:0\r\n"))
	} else {
		res.StatusCode = http.StatusServiceUnavailable
		res.Body = io.NopCloser(strings.NewReader(""))
	}
	return res, nil
}

// TestAscPasswordRange checks the breach rule of the strength meter (board Onboarding): GET /-/password/range/{prefix}
// answers the breached suffixes of a range, only with PASSWORD_CHECK_PWN, and the sign up form shows the rule then.
func TestAscPasswordRange(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.EnableCaptcha, false)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	var asked []string
	defer test.MockVariableValue(&password.PwnHTTPClient, &http.Client{Transport: rangeAPI{&asked}})()

	t.Run("Off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.PasswordCheckPwn, false)()
		MakeRequest(t, NewRequest(t, "GET", "/-/password/range/5BAA6"), http.StatusNotFound)
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body)
		doc.AssertElement(t, `[data-rule="breach"]`, false)
		doc.AssertElement(t, `[data-password-meter][data-range-url]`, false)
		assert.Empty(t, asked)
	})

	defer test.MockVariableValue(&setting.PasswordCheckPwn, true)()

	t.Run("Range", func(t *testing.T) {
		resp := MakeRequest(t, NewRequest(t, "GET", "/-/password/range/5baa6"), http.StatusOK)
		assert.Equal(t, "1E4C9B93F3F0682250B6CF8331B7EE68FD8", resp.Body.String())
		assert.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
		assert.Equal(t, []string{"api.pwnedpasswords.com/range/5BAA6"}, asked)
	})

	t.Run("BadPrefix", func(t *testing.T) {
		for _, p := range []string{"5BAA", "5BAA61", "ZZZZZ"} {
			MakeRequest(t, NewRequest(t, "GET", "/-/password/range/"+p), http.StatusBadRequest)
		}
		assert.Len(t, asked, 1)
	})

	t.Run("ServiceFails", func(t *testing.T) {
		MakeRequest(t, NewRequest(t, "GET", "/-/password/range/FFFFF"), http.StatusBadGateway)
	})

	t.Run("Form", func(t *testing.T) {
		doc := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body)
		doc.AssertElement(t, `[data-rule="breach"]`, true)
		url, _ := doc.Find(`[data-password-meter]`).Attr("data-range-url")
		assert.Equal(t, setting.AppSubURL+"/-/password/range", url)
	})

	t.Run("Limited", func(t *testing.T) {
		status := 0
		for range 31 {
			resp := MakeRequest(t, NewRequest(t, "GET", "/-/password/range/5BAA6"), NoExpectedStatus)
			if status = resp.Code; status == http.StatusTooManyRequests {
				assert.Equal(t, "60", resp.Header().Get("Retry-After"))
				break
			}
		}
		assert.Equal(t, http.StatusTooManyRequests, status)
	})
}
