// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	permsRepoUser = int64(1) // user2/repo1
	permsRepoOrg  = int64(3) // org3/repo3, private; user2 is an owner of org3, user4 is a member that is not
	permsOrg      = int64(3) // org3
)

func packagesWriteForm(on bool) map[string]string {
	if on {
		return map[string]string{"allow_packages_write": "on"}
	}
	return map[string]string{}
}

// TestActionsPermissionsRepoToggle: the Actions setting that lets `permissions: packages: write` give the job token
// write access to the packages of the owner has a web form that only a repository admin can use.
func TestActionsPermissionsRepoToggle(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	page := "/user2/repo1/settings/actions/permissions"
	checkbox := "input[name=allow_packages_write]"

	checked := func(t *testing.T, session *TestSession) bool {
		t.Helper()
		resp := session.MakeRequest(t, NewRequest(t, "GET", page), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		require.Equal(t, 1, doc.Find(checkbox).Length())
		_, has := doc.Find(checkbox).Attr("checked")
		return has
	}

	admin := loginUser(t, "user2")

	t.Run("off by default", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		assert.False(t, packagesWriteOfRepo(t, permsRepoUser))
		assert.False(t, checked(t, admin))
	})

	t.Run("a repository admin turns it on and off", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		admin.MakeRequest(t, NewRequestWithValues(t, "POST", page, packagesWriteForm(true)), http.StatusSeeOther)
		assert.True(t, packagesWriteOfRepo(t, permsRepoUser))
		assert.True(t, checked(t, admin))

		admin.MakeRequest(t, NewRequestWithValues(t, "POST", page, packagesWriteForm(false)), http.StatusSeeOther)
		assert.False(t, packagesWriteOfRepo(t, permsRepoUser))
		assert.False(t, checked(t, admin))
	})

	t.Run("a cross-origin request is refused", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		req := NewRequestWithValues(t, "POST", page, packagesWriteForm(true))
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		admin.MakeRequest(t, req, http.StatusForbidden)
		assert.False(t, packagesWriteOfRepo(t, permsRepoUser))
	})

	t.Run("only a repository admin", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		// user5 can read the public repository and is not an admin of it.
		other := loginUser(t, "user5")
		other.MakeRequest(t, NewRequest(t, "GET", page), http.StatusNotFound)
		other.MakeRequest(t, NewRequestWithValues(t, "POST", page, packagesWriteForm(true)), http.StatusNotFound)
		assert.False(t, packagesWriteOfRepo(t, permsRepoUser))

		// Not signed in.
		resp := MakeRequest(t, NewRequestWithValues(t, "POST", page, packagesWriteForm(true)), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")
		assert.False(t, packagesWriteOfRepo(t, permsRepoUser))
	})
}

// TestActionsPermissionsOrgDefault: an organization sets the most its repositories can allow. A repository can only
// narrow it. The default of the organization is no.
func TestActionsPermissionsOrgDefault(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	addActionsUnit(t, permsRepoOrg)
	repoPage := "/org3/repo3/settings/actions/permissions"
	orgPage := "/org3/settings/actions/permissions"
	checkbox := "input[name=allow_packages_write]"
	owner := loginUser(t, "user2")

	t.Run("a repository cannot turn it on while the organization says no", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		setOrgPackagesWrite(t, permsOrg, false)

		resp := owner.MakeRequest(t, NewRequest(t, "GET", repoPage), http.StatusOK)
		_, disabled := NewHTMLParser(t, resp.Body).Find(checkbox).Attr("disabled")
		assert.True(t, disabled)

		owner.MakeRequest(t, NewRequestWithValues(t, "POST", repoPage, packagesWriteForm(true)), http.StatusSeeOther)
		assert.False(t, packagesWriteOfRepo(t, permsRepoOrg))
	})

	t.Run("a repository turns it on when the organization says yes, and off again", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		setOrgPackagesWrite(t, permsOrg, true)
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", repoPage, packagesWriteForm(true)), http.StatusSeeOther)
		assert.True(t, packagesWriteOfRepo(t, permsRepoOrg))
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", repoPage, packagesWriteForm(false)), http.StatusSeeOther)
		assert.False(t, packagesWriteOfRepo(t, permsRepoOrg))
	})

	t.Run("an owner of the organization sets it", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		setOrgPackagesWrite(t, permsOrg, false)
		checked := func() bool {
			resp := owner.MakeRequest(t, NewRequest(t, "GET", orgPage), http.StatusOK)
			_, has := NewHTMLParser(t, resp.Body).Find(checkbox).Attr("checked")
			return has
		}
		assert.False(t, checked())
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", orgPage, packagesWriteForm(true)), http.StatusSeeOther)
		assert.True(t, orgPackagesWrite(t, permsOrg))
		assert.True(t, checked())
		owner.MakeRequest(t, NewRequestWithValues(t, "POST", orgPage, packagesWriteForm(false)), http.StatusSeeOther)
		assert.False(t, orgPackagesWrite(t, permsOrg))
		assert.False(t, checked())
	})

	t.Run("only an owner of the organization", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		setOrgPackagesWrite(t, permsOrg, false)

		member := loginUser(t, "user4")
		member.MakeRequest(t, NewRequest(t, "GET", orgPage), http.StatusNotFound)
		member.MakeRequest(t, NewRequestWithValues(t, "POST", orgPage, packagesWriteForm(true)), http.StatusNotFound)

		req := NewRequestWithValues(t, "POST", orgPage, packagesWriteForm(true))
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		owner.MakeRequest(t, req, http.StatusForbidden)

		resp := MakeRequest(t, NewRequestWithValues(t, "POST", orgPage, packagesWriteForm(true)), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")

		assert.False(t, orgPackagesWrite(t, permsOrg))
	})

	t.Run("the job token of a repository of the organization", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: permsRepoOrg})
		upload := func(name, token string, status int) {
			t.Helper()
			req := NewRequestWithBody(t, "PUT", fmt.Sprintf("/api/packages/org3/generic/%s/1.0.0/file.bin", name), bytes.NewReader([]byte{1, 2, 3})).AddTokenAuth(token)
			MakeRequest(t, req, status)
		}

		// The repository setting is on. Only the organization differs.
		setOrgPackagesWrite(t, permsOrg, false)
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		upload("org-says-no", task.Token, http.StatusUnauthorized)

		setOrgPackagesWrite(t, permsOrg, true)
		upload("org-says-yes", task.Token, http.StatusCreated)

		// The organization turns it off while the job runs.
		setOrgPackagesWrite(t, permsOrg, false)
		upload("org-turned-off", task.Token, http.StatusUnauthorized)

		// An organization that allows it does not give the grant to a repository that did not turn it on.
		setOrgPackagesWrite(t, permsOrg, true)
		task = newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: false})
		upload("repo-says-no", task.Token, http.StatusUnauthorized)
	})
}

