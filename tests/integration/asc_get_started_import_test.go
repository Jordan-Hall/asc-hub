// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	admin_model "forgejo.org/models/admin"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	migrations_allowlist "forgejo.org/services/migrations/allowlist"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ascImportAnswer struct {
	Repo  string `json:"repo"`
	Link  string `json:"link"`
	Error string `json:"error"`
}

type ascImportStateAnswer struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Commits int64  `json:"commits"`
	Count   string `json:"count"`
	Link    string `json:"link"`
}

func postGetStartedImport(t *testing.T, session *TestSession, addr string, status int) ascImportAnswer {
	t.Helper()
	req := NewRequestWithValues(t, "POST", "/-/get-started/import", map[string]string{
		"clone_addr": addr,
		"inline":     "1",
	})
	resp := session.MakeRequest(t, req, status)
	var got ascImportAnswer
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	return got
}

func getImportStatus(t *testing.T, session *TestSession, name string, status int) ascImportStateAnswer {
	t.Helper()
	resp := session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started/import/status?repo="+url.QueryEscape(name)), status)
	var got ascImportStateAnswer
	if status == http.StatusOK {
		assert.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	}
	return got
}

// TestAscGetStartedImport checks the import of step 3 of Get started (board GetStarted): the page has the form, an
// address that is not https is refused, an https address queues a private migration owned by the person, and the
// status route follows the task up to the number of commits read.
func TestAscGetStartedImport(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	// the import reads from a loopback port where nothing listens, so the queued task fails on its own
	defer test.MockVariableValue(&setting.Migrations.AllowLocalNetworks, true)()
	require.NoError(t, migrations_allowlist.Init())
	defer func() { require.NoError(t, migrations_allowlist.Init()) }()

	// user8 has no repository yet, so step 3 offers the import
	user8 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 8})
	session := loginUser(t, user8.Name)

	page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/-/get-started"), http.StatusOK).Body)
	page.AssertElement(t, "form[data-gs-imp] input[name=clone_addr][type=url]", true)
	page.AssertElement(t, "[data-gs-imp-open]", true)
	page.AssertElement(t, "[data-gs-imp-bar]", true)
	lost, _ := page.Find("form[data-gs-imp]").Attr("data-t-lost")
	assert.NotEmpty(t, lost)

	t.Run("RefusesPlainHTTP", func(t *testing.T) {
		got := postGetStartedImport(t, session, "http://nova.invalid/acme/nova.git", http.StatusUnprocessableEntity)
		assert.Equal(t, "Use an https address.", got.Error)
		unittest.AssertNotExistsBean(t, &repo_model.Repository{OwnerID: user8.ID, LowerName: "nova"})
	})

	t.Run("QueuesTheImport", func(t *testing.T) {
		got := postGetStartedImport(t, session, "https://127.0.0.1:9/acme/nova.git", http.StatusOK)
		assert.Empty(t, got.Error)
		assert.Equal(t, "nova", got.Repo)
		assert.Equal(t, "/user8/nova", got.Link)
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: user8.ID, LowerName: "nova"})
		assert.True(t, repo.IsPrivate)
		task := unittest.AssertExistsAndLoadBean(t, &admin_model.Task{RepoID: repo.ID, Type: structs.TaskTypeMigrateRepo})
		assert.Equal(t, user8.ID, task.DoerID)

		st := getImportStatus(t, session, "nova", http.StatusOK)
		assert.Contains(t, []string{"queued", "running", "failed"}, st.Status)
		assert.Equal(t, "/user8/nova", st.Link)
	})

	t.Run("RefusesATakenName", func(t *testing.T) {
		got := postGetStartedImport(t, session, "https://127.0.0.1:9/orbit/nova.git", http.StatusUnprocessableEntity)
		assert.Equal(t, "The repository name is already used.", got.Error)
	})

	// user2 owns repositories with history in the fixtures
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	owner := loginUser(t, user2.Name)

	t.Run("CountsTheCommitsOnceDone", func(t *testing.T) {
		repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		task := &admin_model.Task{DoerID: user2.ID, OwnerID: user2.ID, RepoID: repo1.ID, Type: structs.TaskTypeMigrateRepo, Status: structs.TaskStatusFinished}
		require.NoError(t, db.Insert(db.DefaultContext, task))

		st := getImportStatus(t, owner, repo1.Name, http.StatusOK)
		assert.Equal(t, "done", st.Status)
		assert.Positive(t, st.Commits)
		if st.Commits == 1 {
			assert.Equal(t, "1 commit", st.Count)
		} else {
			assert.Contains(t, st.Count, " commits")
		}

		// a task that another person started is not shown
		task.DoerID = 1
		_, err := db.GetEngine(db.DefaultContext).ID(task.ID).Cols("doer_id").Update(task)
		require.NoError(t, err)
		getImportStatus(t, owner, repo1.Name, http.StatusNotFound)
	})

	t.Run("FailedTaskGivesTheReason", func(t *testing.T) {
		repo2 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
		task := &admin_model.Task{DoerID: user2.ID, OwnerID: user2.ID, RepoID: repo2.ID, Type: structs.TaskTypeMigrateRepo, Status: structs.TaskStatusFailed, Message: "authentication failed"}
		require.NoError(t, db.Insert(db.DefaultContext, task))

		st := getImportStatus(t, owner, repo2.Name, http.StatusOK)
		assert.Equal(t, "failed", st.Status)
		assert.Equal(t, "authentication failed", st.Message)
	})

	t.Run("NoTaskIsNotFound", func(t *testing.T) {
		getImportStatus(t, owner, "repo16", http.StatusNotFound)
		getImportStatus(t, owner, "no-such-repo", http.StatusNotFound)
	})

	t.Run("NeedsASignIn", func(t *testing.T) {
		resp := emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", "/-/get-started/import/status?repo=nova"), http.StatusSeeOther)
		assert.Contains(t, resp.Header().Get("Location"), "/user/login")
	})
}
