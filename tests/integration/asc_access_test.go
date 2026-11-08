// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/timeutil"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscAccessMaintain checks the Maintain role (board RepoSettingsAccess): it is picked before adding, it opens
// the General page and the rules but no other setting, it cannot rename the repository or change its visibility,
// and another role ends it.
func TestAscAccessMaintain(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	admin := loginUser(t, "user2")
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration", map[string]string{
		"collaborator": "user4", "role": "maintain",
	}), http.StatusSeeOther)
	c := unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 4})
	assert.Equal(t, perm.AccessModeWrite, c.Mode)
	assert.True(t, c.AscMaintain)

	// the list names the role and the invite of someone who has it says so
	page := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration"), http.StatusOK).Body)
	mode, _ := page.Find(`[data-access-row=person][data-name=user4] .access-mode`).Attr("data-last-value")
	assert.Equal(t, "maintain", mode)
	assert.Contains(t, page.Find(`[data-access-row=person][data-name=user4] .access-mode .text`).Text(), "Maintain")
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration", map[string]string{
		"collaborator": "user4",
	}), http.StatusSeeOther)
	assert.Contains(t, flashError(admin.GetCookie(app_context.CookieNameFlash)), "already has Maintain access")

	m := loginUser(t, "user4")
	home := NewHTMLParser(t, m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	home.AssertElement(t, "#settings-btn", true)
	general := NewHTMLParser(t, m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings"), http.StatusOK).Body)
	general.AssertElement(t, "[data-testid=maintain-note]", true)
	general.AssertElement(t, "#danger-zone", false)
	general.AssertElement(t, "[data-testid=repo-features]", false)
	general.AssertElement(t, "[data-visibility-form] input[name=visibility]", false)
	_, readonly := general.Find("#repo_name").Attr("readonly")
	assert.True(t, readonly)
	m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches"), http.StatusOK)
	m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/tags"), http.StatusOK)
	branches := NewHTMLParser(t, m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/branches"), http.StatusOK).Body)
	branches.AssertElement(t, "[data-testid=maintain-default-note]", true)
	branches.AssertElement(t, "input[name=branch]", false)

	// the default branch stays: it is neither switched nor renamed; another branch can be renamed
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/branches", map[string]string{"action": "default_branch", "branch": "branch2"}), http.StatusNotFound)
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/rename_branch", map[string]string{"from": "master", "to": "main"}), http.StatusNotFound)
	assert.Equal(t, "master", unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}).DefaultBranch)
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/rename_branch", map[string]string{"from": "branch2", "to": "branch2-renamed"}), http.StatusSeeOther)
	assert.Contains(t, m.GetCookie(app_context.CookieNameFlash).Value, "success")
	list := NewHTMLParser(t, m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/branches"), http.StatusOK).Body)
	list.AssertElement(t, `.show-rename-branch-modal[data-old-branch-name=master]`, false)
	for _, p := range []string{"/settings/collaboration", "/settings/keys", "/settings/hooks", "/settings/units", "/settings/actions/secrets", "/settings/collaboration/search?q=user"} {
		m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"+p), http.StatusNotFound)
	}
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration", map[string]string{"collaborator": "user5"}), http.StatusNotFound)
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings", map[string]string{"action": "delete", "repo_name": "repo1"}), http.StatusNotFound)
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	// a rename or a visibility change is refused; the description is saved and the visibility kept
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings", map[string]string{"action": "update", "repo_name": "repo1-renamed"}), http.StatusSeeOther)
	assert.Contains(t, flashError(m.GetCookie(app_context.CookieNameFlash)), "Only an admin")
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings", map[string]string{"action": "update", "repo_name": "repo1", "visibility": "private"}), http.StatusSeeOther)
	assert.Contains(t, flashError(m.GetCookie(app_context.CookieNameFlash)), "Only an admin")
	m.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings", map[string]string{"action": "update", "repo_name": "repo1", "description": "kept by a maintainer"}), http.StatusSeeOther)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	assert.Equal(t, "repo1", repo.Name)
	assert.Equal(t, "kept by a maintainer", repo.Description)
	assert.False(t, repo.IsPrivate)

	// a role picked in the list ends Maintain, and the settings close again
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration/access_mode", map[string]string{"uid": "4", "mode": "2"}), http.StatusOK)
	c = unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 4})
	assert.False(t, c.AscMaintain)
	m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings"), http.StatusNotFound)
	home = NewHTMLParser(t, m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1"), http.StatusOK).Body)
	home.AssertElement(t, "#settings-btn", false)

	// the role menu gives it back
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration/access_mode", map[string]string{"uid": "4", "mode": "maintain"}), http.StatusOK)
	m.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings"), http.StatusOK)
}