// TestActionsPermissionsPackagesConan: a Conan client logs in with the job token and gets a Conan token that carries
// the task, so `permissions: packages: write` applies as it does for a container registry.
func TestActionsPermissionsPackagesConan(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: permsRepoUser}) // user2/repo1

	conanToken := func(t *testing.T, owner string, task *actions_model.ActionTask) string {
		t.Helper()
		req := NewRequest(t, "GET", fmt.Sprintf("%sapi/packages/%s/conan/v2/users/authenticate", setting.AppURL, owner))
		req.Request.SetBasicAuth("token", task.Token)
		resp := MakeRequest(t, req, http.StatusOK)
		require.NotEmpty(t, resp.Body.String())
		return "Bearer " + resp.Body.String()
	}
	upload := func(t *testing.T, owner, name, token string, status int) {
		t.Helper()
		recipeURL := fmt.Sprintf("%sapi/packages/%s/conan/v2/conans/%s/1.0/job/test/revisions/rev1", setting.AppURL, owner, name)
		req := NewRequestWithBody(t, "PUT", fmt.Sprintf("%s/files/%s", recipeURL, conanfileName), strings.NewReader(buildConanfileContent(name, "1.0"))).
			AddTokenAuth(token)
		MakeRequest(t, req, status)
	}

	t.Run("packages write with the setting on", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		token := conanToken(t, "user2", task)
		upload(t, "user2", "ConanJob", token, http.StatusCreated)

		t.Run("the job is over", func(t *testing.T) {
			task.Status = actions_model.StatusSuccess
			require.NoError(t, actions_model.UpdateTask(db.DefaultContext, task, "status"))
			upload(t, "user2", "ConanJobOver", token, http.StatusUnauthorized)
		})
	})

	t.Run("the setting is off", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: false})
		upload(t, "user2", "ConanOff", conanToken(t, "user2", task), http.StatusUnauthorized)
	})

	t.Run("the setting is turned off while the job runs", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		token := conanToken(t, "user2", task)
		setActionsPackagesWrite(t, repo.ID, false)
		upload(t, "user2", "ConanTurnedOff", token, http.StatusUnauthorized)
	})

	t.Run("the workflow does not name packages", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{contents: write}", AllowPackagesWrite: true})
		upload(t, "user2", "ConanNotNamed", conanToken(t, "user2", task), http.StatusUnauthorized)
	})

	t.Run("another owner's namespace", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", AllowPackagesWrite: true})
		token := conanToken(t, "user2", task)
		upload(t, "user5", "ConanOther", token, http.StatusUnauthorized)
		upload(t, "org3", "ConanOtherOrg", token, http.StatusUnauthorized)
	})

	t.Run("fork pull request", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "write-all", IsFork: true, AllowPackagesWrite: true})
		upload(t, "user2", "ConanFork", conanToken(t, "user2", task), http.StatusUnauthorized)
	})

	t.Run("a Conan token made for a job that is over is refused", func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()
		task := newPermissionsTask(t, repo, permissionsTask{Permissions: "{packages: write}", AllowPackagesWrite: true})
		token := conanToken(t, "user2", task)
		task.Status = actions_model.StatusFailure
		require.NoError(t, actions_model.UpdateTask(db.DefaultContext, task, "status"))
		req := NewRequest(t, "GET", fmt.Sprintf("%sapi/packages/user2/conan/v2/users/check_credentials", setting.AppURL)).AddTokenAuth(token)
		MakeRequest(t, req, http.StatusUnauthorized)
	})
}
