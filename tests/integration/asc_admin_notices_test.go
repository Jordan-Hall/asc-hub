// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"testing"

	system_model "forgejo.org/models/system"
	"forgejo.org/models/unittest"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscAdminNoticesBoardNotes covers the notes of board AdminNotices: Dismiss removes one notice; Dismiss all asks to
// confirm in a dialog first; the default list and the empty state ("Nothing to report").
func TestAscAdminNoticesBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	require.NoError(t, system_model.DeleteNotices(t.Context(), 0, 0))
	for _, d := range []string{"Disk usage crossed 75%", "Mail delivery failed 3 times", "Repository check found a broken repository"} {
		require.NoError(t, system_model.CreateNotice(t.Context(), system_model.NoticeRepository, d))
	}
	session := loginUser(t, "user1")
	page := func() *HTMLDoc {
		t.Helper()
		return NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/notices"), http.StatusOK).Body)
	}

	// default: every notice with its own Dismiss, and Dismiss all behind a confirmation dialog
	doc := page()
	items := doc.Find("ul.ds-list > li")
	require.Equal(t, 3, items.Length())
	assert.Equal(t, 3, doc.Find(`form[action="/admin/notices/delete"] button[type=submit]`).Length())
	open := doc.Find(`button.show-modal[data-modal="#adm-dismiss-all"]`)
	require.Equal(t, 1, open.Length(), "Dismiss all only opens the dialog")
	assert.Equal(t, 1, doc.Find(`dialog#adm-dismiss-all form[action="/admin/notices/empty"]`).Length())
	assert.Contains(t, doc.Find("dialog#adm-dismiss-all").Text(), "Every notice is deleted for good")
	assert.Equal(t, 0, doc.Find(`form[action="/admin/notices/empty"]`).Not("dialog form").Length(), "no form outside the dialog empties the list")

	// Dismiss removes that one notice only
	id, ok := items.First().Find(`input[name="ids[]"]`).Attr("value")
	require.True(t, ok)
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/notices/delete", map[string]string{"ids[]": id, "dismiss": "true"}), http.StatusSeeOther)
	n, _ := strconv.ParseInt(id, 10, 64)
	unittest.AssertNotExistsBean(t, &system_model.Notice{ID: n})
	assert.Equal(t, 2, page().Find("ul.ds-list > li").Length())

	// the confirmed Dismiss all empties the list: the empty state
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/notices/empty", map[string]string{}), http.StatusSeeOther)
	assert.Zero(t, unittest.GetCount(t, &system_model.Notice{}))
	doc = page()
	assert.Equal(t, 0, doc.Find("ul.ds-list > li").Length())
	assert.Contains(t, doc.Find(".ds-settings-body").Text(), "Nothing to report")
	assert.Equal(t, 0, doc.Find("#adm-dismiss-all").Length(), "nothing to dismiss")
}
