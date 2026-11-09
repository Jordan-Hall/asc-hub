// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/setting"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscProfileContributions checks the contribution grid of a profile (board Profile): the year switch keeps the
// tab, a year shows its own total, and the summary and the table of days are there on the overview and on the
// activity tab.
func TestAscProfileContributions(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	year := strconv.Itoa(time.Now().In(setting.DefaultUILocation).Year())

	// the fixtures have no activity in the last year, so the overview of a chosen year shows the grid
	page := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2?year="+year), http.StatusOK).Body)
	page.AssertElement(t, ".wk-hm-years .wk-hm-year.is-current[aria-current='page']", true)
	page.AssertElement(t, "[data-asc-heat-summary]", true)
	page.AssertElement(t, "[data-asc-heat-table] summary", true)
	page.AssertElement(t, ".wk-hm-years a[href='/user2']", true)

	act := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2?tab=activity&year="+year), http.StatusOK).Body)
	current := act.Find(".wk-hm-year.is-current")
	assert.Equal(t, year, strings.TrimSpace(current.Text()))
	act.AssertElement(t, ".wk-hm-years a[href='/user2?tab=activity']", true)
	assert.Contains(t, act.Find("#wk-heat-title").Parent().Text(), "in "+year)

	// a year the switch does not offer falls back to the last twelve months
	old := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user2?tab=activity&year=1990"), http.StatusOK).Body)
	assert.Contains(t, old.Find(".wk-hm-year.is-current").Text(), "Last 12 months")
}
