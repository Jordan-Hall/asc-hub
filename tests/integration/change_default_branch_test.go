// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/optional"
	repo_service "forgejo.org/services/repository"
	files_service "forgejo.org/services/repository/files"
	"forgejo.org/tests"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangeDefaultBranch(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})

	session := loginUser(t, owner.Name)
	branchesURL := fmt.Sprintf("/%s/%s/settings/branches", owner.Name, repo.Name)

	req := NewRequestWithValues(t, "POST", branchesURL, map[string]string{
		"action": "default_branch",
		"branch": "DefaultBranch",
	})
	session.MakeRequest(t, req, http.StatusSeeOther)

	req = NewRequestWithValues(t, "POST", branchesURL, map[string]string{
		"action": "default_branch",
		"branch": "does_not_exist",
	})
	session.MakeRequest(t, req, http.StatusNotFound)
}

func TestChangeDefaultBranchUpdatesSchedules(t *testing.T) {
	type expectedSpec struct {
		cron     string
		timeZone optional.Option[string]
	}

	expectedMainSpec := expectedSpec{
		cron:     "30 5,17 * * *",
		timeZone: optional.None[string](),
	}

	expectedTestSpec := expectedSpec{
		cron:     "0 * * * *",
		timeZone: optional.None[string](),
	}

	testWorkflow := struct {
		name                   string
		workflowID             string
		workflowDirectory      string
		workflowContent        string
		updatedWorkflowContent string
		expectedWorkflowTitle  string
	}{
		name:              "Forgejo",
		workflowID:        "scheduled.yml",
		workflowDirectory: ".forgejo/workflows",
		workflowContent: `
on:
  schedule:
    - cron: "30 5,17 * * *"
jobs:
  test:
    steps:
      - run: echo OK
`,
		updatedWorkflowContent: `
on:
  schedule:
    - cron: "0 * * * *"
jobs:
  test:
    steps:
      - run: echo updated
`,
		expectedWorkflowTitle: ".forgejo/workflows/scheduled.yml",
	}

	onApplicationRun(t, func(t *testing.T, u *url.URL) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

		// create repo
		var sha string
		repo := forgery.CreateRepository(t, user, &forgery.CreateRepositoryOptions{
			Files: forgery.MapFS{
				fmt.Sprintf("%s/%s", testWorkflow.workflowDirectory, testWorkflow.workflowID): forgery.MapFile(testWorkflow.workflowContent),
			},
			LatestSha: &sha,
		})

		gitRepo, err := gitrepo.OpenRepository(t.Context(), repo)
		require.NoError(t, err)
		defer gitRepo.Close()

		// create new branch
		err = repo_service.CreateNewBranch(t.Context(), user, repo, gitRepo, repo.DefaultBranch, "test")
		require.NoError(t, err)

		commit, err := gitRepo.GetBranchCommit("test")
		require.NoError(t, err)

		_, err = files_service.ChangeRepoFiles(
			t.Context(),
			repo,
			user,
			&files_service.ChangeRepoFilesOptions{
				LastCommitID: commit.ID.String(),
				OldBranch:    "test",
				NewBranch:    "test",
				Message:      "update workflow",
				Files: []*files_service.ChangeRepoFile{
					{
						Operation:     "update",
						TreePath:      testWorkflow.expectedWorkflowTitle,
						ContentReader: strings.NewReader(testWorkflow.updatedWorkflowContent),
					},
				},
			},
		)
		require.NoError(t, err)

		assertSchedule := func(t *testing.T, expectedRef, content string, spec expectedSpec) {
			t.Helper()
			schedules, err := db.Find[actions_model.ActionSchedule](t.Context(), actions_model.FindScheduleOptions{RepoID: repo.ID})

			require.NoError(t, err)
			require.Len(t, schedules, 1)

			assert.Equal(t, expectedRef, schedules[0].Ref)
			assert.Equal(t, testWorkflow.expectedWorkflowTitle, schedules[0].Title)
			assert.Equal(t, repo.ID, schedules[0].RepoID)
			assert.Equal(t, testWorkflow.workflowID, schedules[0].WorkflowID)
			assert.Equal(t, testWorkflow.workflowDirectory, schedules[0].WorkflowDirectory)
			assert.Equal(t, []byte(content), schedules[0].Content)

			specs, total, err := actions_model.FindSpecs(t.Context(), actions_model.FindSpecOptions{RepoID: repo.ID})

			require.NoError(t, err)
			require.Equal(t, int64(1), total)
			require.Len(t, specs, 1)

			assert.Equal(t, schedules[0].ID, specs[0].ScheduleID)
			assert.Equal(t, spec.cron, specs[0].Spec)
			assert.Equal(t, spec.timeZone, specs[0].TimeZone)
		}

		// change default branch to test
		err = repo_service.SetRepoDefaultBranch(t.Context(), repo, gitRepo, "test")
		require.NoError(t, err)

		assertSchedule(t, "refs/heads/test", testWorkflow.updatedWorkflowContent, expectedTestSpec)

		// change default branch to main
		err = repo_service.SetRepoDefaultBranch(t.Context(), repo, gitRepo, "main")
		require.NoError(t, err)

		assertSchedule(t, "refs/heads/main", testWorkflow.workflowContent, expectedMainSpec)
	})
}
