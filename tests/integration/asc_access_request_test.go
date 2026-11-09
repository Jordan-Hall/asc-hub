// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/test"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ascFlash is the text of the flash cookie of the session.
func ascFlash(t *testing.T, s *TestSession) string {
	t.Helper()
	c := s.GetCookie(app_context.CookieNameFlash)
	if c == nil {
		return ""
	}
	v, err := url.QueryUnescape(c.Value)
	require.NoError(t, err)
	v, err = url.QueryUnescape(v)
	require.NoError(t, err)
	return v
}

// The 403 page of a repository lets a reader ask its admins for access and lets anyone switch account; the
// admins grant or dismiss the request on the Collaborators page.
func TestAscAccessRequestFromTheForbiddenPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	// user5 can read repo1 of user2 and may not rename issue 1, which user1 opened
	forbidden := "/user2/repo1/issues/1/title"
	refused := func(t *testing.T, s *TestSession) *HTMLDoc {
		t.Helper()
		req := NewRequestWithValues(t, "POST", forbidden, map[string]string{"title": "renamed"})
		req.Header.Set("Accept", "text/html")
		return NewHTMLParser(t, s.MakeRequest(t, req, http.StatusForbidden).Body)
	}
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	reader := loginUser(t, "user5")

	t.Run("A reader is offered a request for write access and to switch account", func(t *testing.T) {
		doc := refused(t, reader)
		form := doc.Find(`form[data-testid="access-request"]`)
		require.Equal(t, 1, form.Length())
		assert.Equal(t, "/user2/repo1/action/request_access", form.AttrOr("action", ""))
		assert.Equal(t, forbidden, form.Find(`input[name="redirect_to"]`).AttrOr("value", ""))
		assert.Equal(t, "500", doc.Find(`textarea[name="message"]`).AttrOr("maxlength", ""))
		assert.Contains(t, form.Find(".ds-hint").Text(), "write access")
		assert.Equal(t, 1, doc.Find(`form[data-testid="switch-account"][action="/user/switch_account"]`).Length())
	})

	t.Run("The request is stored, said to be sent, and can be withdrawn", func(t *testing.T) {
		resp := reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/request_access", map[string]string{
			"message": "I fix the docs", "redirect_to": forbidden,
		}), http.StatusSeeOther)
		assert.Equal(t, forbidden, test.RedirectURL(resp))
		assert.Contains(t, ascFlash(t, reader), "user2/repo1")
		r := unittest.AssertExistsAndLoadBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 5})
		assert.Equal(t, perm.AccessModeWrite, r.Mode)
		assert.Equal(t, "I fix the docs", r.Message)

		doc := refused(t, reader)
		assert.Equal(t, 0, doc.Find(`form[data-testid="access-request"]`).Length())
		requested := doc.Find(`[data-testid="access-requested"]`)
		require.Equal(t, 1, requested.Length())
		assert.Equal(t, 1, requested.Find(`relative-time`).Length())
		assert.Equal(t, 1, requested.Find(`form[action="/user2/repo1/action/withdraw_access_request"]`).Length())

		reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/withdraw_access_request", map[string]string{"redirect_to": forbidden}), http.StatusSeeOther)
		unittest.AssertNotExistsBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 5})
	})

	t.Run("An address outside the hub is not followed back", func(t *testing.T) {
		resp := reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/request_access", map[string]string{"redirect_to": "https://example.org/x"}), http.StatusSeeOther)
		assert.Equal(t, "/user2/repo1", test.RedirectURL(resp))
	})

	t.Run("An admin of the repository has nothing to ask for", func(t *testing.T) {
		owner := loginUser(t, "user2")
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/request_access", map[string]string{"message": "x"}), http.StatusSeeOther)
		unittest.AssertNotExistsBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 2})
	})

	t.Run("The admins see the requests and grant or dismiss them", func(t *testing.T) {
		other := loginUser(t, "user4")
		other.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/request_access", map[string]string{"message": "line one\nline two"}), http.StatusSeeOther)
		owner := loginUser(t, "user2")
		doc := NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration"), http.StatusOK).Body)
		list := doc.Find(`[data-testid="access-requests"]`)
		require.Equal(t, 1, list.Length())
		assert.Equal(t, 2, list.Find(`[data-access-request]`).Length())
		row := list.Find(`[data-access-request="user4"]`)
		assert.Equal(t, "write", row.Find(`[data-request-role]`).AttrOr("data-request-role", ""))
		assert.Equal(t, "line one\nline two", row.Find(`[data-request-message]`).Text())

		r5 := unittest.AssertExistsAndLoadBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 5})
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration/requests/grant", map[string]string{"id": id(r5.ID)}), http.StatusSeeOther)
		assert.Contains(t, ascFlash(t, owner), "user5")
		c := unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 5})
		assert.Equal(t, perm.AccessModeWrite, c.Mode)
		unittest.AssertNotExistsBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 5})

		r4 := unittest.AssertExistsAndLoadBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 4})
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration/requests/dismiss", map[string]string{"id": id(r4.ID)}), http.StatusSeeOther)
		unittest.AssertNotExistsBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 4})
		unittest.AssertNotExistsBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 4})

		owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration/requests/grant", map[string]string{"id": id(r4.ID)}), http.StatusSeeOther)
		assert.Contains(t, ascFlash(t, owner), "no longer there")

		doc = NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration"), http.StatusOK).Body)
		assert.Equal(t, 0, doc.Find(`[data-testid="access-requests"]`).Length())
	})

	t.Run("A writer is offered a request to become an admin", func(t *testing.T) {
		// user5 can write now, and still may not change the settings of the repository
		reader.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration"), http.StatusNotFound)
		reader.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/request_access", nil), http.StatusSeeOther)
		r := unittest.AssertExistsAndLoadBean(t, &repo_model.AscAccessRequest{RepoID: 1, UserID: 5})
		assert.Equal(t, perm.AccessModeAdmin, r.Mode)
	})

	t.Run("A visitor is sent to sign in", func(t *testing.T) {
		resp := MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/action/request_access", nil), http.StatusSeeOther)
		assert.Contains(t, test.RedirectURL(resp), "/user/login")
	})
}

// Switch account signs the person out and opens the sign-in page, which comes back to the page of the hub.
func TestAscSwitchAccount(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	s := loginUser(t, "user5")
	resp := s.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/switch_account", map[string]string{"redirect_to": "/user2/repo1/issues/1"}), http.StatusSeeOther)
	assert.Equal(t, "/user/login?redirect_to="+url.QueryEscape("/user2/repo1/issues/1"), test.RedirectURL(resp))
	resp = s.MakeRequest(t, NewRequest(t, "GET", "/user/settings"), http.StatusSeeOther)
	assert.Contains(t, test.RedirectURL(resp), "/user/login", "the session is signed out")

	for _, risky := range []string{"https://example.org/", "//example.org/", `/\example.org`, "issues"} {
		s := loginUser(t, "user5")
		resp := s.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/switch_account", map[string]string{"redirect_to": risky}), http.StatusSeeOther)
		assert.Equal(t, "/user/login", test.RedirectURL(resp), risky)
	}

	resp = MakeRequest(t, NewRequestWithValues(t, "POST", "/user/switch_account", nil), http.StatusSeeOther)
	assert.Contains(t, test.RedirectURL(resp), "/user/login")
}
