// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board note: each runner row says Running a job, Idle or Offline with when it was last seen, Draining when it takes no new
// job, and with no runners the page shows the empty state beside the register card.
func TestAscAdminRunnersBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	ctx := t.Context()
	now := timeutil.TimeStampNow()
	set := func(name string, cols map[string]any) {
		_, err := db.GetEngine(ctx).Table("action_runner").Where("name = ?", name).Update(cols)
		require.NoError(t, err)
	}
	set("global-online", map[string]any{"last_online": now, "last_active": now})                                                  // running a job
	set("user-online", map[string]any{"last_online": now, "last_active": now.Add(-int64(time.Hour.Seconds())), "draining": true}) // idle, draining
	set("global-offline", map[string]any{"last_online": now.Add(-int64((3 * time.Hour).Seconds())), "last_active": 0})

	session := loginUser(t, "user1")
	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/actions/runners"), http.StatusOK).Body)
	rows := map[string]string{}
	doc.Find(".runner-list > li").Each(func(_ int, li *goquery.Selection) {
		rows[strings.TrimSpace(li.Find(".ds-li-title a").First().Text())] = strings.Join(strings.Fields(li.Text()), " ")
	})
	assert.Contains(t, rows["global-online"], "Running a job")
	assert.NotContains(t, rows["global-online"], "Draining")
	assert.Contains(t, rows["user-online"], "Idle")
	assert.Contains(t, rows["user-online"], "Draining")
	assert.Contains(t, rows["global-offline"], "Offline")
	assert.Contains(t, rows["global-offline"], "Last seen", "offline says since when")
	assert.Contains(t, rows["repo-never-used"], "Never")

	// no runners: the empty state, and the register card with Get a token beside it
	_, err := db.GetEngine(ctx).Where("1 = 1").Delete(new(actions_model.ActionRunner))
	require.NoError(t, err)
	doc = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/actions/runners"), http.StatusOK).Body)
	assert.Equal(t, 0, doc.Find(".runner-list").Length())
	assert.Contains(t, doc.Find(".ds-empty").First().Text(), "No runners yet")
	assert.Equal(t, 1, doc.Find(`.adm-reg [data-testid="runner-token-get"]`).Length())
}
