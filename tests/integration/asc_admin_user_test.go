// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board note: the person page default, suspended (banner), pending verification (banner) and deletion blocked by owned
// repositories.
func TestAscAdminUserBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user1")
	page := func(id string) *HTMLDoc {
		return NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/users/"+id), http.StatusOK).Body)
	}

	// default: no banner, and Delete is offered to a person who owns no repository
	doc := page("4")
	banners := doc.Find(".ds-banner").Text()
	assert.NotContains(t, banners, "suspended")
	assert.NotContains(t, banners, "not activated")
	doc.AssertElement(t, `[data-testid="delete-blocked"]`, false)
	_, disabled := doc.Find(`button[data-modal="#adm-delete-user"]`).Attr("disabled")
	assert.False(t, disabled)

	// deletion blocked: user2 owns repositories, so the page says so, links them and Delete is off
	doc = page("2")
	blocked := doc.Find(`[data-testid="delete-blocked"]`)
	assert.Contains(t, blocked.Text(), "They still own 19 repositories")
	assert.Equal(t, "/admin/repos?q=user2", blocked.Find("a").AttrOr("href", ""))
	_, disabled = doc.Find(`button[data-modal="#adm-delete-user"]`).Attr("disabled")
	assert.True(t, disabled)
	// and the server refuses too
	session.MakeRequest(t, NewRequest(t, "POST", "/admin/users/2/delete"), http.StatusSeeOther)
	unittestUserExists(t, 2)

	// suspended: the red banner
	_, err := db.GetEngine(t.Context()).ID(4).Cols("prohibit_login").Update(&user_model.User{ProhibitLogin: true})
	require.NoError(t, err)
	assert.Contains(t, page("4").Find(".ds-banner-err").Text(), "This person is suspended.")

	// pending verification: the warning banner
	assert.Contains(t, page("9").Find(".ds-banner-warn").Text(), "This account is not activated yet.")
}

func unittestUserExists(t *testing.T, id int64) {
	t.Helper()
	has, err := db.GetEngine(t.Context()).ID(id).Exist(new(user_model.User))
	require.NoError(t, err)
	assert.True(t, has)
}
