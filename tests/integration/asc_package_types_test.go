// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
)

func TestPackageTypeChips(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	for _, name := range []string{"nova-client", "orbit-client"} {
		req := NewRequestWithBody(t, "PUT", "/api/packages/user2/generic/"+name+"/1.0.0/build.tar", bytes.NewReader([]byte("build"))).
			AddBasicAuth("user2")
		MakeRequest(t, req, http.StatusCreated)
	}
	sess := loginUser(t, "user2")
	chips := func(t *testing.T, url string) []string {
		t.Helper()
		resp := sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		assert.Zero(t, doc.Find(`select[name="type"]`).Length(), "the type select is replaced by the chips")
		return doc.Find(`[data-testid="package-type-chips"] a.ds-chip-btn`).Map(func(_ int, s *goquery.Selection) string {
			text := strings.Join(strings.Fields(s.Text()), " ")
			if s.AttrOr("aria-current", "") == "true" {
				text += " (current)"
			}
			return text + " " + s.AttrOr("href", "")
		})
	}

	t.Run("Every type with packages gets a chip with its count", func(t *testing.T) {
		assert.Equal(t, []string{"All 2 (current) ?q=", "Generic 2 ?q=&type=generic"}, chips(t, "/user2/-/packages"))
	})

	t.Run("The chosen type is the current chip and the search keeps it", func(t *testing.T) {
		assert.Equal(t, []string{"All 2 ?q=", "Generic 2 (current) ?q=&type=generic"}, chips(t, "/user2/-/packages?type=generic"))
		resp := sess.MakeRequest(t, NewRequest(t, "GET", "/user2/-/packages?type=generic"), http.StatusOK)
		assert.Equal(t, "generic", NewHTMLParser(t, resp.Body).Find(`form[role="search"] input[name="type"]`).AttrOr("value", ""))
	})

	t.Run("The counts follow the name filter", func(t *testing.T) {
		assert.Equal(t, []string{"All 1 (current) ?q=nova", "Generic 1 ?q=nova&type=generic"}, chips(t, "/user2/-/packages?q=nova"))
	})
}
