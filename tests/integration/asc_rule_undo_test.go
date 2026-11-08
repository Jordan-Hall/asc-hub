// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	git_model "forgejo.org/models/git"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscRuleUndo checks the rules page (board RepoSettingsRules): a removed rule is listed with Undo for 30
// minutes, Undo puts it back with its settings, and an undo that comes too late or would overlap is refused.
func TestAscRuleUndo(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/branches/edit", map[string]string{
		"rule_name": "master", "required_approvals": "2", "enable_status_check": "on", "status_check_contexts": "ci",
	}), http.StatusSeeOther)
	master := unittest.AssertExistsAndLoadBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "master"})

	resp := session.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/user2/repo1/settings/branches/%d/delete", master.ID)), http.StatusOK)
	assert.Contains(t, resp.Body.String(), "/user2/repo1/settings/branches")
	unittest.AssertNotExistsBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "master"})
	removed := unittest.AssertExistsAndLoadBean(t, &git_model.AscRemovedRule{RepoID: 1, RuleName: "master"})

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches"), http.StatusOK).Body)
	row := page.Find("[data-testid=removed-rules] form[data-removed-rule=master]")
	require.Equal(t, 1, row.Length())
	action, _ := row.Attr("action")
	assert.Equal(t, fmt.Sprintf("/user2/repo1/settings/branches/removed/%d/restore", removed.ID), action)
	assert.Contains(t, row.Text(), "Rule master removed")
	assert.Contains(t, row.Text(), "Undo until")
	label, _ := row.Find("button").Attr("aria-label")
	assert.Equal(t, `Put back the rule "master"`, label)

	session.MakeRequest(t, NewRequest(t, "POST", action), http.StatusSeeOther)
	assert.Contains(t, session.GetCookie(app_context.CookieNameFlash).Value, "success")
	back := unittest.AssertExistsAndLoadBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "master"})
	assert.EqualValues(t, 2, back.RequiredApprovals)
	assert.Equal(t, []string{"ci"}, back.StatusCheckContexts)
	unittest.AssertNotExistsBean(t, &git_model.AscRemovedRule{ID: removed.ID})
	page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches"), http.StatusOK).Body)
	page.AssertElement(t, "[data-testid=removed-rules]", false)

	// A second Undo of the same removal finds nothing to put back.
	session.MakeRequest(t, NewRequest(t, "POST", action), http.StatusSeeOther)
	assert.Contains(t, flashError(session.GetCookie(app_context.CookieNameFlash)), "can no longer be put back")

	// A rule added since that overlaps keeps the removed one out.
	session.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/user2/repo1/settings/branches/%d/delete", back.ID)), http.StatusOK)
	removed = unittest.AssertExistsAndLoadBean(t, &git_model.AscRemovedRule{RepoID: 1, RuleName: "master"})
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/branches/edit", map[string]string{
		"rule_name": "ma*",
	}), http.StatusSeeOther)
	session.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/user2/repo1/settings/branches/removed/%d/restore", removed.ID)), http.StatusSeeOther)
	assert.Contains(t, flashError(session.GetCookie(app_context.CookieNameFlash)), `would overlap the rule "ma*"`)
	unittest.AssertNotExistsBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "master"})

	// Past 30 minutes the removal is no longer listed and cannot be undone.
	timeutil.MockSet(time.Now().Add(git_model.AscRuleUndoWindow + time.Minute))
	defer timeutil.MockUnset()
	page = NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches"), http.StatusOK).Body)
	page.AssertElement(t, "[data-testid=removed-rules]", false)
	session.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/user2/repo1/settings/branches/removed/%d/restore", removed.ID)), http.StatusSeeOther)
	assert.Contains(t, flashError(session.GetCookie(app_context.CookieNameFlash)), "the 30 minutes have passed")
}

// TestAscRuleUndoAPI checks that a rule removed through the branch protection API can be put back on the page too.
func TestAscRuleUndoAPI(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository)
	session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/branches/edit", map[string]string{
		"rule_name": "branch2",
	}), http.StatusSeeOther)
	MakeRequest(t, NewRequest(t, "DELETE", "/api/v1/repos/user2/repo1/branch_protections/branch2").AddTokenAuth(token), http.StatusNoContent)
	removed := unittest.AssertExistsAndLoadBean(t, &git_model.AscRemovedRule{RepoID: 1, RuleName: "branch2"})
	assert.EqualValues(t, 2, removed.RemovedBy)

	// Only someone who can change the settings may undo.
	other := loginUser(t, "user4")
	other.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/user2/repo1/settings/branches/removed/%d/restore", removed.ID)), http.StatusNotFound)
	unittest.AssertNotExistsBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "branch2"})

	session.MakeRequest(t, NewRequest(t, "POST", fmt.Sprintf("/user2/repo1/settings/branches/removed/%d/restore", removed.ID)), http.StatusSeeOther)
	unittest.AssertExistsAndLoadBean(t, &git_model.ProtectedBranch{RepoID: 1, RuleName: "branch2"})
}