// TestAscAccessList checks the one list of the access page (board RepoSettingsAccess): people, teams and agents
// with their role and last activity, the Show All / People / Agents filter, the combined search and the one add
// form that takes a person, an agent or a team.
func TestAscAccessList(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	_, err := db.GetEngine(t.Context()).ID(5).Cols("type", "last_login_unix").Update(&user_model.User{Type: user_model.UserTypeBot, LastLoginUnix: timeutil.TimeStampNow()})
	require.NoError(t, err)

	admin := loginUser(t, "user2")
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration", map[string]string{"collaborator": "user4", "role": "read"}), http.StatusSeeOther)
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration", map[string]string{"collaborator": "user5", "role": "admin"}), http.StatusSeeOther)
	assert.Equal(t, perm.AccessModeRead, unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 4}).Mode)
	assert.Equal(t, perm.AccessModeAdmin, unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 5}).Mode)
	admin.MakeRequest(t, NewRequestWithValues(t, "POST", "/user2/repo1/settings/collaboration", map[string]string{"collaborator": "user8", "role": "owner"}), http.StatusSeeOther)
	assert.Contains(t, flashError(admin.GetCookie(app_context.CookieNameFlash)), "role does not exist")
	unittest.AssertNotExistsBean(t, &repo_model.Collaboration{RepoID: 1, UserID: 8})

	page := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration"), http.StatusOK).Body)
	page.AssertElement(t, "[data-access-row=person][data-name=user4]", true)
	page.AssertElement(t, "[data-access-row=agent][data-name=user5]", true)
	assert.Contains(t, page.Find("[data-access-row=agent][data-name=user5]").Text(), "Agent")
	assert.Contains(t, page.Find("[data-access-row=agent][data-name=user5] [data-last-active]").Text(), "Last active")
	assert.Contains(t, page.Find("[data-access-row=person][data-name=user4] [data-last-active]").Text(), "Not active yet")
	assert.Equal(t, "2", page.Find("[data-testid=access-show] [data-show=all] .ds-count").Text())
	assert.Equal(t, "1", page.Find("[data-testid=access-show] [data-show=agents] .ds-count").Text())
	assert.Equal(t, "Write", page.Find("#access-role option[selected]").Text())
	page.AssertElement(t, "#access-role option[value=maintain]", true)

	agents := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration?show=agents"), http.StatusOK).Body)
	agents.AssertElement(t, "[data-access-row=person]", false)
	agents.AssertElement(t, "[data-access-row=agent][data-name=user5]", true)
	_, current := agents.Find("[data-testid=access-show] [data-show=agents]").Attr("aria-current")
	assert.True(t, current)
	people := NewHTMLParser(t, admin.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration?show=people"), http.StatusOK).Body)
	people.AssertElement(t, "[data-access-row=agent]", false)

	// the combined search answers people and agents with their kind
	var found struct {
		Data []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"data"`
	}
	resp := admin.MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/settings/collaboration/search?q=user5"), http.StatusOK)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &found))
	require.NotEmpty(t, found.Data)
	assert.Equal(t, "user5", found.Data[0].Name)
	assert.Equal(t, "agent", found.Data[0].Kind)

	// in an organization the teams are in the list, the search and the add form
	owner := loginUser(t, "user2")
	org := NewHTMLParser(t, owner.MakeRequest(t, NewRequest(t, "GET", "/org3/repo3/settings/collaboration"), http.StatusOK).Body)
	org.AssertElement(t, "[data-access-row=team][data-name=Owners]", true)
	assert.Contains(t, org.Find("[data-access-row=team][data-name=Owners]").Text(), "member")
	resp = owner.MakeRequest(t, NewRequest(t, "GET", "/org3/repo3/settings/collaboration/search?q=test_team"), http.StatusOK)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &found))
	require.NotEmpty(t, found.Data)
	assert.Equal(t, "team", found.Data[0].Kind)
	// a repository admin who may not add teams is not shown the teams of the organization
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/org3/repo3/settings/collaboration", map[string]string{"collaborator": "user8", "role": "admin"}), http.StatusSeeOther)
	resp = loginUser(t, "user8").MakeRequest(t, NewRequest(t, "GET", "/org3/repo3/settings/collaboration/search?q=test_team"), http.StatusOK)
	found.Data = nil
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &found))
	for _, r := range found.Data {
		assert.NotEqual(t, "team", r.Kind, r.Name)
	}
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/org3/repo3/settings/collaboration", map[string]string{"collaborator": "test_team", "kind": "team", "role": "admin"}), http.StatusSeeOther)
	assert.True(t, organization.HasTeamRepo(t.Context(), 3, 7, 3))
	owner.MakeRequest(t, NewRequestWithValues(t, "POST", "/org3/repo3/settings/collaboration", map[string]string{"collaborator": "teamcreaterepo"}), http.StatusSeeOther)
	assert.True(t, organization.HasTeamRepo(t.Context(), 3, 14, 3), "a typed name that is no user is a team")
}
