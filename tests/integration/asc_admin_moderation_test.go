// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/routers"
	"forgejo.org/services/ascmoderation"
	moderation_service "forgejo.org/services/moderation"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscAdminModerationBoardNotes covers the notes of board AdminModeration: Dismiss, Hide and Suspend are separate
// actions, each with a reason kept with the decision; the queue with items, empty (all caught up), the hidden toast and
// an appeal received.
func TestAscAdminModerationBoardNotes(t *testing.T) {
	defer unittest.OverrideFixtures("tests/integration/fixtures/TestAdminModerationViewReports")()
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Moderation.Enabled, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	admin := loginUser(t, "user1")
	queue := func() *HTMLDoc {
		t.Helper()
		return NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/admin/moderation/reports"), http.StatusOK).Body)
	}
	act := func(values map[string]string) string {
		t.Helper()
		resp := admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/admin/abuse_reports/act", values), http.StatusOK)
		return resp.Body.String()
	}
	vals := func(contentType moderation.ReportedContentType, id int64, ca moderation_service.ContentAction, ra moderation_service.ReportAction, reason string) map[string]string {
		return map[string]string{
			"content_type": strconv.Itoa(int(contentType)), "content_id": strconv.FormatInt(id, 10),
			"content_action": strconv.Itoa(int(ca)), "report_action": strconv.Itoa(int(ra)), "reason": reason,
		}
	}

	// the queue lists items; Dismiss, Hide and Suspend are separate buttons on a reported account, with one reason field
	doc := queue()
	assert.Positive(t, doc.Find("li.report").Length())
	item := doc.Find("#report-4")
	require.Equal(t, 1, item.Length())
	assert.Equal(t, 1, item.Find("textarea[name=reason]").Length())
	hide := fmt.Sprintf(`"content_action": %d`, moderation_service.ContentActionHide)
	suspend := fmt.Sprintf(`"content_action": %d`, moderation_service.ContentActionSuspendAccount)
	var dismissN, hideN, suspendN int
	item.Find(".ds-li-actions button").Each(func(_ int, b *goquery.Selection) {
		v, _ := b.Attr("hx-vals")
		switch {
		case v == fmt.Sprintf(`{"report_action": %d}`, moderation_service.ReportActionMarkAsIgnored):
			dismissN++
		case strings.Contains(v, hide):
			hideN++
		case strings.Contains(v, suspend):
			suspendN++
		}
	})
	assert.Equal(t, [3]int{1, 1, 1}, [3]int{dismissN, hideN, suspendN})

	// Hide without a reason is refused; with one, the comment is hidden and the toast says so
	body := act(vals(moderation.ReportedContentTypeComment, 1001, moderation_service.ContentActionHide, moderation_service.ReportActionMarkAsHandled, ""))
	assert.Contains(t, body, "reason")
	assert.NotEqual(t, ascmoderation.HiddenText, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 1001}).Content)
	body = act(vals(moderation.ReportedContentTypeComment, 1001, moderation_service.ContentActionHide, moderation_service.ReportActionMarkAsHandled, "Spam links."))
	assert.Contains(t, body, "Hidden. It is out of public view until the decision is reversed")
	assert.Equal(t, ascmoderation.HiddenText, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 1001}).Content)
	assert.Equal(t, 0, queue().Find("#report-5").Length(), "its reports are handled and leave the queue")

	// Suspend and Dismiss are their own actions, each kept with its reason
	act(vals(moderation.ReportedContentTypeUser, 1002, moderation_service.ContentActionSuspendAccount, moderation_service.ReportActionNone, "Bulk spam account."))
	assert.True(t, unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1002}).ProhibitLogin)
	act(vals(moderation.ReportedContentTypeIssue, 1001, moderation_service.ContentActionNone, moderation_service.ReportActionMarkAsIgnored, "Not abuse."))
	assert.Equal(t, 0, queue().Find("#report-8").Length())
	ds, err := moderation.DecisionsOf(t.Context(), 1002, 10)
	require.NoError(t, err)
	reasons := map[int]string{}
	for _, d := range ds {
		reasons[d.Action] = d.Reason
	}
	assert.Equal(t, map[int]string{int(moderation_service.ContentActionHide): "Spam links.", int(moderation_service.ContentActionSuspendAccount): "Bulk spam account."}, reasons)

	// an appeal of the hide is received and waits first on the queue
	var hid *moderation.Decision
	for _, d := range ds {
		if d.Action == int(moderation_service.ContentActionHide) {
			hid = d
		}
	}
	require.NotNil(t, hid)
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = 1002", false)
	require.NoError(t, err)
	owner := loginUser(t, "spammer01")
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", fmt.Sprintf("/-/moderation/decisions/%d/appeal", hid.ID), map[string]string{"text": "The link was to my own project."}), http.StatusSeeOther)
	doc = queue()
	appeal := doc.Find(fmt.Sprintf("#appeal-%d", hid.ID))
	require.Equal(t, 1, appeal.Length())
	assert.Contains(t, appeal.Text(), "Hidden from public view")
	assert.Contains(t, appeal.Text(), "The link was to my own project.")
	assert.Contains(t, doc.Find("h2.ds-t-h3").First().Text(), "Appeals waiting (1)")

	// accepting it puts the comment back
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", fmt.Sprintf("/admin/moderation/appeals/%d", hid.ID), map[string]string{"answer": "overturn", "reason": "Own project."}), http.StatusOK)
	assert.Equal(t, "And the first spammer; check SPAM-services/spammer-Tools#1", unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 1001}).Content)

	// empty: every report handled, the queue is all caught up
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `abuse_report` SET status = ?", moderation.ReportStatusTypeHandled)
	require.NoError(t, err)
	doc = queue()
	assert.Equal(t, 0, doc.Find("li.report").Length())
	assert.Contains(t, doc.Find(".ds-settings-body").Text(), "All caught up")
}

// TestAscHiddenContentStaysHidden: while a hide stands the owner cannot edit the text back, and nobody but an
// administrator reads its earlier versions.
func TestAscHiddenContentStaysHidden(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	ctx := t.Context()
	// a comment on user2/repo1#1, posted by user4
	_, err := db.GetEngine(ctx).Exec("UPDATE `comment` SET poster_id = 4 WHERE id = 2")
	require.NoError(t, err)
	comment := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 2})
	adminUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	require.NoError(t, ascmoderation.Hide(ctx, adminUser, moderation.ReportedContentTypeComment, comment.ID))

	owner := loginUser(t, "user4")
	resp := owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/comments/2", map[string]string{"content": "back again", "content_version": strconv.Itoa(comment.ContentVersion)}), http.StatusBadRequest)
	assert.Contains(t, resp.Body.String(), "A moderator hid this")
	assert.Equal(t, ascmoderation.HiddenText, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{ID: 2}).Content)
	resp = owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1/content-history/list?comment_id=2"), http.StatusNotFound)
	assert.Contains(t, resp.Body.String(), "earlier versions are hidden")
	loginUser(t, "user1").MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1/content-history/list?comment_id=2"), http.StatusOK)

	require.NoError(t, ascmoderation.Unhide(ctx, moderation.ReportedContentTypeComment, comment.ID))
	owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1/content-history/list?comment_id=2"), http.StatusOK)
}
