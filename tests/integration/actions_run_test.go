// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"testing"

	actions_model "forgejo.org/models/actions"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionRunDeletion(t *testing.T) {
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	repo62 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 62, OwnerID: user2.ID})

	sessUser2 := loginUser(t, user2.Name)
	sessUser5 := loginUser(t, user5.Name)

	t.Run("Run removed", func(t *testing.T) {
		defer unittest.OverrideFixtures("tests/integration/fixtures/TestActionRunDeletion")()
		defer tests.PrepareTestEnv(t)()

		run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 34901})
		job := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunJob{RunID: run.ID})
		unittest.AssertCount(t, &actions_model.ActionArtifact{RunID: run.ID}, 2)
		runner := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRunner{ID: 41601})

		requestURL := fmt.Sprintf("/%s/actions/runs/%d/delete", repo1.FullName(), run.Index)
		request := NewRequest(t, "POST", requestURL)
		response := sessUser2.MakeRequest(t, request, http.StatusOK)
		assert.JSONEq(t, `{"ok":true}`, response.Body.String())

		unittest.AssertNotExistsBean(t, &actions_model.ActionRun{ID: run.ID})
		unittest.AssertNotExistsBean(t, &actions_model.ActionRunJob{ID: job.ID})
		unittest.AssertCount(t, &actions_model.ActionArtifact{
			RunID:  run.ID,
			Status: int64(actions_model.ArtifactStatusPendingDeletion),
		}, 2)
		unittest.AssertNotExistsBean(t, &actions_model.ActionRunner{ID: runner.ID})
	})

	t.Run("Error if run has not completed", func(t *testing.T) {
		defer unittest.OverrideFixtures("tests/integration/fixtures/TestActionRunDeletion")()
		defer tests.PrepareTestEnv(t)()

		run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 34902})

		requestURL := fmt.Sprintf("/%s/actions/runs/%d/delete", repo1.FullName(), run.Index)
		request := NewRequest(t, "POST", requestURL)
		response := sessUser2.MakeRequest(t, request, http.StatusInternalServerError)
		assert.JSONEq(t, `{"message":"Could not delete run."}`, response.Body.String())

		// Verify that the run still exists.
		unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: run.ID})
	})

	t.Run("Nothing happens if run does not belong to repository", func(t *testing.T) {
		defer unittest.OverrideFixtures("tests/integration/fixtures/TestActionRunDeletion")()
		defer tests.PrepareTestEnv(t)()

		run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 34901})

		requestURL := fmt.Sprintf("/%s/actions/runs/%d/delete", repo62.FullName(), run.Index)
		request := NewRequest(t, "POST", requestURL)
		response := sessUser2.MakeRequest(t, request, http.StatusOK)
		assert.JSONEq(t, `{"ok":true}`, response.Body.String())

		// Verify that the run still exists.
		unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: run.ID})
	})

	t.Run("Nothing happens if run does not exist", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		unittest.AssertNotExistsBean(t, &actions_model.ActionRun{Index: 260871})

		requestURL := fmt.Sprintf("/%s/actions/runs/%d/delete", repo1.FullName(), 260871)
		request := NewRequest(t, "POST", requestURL)
		response := sessUser2.MakeRequest(t, request, http.StatusOK)
		assert.JSONEq(t, `{"ok":true}`, response.Body.String())
	})

	t.Run("Removal requires ownership", func(t *testing.T) {
		defer unittest.OverrideFixtures("tests/integration/fixtures/TestActionRunDeletion")()
		defer tests.PrepareTestEnv(t)()

		isCollaborator, err := repo_model.IsCollaborator(t.Context(), repo1.ID, user5.ID)
		require.NoError(t, err)
		require.True(t, isCollaborator)

		run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 34901})

		requestURL := fmt.Sprintf("/%s/actions/runs/%d/delete", repo1.FullName(), run.Index)
		request := NewRequest(t, "POST", requestURL)
		MakeRequest(t, request, http.StatusNotFound)

		// Verify that run still exists
		unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: run.ID})

		request = NewRequest(t, "POST", requestURL)
		sessUser5.MakeRequest(t, request, http.StatusNotFound)

		// Verify that run still exists
		unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: run.ID})

		request = NewRequest(t, "POST", requestURL)
		sessUser2.MakeRequest(t, request, http.StatusOK)

		// Verify that run no longer exists
		unittest.AssertNotExistsBean(t, &actions_model.ActionRun{ID: run.ID})
	})
}
