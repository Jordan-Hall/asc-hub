// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Recovery codes: ten are made together and shown once; each opens the second factor once; making new ones retires the old.
func TestRecoveryCodesOpenTheSecondFactorOnce(t *testing.T) {
	defer tests.PrintCurrentTest(t)()
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	session.EnrollTOTP(t)

	// no codes yet
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK)
	assert.Equal(t, 0, NewHTMLParser(t, resp.Body).Find(`[data-testid="recovery-count"]`).Length())

	codesOf := func() []string {
		resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/security/two_factor/recovery_codes", nil), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		assert.Equal(t, "no-store", resp.Header().Get("Cache-Control"), "the codes are shown once and not kept by a cache")
		var codes []string
		doc.Find(`[data-testid="recovery-codes"] code`).Each(func(_ int, s *goquery.Selection) { codes = append(codes, strings.TrimSpace(s.Text())) })
		href, _ := doc.Find(`[data-testid="recovery-download"]`).Attr("href")
		assert.True(t, strings.HasPrefix(href, "data:text/plain;charset=utf-8,"), "the download is the codes as a text file")
		return codes
	}
	codes := codesOf()
	require.Len(t, codes, 10)
	for _, c := range codes {
		assert.Regexp(t, regexp.MustCompile(`^[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}$`), c)
	}

	// the security page counts them
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK)
	assert.Contains(t, NewHTMLParser(t, resp.Body).Find(`[data-testid="recovery-count"]`).Text(), "10")

	signInWith := func(code string, want int) {
		t.Helper()
		s := loginUser(t, "user2") // the password step: the second factor is asked next
		s.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/two_factor/scratch", map[string]string{"token": code}), want)
	}
	signInWith(strings.ToLower(codes[0]), http.StatusSeeOther) // a code works, in any case
	signInWith(codes[0], http.StatusOK)                        // and once
	signInWith("AAAA-AAAA-AAAA", http.StatusOK)                // a code nobody has

	// new codes retire the old ones
	fresh := codesOf()
	require.Len(t, fresh, 10)
	signInWith(codes[1], http.StatusOK)
	signInWith(fresh[0], http.StatusSeeOther)

	// the codes count what is left
	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/security"), http.StatusOK)
	assert.Contains(t, NewHTMLParser(t, resp.Body).Find(`[data-testid="recovery-count"]`).Text(), "9")
}
