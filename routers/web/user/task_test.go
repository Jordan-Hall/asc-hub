// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"
	"strconv"
	"testing"

	admin_model "forgejo.org/models/admin"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/translation"
	"forgejo.org/services/contexttest"
	repo_service "forgejo.org/services/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskStatusOf(t *testing.T, message string) map[string]any {
	t.Helper()
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	task := &admin_model.Task{
		DoerID: 2, OwnerID: 2, RepoID: 1, Type: structs.TaskTypeMigrateRepo, Status: structs.TaskStatusRunning,
		PayloadContent: `{"repo_name":"repo1"}`, Message: message,
	}
	require.NoError(t, db.Insert(db.DefaultContext, task))
	ctx, resp := contexttest.MockContext(t, "/user/task/"+strconv.FormatInt(task.ID, 10))
	ctx.Locale = translation.NewLocale("en-US")
	ctx.Doer = user2
	ctx.IsSigned = true
	ctx.SetParams(":task", strconv.FormatInt(task.ID, 10))
	TaskStatus(ctx)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	return out
}

func translatable(t *testing.T, format string, args ...any) string {
	t.Helper()
	bs, err := json.Marshal(admin_model.TranslatableMessage{Format: format, Args: args})
	require.NoError(t, err)
	return string(bs)
}

func TestTaskStatusCarriesTheHistoryImportProgress(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	translation.InitLocales(t.Context())

	out := taskStatusOf(t, translatable(t, repo_service.ImportProgressFormat, 3412, 9880))
	assert.InDelta(t, 3412, out["done"], 0)
	assert.InDelta(t, 9880, out["total"], 0)
	assert.Equal(t, "Importing history: 3,412 of 9,880 history entries", out["message"])

	// Any other message has no counts, and counts that make no sense are not shown as progress.
	for _, msg := range []string{
		translatable(t, "migrate.in_progress.git"),
		translatable(t, repo_service.ImportProgressFormat, 10, 0),
		translatable(t, repo_service.ImportProgressFormat, 11, 10),
		translatable(t, repo_service.ImportProgressFormat, -1, 10),
		translatable(t, repo_service.ImportProgressFormat, "1", "2"),
		"cloning",
	} {
		out := taskStatusOf(t, msg)
		assert.InDelta(t, 0, out["done"], 0, msg)
		assert.InDelta(t, 0, out["total"], 0, msg)
	}
}
