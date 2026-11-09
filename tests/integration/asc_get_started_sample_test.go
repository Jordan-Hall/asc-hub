// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsample"
	files_service "forgejo.org/services/repository/files"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Open the sample project on Get started makes the read-only sample once and opens it; it does not count as a
// repository of the person.
func TestAscGetStartedOpensTheSampleProject(t *testing.T) {
	// the commits of the sample go through the hooks, which call the running hub
	onApplicationRun(t, testAscGetStartedOpensTheSampleProject)
}

func testAscGetStartedOpensTheSampleProject(t *testing.T, _ *url.URL) {
	ctx := t.Context()
	session := loginUser(t, "user8")

	t.Run("The card offers the sample project", func(t *testing.T) {
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started"), http.StatusOK).Body)
		form := page.Find(`form[data-testid="open-sample"]`)
		require.Equal(t, 1, form.Length())
		assert.Equal(t, "/-/get-started/sample", form.AttrOr("action", ""))
		assert.Equal(t, "post", form.AttrOr("method", ""))
		assert.Equal(t, "Open the sample project", strings.TrimSpace(form.Find("button").Text()))
	})

	t.Run("Opening it makes it and goes there", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequest(t, "POST", "/-/get-started/sample"), http.StatusSeeOther)
		assert.Equal(t, "/asc-sample/sample-project", test.RedirectURL(resp))

		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{LowerName: ascsample.OwnerName})
		assert.True(t, owner.IsBot())
		assert.True(t, owner.ProhibitLogin)
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: owner.ID, LowerName: ascsample.RepoName})
		assert.True(t, repo.IsArchived)
		assert.False(t, repo.IsPrivate)
		assert.Equal(t, "main", repo.DefaultBranch)

		gitRepo, err := gitrepo.OpenRepository(ctx, repo)
		require.NoError(t, err)
		defer gitRepo.Close()
		head, err := gitRepo.GetBranchCommit("main")
		require.NoError(t, err)
		n, err := head.CommitsCount()
		require.NoError(t, err)
		assert.EqualValues(t, 4, n)

		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/asc-sample/sample-project"), http.StatusOK).Body)
		assert.Contains(t, page.Find("#readme").Text(), "read-only sample project")
		session.MakeRequest(t, NewRequest(t, "GET", "/asc-sample/sample-project/src/branch/main/src/client.rs"), http.StatusOK)
		MakeRequest(t, NewRequest(t, "GET", "/asc-sample/sample-project/src/branch/main/docs/STACKS.md"), http.StatusOK)
	})

	t.Run("It cannot be changed", func(t *testing.T) {
		repo, err := ascsample.Ensure(ctx)
		require.NoError(t, err)
		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})
		_, err = files_service.ChangeRepoFiles(ctx, repo, owner, &files_service.ChangeRepoFilesOptions{
			Message: "x",
			Files:   []*files_service.ChangeRepoFile{{Operation: "create", TreePath: "x.txt", ContentReader: strings.NewReader("x")}},
		})
		require.ErrorAs(t, err, &repo_model.ErrRepoIsArchived{})
	})

	t.Run("A second opening finds the same project", func(t *testing.T) {
		resp := loginUser(t, "user2").MakeRequest(t, NewRequest(t, "POST", "/-/get-started/sample"), http.StatusSeeOther)
		assert.Equal(t, "/asc-sample/sample-project", test.RedirectURL(resp))
		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{LowerName: ascsample.OwnerName})
		assert.Equal(t, 1, unittest.GetCount(t, &repo_model.Repository{OwnerID: owner.ID}))
	})

	t.Run("The person still has no repository of their own", func(t *testing.T) {
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/"), http.StatusOK).Body)
		page.AssertElement(t, ".site-started", true)
	})

	t.Run("A visitor is sent to sign in", func(t *testing.T) {
		resp := emptyTestSession(t).MakeRequest(t, NewRequest(t, "POST", "/-/get-started/sample"), http.StatusSeeOther)
		assert.Contains(t, test.RedirectURL(resp), "/user/login")
	})
}

// With [asc] SAMPLE_PROJECT off, or with the name taken by a person, the card links to the public projects and the
// sample is not made.
func TestAscGetStartedSampleOffOrTaken(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	ctx := t.Context()
	session := loginUser(t, "user8")

	t.Run("Off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.SampleProject, false)()
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started"), http.StatusOK).Body)
		assert.Equal(t, 0, page.Find(`form[data-testid="open-sample"]`).Length())
		assert.Equal(t, 1, page.Find(`.site-look-l a[href="/explore/repos"]`).Length())
		resp := session.MakeRequest(t, NewRequest(t, "POST", "/-/get-started/sample"), http.StatusSeeOther)
		assert.Equal(t, "/explore/repos", test.RedirectURL(resp))
		unittest.AssertNotExistsBean(t, &user_model.User{LowerName: ascsample.OwnerName})
	})

	t.Run("Taken", func(t *testing.T) {
		require.NoError(t, user_model.CreateUser(ctx, &user_model.User{Name: ascsample.OwnerName, Email: "taken@example.org", Passwd: "x-long-password", MustChangePassword: false}))
		resp := session.MakeRequest(t, NewRequest(t, "POST", "/-/get-started/sample"), http.StatusSeeOther)
		assert.Equal(t, "/explore/repos", test.RedirectURL(resp))
		assert.Contains(t, ascFlash(t, session), "could not be opened")
		owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{LowerName: ascsample.OwnerName})
		assert.Equal(t, 0, unittest.GetCount(t, &repo_model.Repository{OwnerID: owner.ID}))
		_, err := ascsample.Ensure(ctx)
		require.ErrorIs(t, err, ascsample.ErrNameTaken)
	})
}
