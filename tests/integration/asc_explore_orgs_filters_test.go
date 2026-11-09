// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheOrganizationDirectoryFilters(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	e := db.GetEngine(db.DefaultContext)
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := e.Exec(append([]any{q}, args...)...)
		require.NoError(t, err)
	}
	exec("INSERT INTO `asc_org_domain` (org_id, domain, token, verified_unix, checked_unix) VALUES (17, 'example.org', 't1', ?, ?)", time.Now().Unix(), time.Now().Unix())
	exec("UPDATE `user` SET type = ? WHERE id = 30", user_model.UserTypeBot)
	exec("INSERT INTO `org_user` (uid, org_id, is_public) VALUES (30, 19, ?)", true)
	exec("UPDATE `user` SET num_members = 150 WHERE id = 19")

	names := func(doc *HTMLDoc) []string {
		return doc.doc.Find(".ds-user-row .ds-li-title > a").Map(func(_ int, s *goquery.Selection) string {
			return strings.TrimSpace(s.Text())
		})
	}
	get := func(t *testing.T, sess *TestSession, url string) *HTMLDoc {
		t.Helper()
		if sess == nil {
			return NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
		}
		return NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
	}

	t.Run("The chips filter and say which one is on", func(t *testing.T) {
		doc := get(t, nil, "/explore/organizations")
		chips := doc.doc.Find("[data-testid=explore-orgs-only] a")
		assert.Equal(t, []string{"all", "verified", "agents"}, chips.Map(func(_ int, s *goquery.Selection) string { return s.AttrOr("data-chip", "") }),
			"a visitor has no I am a member chip")
		assert.Equal(t, "true", chips.First().AttrOr("aria-current", ""))
		assert.Equal(t, "group", doc.doc.Find("[data-testid=explore-orgs-only]").AttrOr("role", ""))

		assert.Equal(t, []string{"org17"}, names(get(t, nil, "/explore/organizations?only=verified")))
		assert.Equal(t, []string{"org19"}, names(get(t, nil, "/explore/organizations?only=agents")))
		doc = get(t, nil, "/explore/organizations?only=verified&sort=alphabetically&q=org")
		on := doc.doc.Find("[data-testid=explore-orgs-only] a[aria-current=true]")
		assert.Equal(t, "verified", on.AttrOr("data-chip", ""))
		href := doc.doc.Find("[data-testid=explore-orgs-only] a[data-chip=agents]").AttrOr("href", "")
		assert.Contains(t, href, "only=agents")
		assert.Contains(t, href, "sort=alphabetically")
		assert.Contains(t, href, "q=org")
		assert.Equal(t, "verified", doc.doc.Find("#explore-user-search input[name=only]").AttrOr("value", ""), "the form keeps the chip")
	})

	t.Run("I am a member is for a signed-in viewer", func(t *testing.T) {
		sess := loginUser(t, "user5")
		doc := get(t, sess, "/explore/organizations?only=member&sort=alphabetically")
		assert.Equal(t, 1, doc.doc.Find("[data-testid=explore-orgs-only] a[data-chip=member][aria-current=true]").Length())
		assert.ElementsMatch(t, []string{"org6", "org7", "privated_org", "limited_org36"}, names(doc))
		assert.Len(t, names(get(t, nil, "/explore/organizations?only=member")), len(names(get(t, nil, "/explore/organizations"))),
			"a visitor asking for it gets All")
	})

	t.Run("Size filters by the number of members", func(t *testing.T) {
		doc := get(t, nil, "/explore/organizations?size=large")
		assert.Equal(t, []string{"org19"}, names(doc))
		assert.Equal(t, "large", doc.doc.Find("[data-testid=explore-orgs-size] option[selected]").AttrOr("value", ""))
		assert.Contains(t, get(t, nil, "/explore/organizations?size=huge").doc.Find("[data-testid=explore-orgs-size] option[selected]").Text(), "Any")
	})

	t.Run("No match says so and offers to clear", func(t *testing.T) {
		doc := get(t, nil, "/explore/organizations?only=verified&size=large")
		assert.Empty(t, names(doc))
		require.Equal(t, 1, doc.doc.Find("[data-asc-orgs-filtered-none]").Length())
		assert.Contains(t, doc.doc.Find("[data-asc-orgs-filtered-none]").Text(), "No organization matches these filters")
	})

	t.Run("Most active puts the organization with the most public actions first", func(t *testing.T) {
		for range 3 {
			exec("INSERT INTO `action` (user_id, act_user_id, op_type, repo_id, is_private, created_unix) VALUES (5, 5, 6, 27, ?, ?)", false, time.Now().Unix())
		}
		doc := get(t, nil, "/explore/organizations?sort=mostactive")
		got := names(doc)
		require.NotEmpty(t, got)
		assert.Equal(t, "org19", got[0])
		assert.Equal(t, "mostactive", doc.doc.Find("select[name=sort] option[selected]").AttrOr("value", ""))
	})
}
