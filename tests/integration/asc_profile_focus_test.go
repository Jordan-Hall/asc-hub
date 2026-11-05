// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscProfileFocus checks the Focus card of a profile (board Profile): chips for the main language and topics
// of the repositories the person changes, each linked to the repositories with it.
func TestAscProfileFocus(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	// user1 wrote pull requests in repo1, which the fixtures give Go
	page := NewHTMLParser(t, emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", "/user1"), http.StatusOK).Body)
	chip := page.Find("[data-asc-pf-focus] a.ds-chip").First()
	assert.Equal(t, "Go", strings.TrimSpace(chip.Text()))
	href, _ := chip.Attr("href")
	assert.Equal(t, "/explore/repos?language=Go", href)
	assert.Equal(t, "Focus", strings.TrimSpace(page.Find("#wk-pfocus-title").Text()))

	// someone who changes nothing has no Focus card
	page = NewHTMLParser(t, emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", "/user5"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-pf-focus]", false)
}
