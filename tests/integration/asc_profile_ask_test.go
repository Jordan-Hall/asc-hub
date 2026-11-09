// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

// TestAscProfileAsk checks Request a review and Mention on a person's profile (board Profile): the head shows both to
// another signed-in person, each dialog loads a picker of the viewer's open pull requests, and posting it requests
// the review or posts a comment that mentions the person.
func TestAscProfileAsk(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user1")

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user5"), http.StatusOK).Body)
	page.AssertElement(t, "[data-asc-pf-review]", true)
	page.AssertElement(t, "[data-asc-pf-mention]", true)
	page.AssertElement(t, "#profile-ask-review [hx-get='/user5/-/ask/review']", true)

	own := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user1"), http.StatusOK).Body)
	own.AssertElement(t, "[data-asc-pf-review]", false)
	anon := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user5"), http.StatusOK).Body)
	anon.AssertElement(t, "[data-asc-pf-mention]", false)
	MakeRequest(t, NewRequest(t, "GET", "/user5/-/ask/review"), http.StatusSeeOther)

	picker := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user5/-/ask/review"), http.StatusOK).Body)
	picker.AssertElement(t, "[data-asc-ask-form='review'] input[name='pull'][value='2']", true)

	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user5/-/ask/review", map[string]string{"pull": "2"}), http.StatusSeeOther)
	unittest.AssertExistsAndLoadBean(t, &issues_model.Review{IssueID: 2, ReviewerID: 5, Type: issues_model.ReviewTypeRequest})
	flash := session.GetCookie(app_context.CookieNameFlash)
	if assert.NotNil(t, flash) {
		assert.Contains(t, flash.Value, "success")
	}

	resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user5/-/ask/mention", map[string]string{"pull": "3", "content": "a look at the parser?"}), http.StatusSeeOther)
	loc := resp.Header().Get("Location")
	assert.True(t, strings.HasPrefix(loc, "/user2/repo1/pulls/3#issuecomment-"), loc)
	unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: 3, PosterID: 1, Content: "@user5 a look at the parser?"})

	// user4 blocked user1: no request goes through
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user4/-/ask/review", map[string]string{"pull": "2"}), http.StatusNotFound)
}
