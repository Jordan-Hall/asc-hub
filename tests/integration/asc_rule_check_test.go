// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	auth_model "forgejo.org/models/auth"
	git_model "forgejo.org/models/git"
	"forgejo.org/models/unittest"
	api "forgejo.org/modules/structs"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

func flashError(c *http.Cookie) string {
	raw, _ := url.QueryUnescape(c.Value)
	v, _ := url.ParseQuery(raw)
	return v.Get("error")
}

// TestAscRuleCheck checks the branch rule form (board RepoSettingsRules): a pattern that would protect a branch
// another rule protects is refused, and the form names the open pull requests that target a branch it matches.
func TestAscRuleCheck(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	save := func(id, name string) *http.Cookie {
		session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/branches/edit", map[string]string{
			"rule_id": id, "rule_name": name,
		}), http.StatusSeeOther)
		return session.GetCookie(app_context.CookieNameFlash)
	}

	assert.Contains(t, save("", "master").Value, "success")
	master := unittest.AssertExistsAndLoadBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "master"})

	// a new pattern that also matches master is refused, and no rule is made
	assert.Contains(t, flashError(save("", "ma*")), `Rules cannot overlap: "ma*" and the rule "master" both match a branch.`)
	unittest.AssertNotExistsBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "ma*"})

	// saving master again under its own name is not an overlap with itself
	assert.Contains(t, save(strconv.FormatInt(master.ID, 10), "master").Value, "success")

	assert.Contains(t, save("", "branch*").Value, "success")
	// renaming master onto a pattern that overlaps branch* is refused
	assert.Contains(t, flashError(save(strconv.FormatInt(master.ID, 10), "*2")), `the rule "branch*"`)

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches/edit?rule_name=master"), http.StatusOK).Body)
	many := page.Find("[data-rule-open-many]")
	_, hidden := many.Attr("hidden")
	assert.False(t, hidden)
	assert.Equal(t, "2 open pull requests target a branch this pattern matches.", many.Text())
	_, hidden = page.Find("[data-rule-overlap]").Attr("hidden")
	assert.True(t, hidden)
	form := page.Find("form[data-rule-check]")
	check, _ := form.Attr("data-rule-check")
	assert.Equal(t, "/user2/repo1/settings/branches/check", check)

	var got struct {
		Overlap string `json:"overlap"`
		Open    int64  `json:"open"`
	}
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches/check?rule_id=0&rule_name=branch2"), http.StatusOK)
	DecodeJSON(t, resp, &got)
	assert.Equal(t, "branch*", got.Overlap)
	assert.EqualValues(t, 1, got.Open)

	resp = session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches/check?rule_name=feature/*"), http.StatusOK)
	DecodeJSON(t, resp, &got)
	assert.Empty(t, got.Overlap)
	assert.Zero(t, got.Open)

	// someone who cannot change the settings gets nothing
	MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches/check?rule_name=master"), http.StatusSeeOther)
	loginUser(t, "user4").MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches/check?rule_name=master"), http.StatusNotFound)
}

// TestAscRuleCheckAPI checks that the API refuses a branch protection that overlaps another rule.
func TestAscRuleCheckAPI(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	token := getUserToken(t, "user2", auth_model.AccessTokenScopeWriteRepository)
	create := func(name string, status int) *httptest.ResponseRecorder {
		return MakeRequest(t, NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/branch_protections", &api.BranchProtection{RuleName: name}).AddTokenAuth(token), status)
	}
	create("release/*", http.StatusCreated)
	resp := create("release/1.0", http.StatusUnprocessableEntity)
	assert.Contains(t, resp.Body.String(), `rules cannot overlap: \"release/1.0\" and the rule \"release/*\" both match a branch`)
	create("feature/*", http.StatusCreated)
}
