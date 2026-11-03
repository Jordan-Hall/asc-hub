// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	packages_model "forgejo.org/models/packages"
	"forgejo.org/modules/test"
	shared_packages "forgejo.org/routers/web/shared/packages"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// The status page answers anyone, lists each part of the hub, and is where the footer sends people.
func TestStatusPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	resp := MakeRequest(t, NewRequest(t, "GET", "/-/status"), http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	page := doc.Find(`[data-testid="status-page"]`)
	assert.Contains(t, []string{"ok", "degraded"}, page.AttrOr("data-status", ""))
	assert.Equal(t, "true", page.Find(`.st-part[data-part="web"]`).AttrOr("data-up", ""))
	assert.Equal(t, "true", page.Find(`.st-part[data-part="database"]`).AttrOr("data-up", ""))
	assert.NotEmpty(t, page.Find("time").AttrOr("datetime", ""), "the page says when the parts were checked")
	assert.Equal(t, 1, doc.Find(`footer a[href="/-/status"]`).Length(), "the footer links the status page")
}

// A package list that cannot be loaded shows its error state in the page: the request id, Try again and the status page.
func TestPackageListErrorState(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	t.Cleanup(test.MockVariableValue(&shared_packages.SearchLatestVersions,
		func(context.Context, *packages_model.PackageSearchOptions) ([]*packages_model.PackageVersion, int64, error) {
			return nil, 0, errors.New("the package table does not answer")
		}))
	sess := loginUser(t, "user2")
	for _, url := range []string{"/user2/-/packages?q=nova", "/user2/repo1/packages?q=nova"} {
		t.Run(url, func(t *testing.T) {
			resp := sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusInternalServerError)
			id := resp.Header().Get("X-Request-Id")
			assert.NotEmpty(t, id)
			doc := NewHTMLParser(t, resp.Body)
			state := doc.Find(`[data-testid="packages-error"]`)
			assert.Equal(t, "We could not load packages", strings.TrimSpace(state.Find("h2").Text()))
			assert.Contains(t, state.Find("p").Text(), "request id "+id)
			assert.Equal(t, url, state.Find("a.ds-btn-primary").AttrOr("href", ""), "Try again loads the same list")
			assert.Equal(t, "/-/status", state.Find("a.ds-btn-ghost").AttrOr("href", ""))
			assert.Zero(t, doc.Find(`[data-testid="package-type-chips"]`).Length())
			assert.Zero(t, doc.Find(".status-page-500").Length(), "the error stays in the page")
		})
	}
}
