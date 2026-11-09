// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"strings"
	"testing"
	"time"

	activities_model "forgejo.org/models/activities"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAscInboxRows(t *testing.T, doer *user_model.User, query string) (int, http.Header, string) {
	t.Helper()
	defer test.MockVariableValue(&ascInboxInfoLoad, func(gocontext.Context, *AscInboxEntry) *AscInboxInfo { return &AscInboxInfo{} })()
	ctx, resp := contexttest.MockContext(t, "/-/asc/inbox/rows"+query)
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	AscInboxRows(ctx)
	return resp.Code, resp.Header(), resp.Body.String()
}

// GET /-/asc/inbox/rows: the rows alone, which the open inbox loads again to add the new ones (asc-inbox-live.js).
func TestAscInboxRows(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	// user 2 has an unread notification on issue 5 in the fixtures
	useAscInboxEntries(t,
		&AscInboxEntry{PullID: 71, IssueID: 5, Title: "unread one", Link: "/o/r/pulls/5", RepoFullName: "o/r", Index: 5, Section: issues_model.AscInboxNeedsReview, Updated: time.Now().Add(-time.Hour), Author: "alice"},
		&AscInboxEntry{PullID: 72, IssueID: 3, Title: "read one", Link: "/o/r/pulls/3", RepoFullName: "o/r", Index: 3, Section: issues_model.AscInboxNeedsReview, Updated: time.Now().Add(-2 * time.Hour), Author: "alice"})

	code, header, body := getAscInboxRows(t, user2, "?pull=71")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "private, no-store", header.Get("Cache-Control"))
	assert.True(t, strings.HasPrefix(strings.TrimSpace(body), "<ul data-asc-inbox-rows>"), "the rows alone, not a page")
	assert.NotContains(t, body, "<html")
	assert.Contains(t, body, `data-asc-key="pull:71"`)
	assert.Contains(t, body, `data-asc-key="pull:72"`)
	assert.Contains(t, body, `data-asc-key="day:`)
	assert.Less(t, strings.Index(body, "pull:71"), strings.Index(body, "pull:72"), "in the order of the page")
	assert.NotContains(t, body, "is-selected", "a row that comes in is never the chosen one")
	assert.NotContains(t, body, `aria-current="true"`)
	assert.Contains(t, body, `name="do" value="done"`, "the rows act as on the page")
	assert.Equal(t, 1, strings.Count(body, "is-unread"))
	n := unittest.AssertExistsAndLoadBean(t, &activities_model.Notification{ID: 4})
	assert.Equal(t, activities_model.NotificationStatusUnread, n.Status, "loading the rows reads nothing, whatever the query says")

	t.Run("the page names the rows of its tab and filters", func(t *testing.T) {
		_, body := getAscInbox(t, user2, "?tab=waiting&sort=oldest&pull=71")
		assert.Contains(t, body, `data-asc-inbox-live="/-/asc/inbox/rows?sort=oldest&amp;tab=waiting"`)
		assert.Contains(t, body, `data-asc-inbox-more`, "the empty tab offers to show what comes in")

		_, body = getAscInbox(t, user2, "")
		assert.Contains(t, body, `<ul class="wk-ilist" data-asc-list data-asc-swipe data-asc-inbox-live="/-/asc/inbox/rows">`)
		assert.Contains(t, body, `data-asc-inbox-news`)
		assert.Contains(t, body, `data-asc-key="pull:71"`, "the page draws the same rows")
	})

	t.Run("off without asc or without a person", func(t *testing.T) {
		code, _, _ := getAscInboxRows(t, nil, "")
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _, _ = getAscInboxRows(t, user2, "")
		assert.Equal(t, http.StatusNotFound, code)
	})
}
