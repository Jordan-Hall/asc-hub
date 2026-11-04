// Copyright 2022 The Forgejo Authors c/o Codeberg e.V.. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/webhook"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoSettingsHookHistory(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	_, err := db.GetEngine(t.Context()).ID(2).Cols("is_delivered").Update(&webhook.HookTask{IsDelivered: false})
	require.NoError(t, err)

	session := loginUser(t, "user2")

	// open one delivery of the hook on its Recent deliveries tab
	open := func(t *testing.T, uuid string) *HTMLDoc {
		t.Helper()
		req := NewRequest(t, "GET", "/user2/repo1/settings/hooks/1?tab=deliveries&delivery="+uuid)
		return NewHTMLParser(t, session.MakeRequest(t, req, http.StatusOK).Body)
	}
	lines := func(doc *HTMLDoc) []string {
		return doc.doc.Find(".wh-dl-detail .wh-cl").Map(func(_ int, s *goquery.Selection) string { return s.Text() })
	}
	chip := func(doc *HTMLDoc, uuid string) *goquery.Selection {
		return doc.doc.Find(`.wh-dl-li[href$="delivery=` + uuid + `"] .wh-dl-code`)
	}

	t.Run("1/delivered", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		doc := open(t, "uuid1")
		assert.Equal(t, "true", doc.doc.Find(`.wh-dl-li[href$="delivery=uuid1"]`).AttrOr("aria-current", ""))
		assert.True(t, chip(doc, "uuid1").HasClass("ds-chip-err"))
		assert.Equal(t, "No response", strings.TrimSpace(doc.doc.Find(".wh-dl-htext p").Text()))
		assert.Equal(t, []string{"{}"}, lines(doc))
		assert.Contains(t, doc.doc.Find(`.wh-dl-banner[role="alert"]`).Text(), "The receiver did not answer.")
		assert.Equal(t, 1, doc.doc.Find(`.wh-dl-h form[action="/user2/repo1/settings/hooks/1/replay/uuid1"]`).Length())
	})

	t.Run("2/undelivered", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		doc := open(t, "uuid2")
		assert.False(t, chip(doc, "uuid2").HasClass("ds-chip-err"))
		assert.Equal(t, "Waiting to be sent", strings.TrimSpace(doc.doc.Find(".wh-dl-htext p").Text()))
		assert.Zero(t, doc.doc.Find(".wh-dl-banner").Length())
		assert.Zero(t, doc.doc.Find(".wh-dl-h form").Length(), "nothing to redeliver yet")
	})

	t.Run("3/success", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		doc := open(t, "uuid3")
		assert.True(t, chip(doc, "uuid3").HasClass("ds-chip-ok"))
		assert.Equal(t, []string{"{", `  "key": "value"`, "}"}, lines(doc))
		assert.Zero(t, doc.doc.Find(".wh-dl-banner").Length())
	})
}
