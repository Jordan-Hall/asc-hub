// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getAscChangesetNew(t *testing.T, doer *user_model.User) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/-/asc/changeset/new")
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	ctx.Data["IsSigned"] = ctx.IsSigned
	ctx.Data["SignedUser"] = doer
	AscChangesetNew(ctx)
	return resp.Code, resp.Body.String()
}

// Board Shell, Create menu "New changeset": the asc repositories the person can write, each opening its editor.
func TestAscChangesetNew(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	setAscBackend(t, 1, "asc")

	code, body := getAscChangesetNew(t, user2)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-testid="asc-changeset-new-repos"`)
	assert.Contains(t, body, `href="/user2/repo1/_new/master/"`)
	assert.NotContains(t, body, "/user2/repo2/_new/", "a repository asc does not store is not offered")
	assert.Regexp(t, `href="/-/asc/changeset/new" data-testid="asc-create-changeset">.*asc_shell.create_changeset</a>\s*<hr>\s*<a class="ds-mi" href="/-/asc/agents">`, body, "the Create menu has it before the agent run, the two parted by a rule")

	t.Run("a mirror cannot be edited", func(t *testing.T) {
		_, err := db.GetEngine(db.DefaultContext).ID(1).Cols("is_mirror").Update(&repo_model.Repository{IsMirror: true})
		require.NoError(t, err)
		defer func() {
			_, err := db.GetEngine(db.DefaultContext).ID(1).Cols("is_mirror").Update(&repo_model.Repository{IsMirror: false})
			require.NoError(t, err)
		}()
		_, body := getAscChangesetNew(t, user2)
		assert.NotContains(t, body, "/user2/repo1/_new/")
		assert.Contains(t, body, "asc_shell.changeset_new_empty")
		assert.Contains(t, body, `href="/repo/create"`)
	})
	t.Run("a collaborator who can only read gets no editor, one who can write does", func(t *testing.T) {
		user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
		require.NoError(t, db.Insert(db.DefaultContext, &repo_model.Collaboration{RepoID: 1, UserID: 5, Mode: perm.AccessModeRead}))
		require.NoError(t, db.Insert(db.DefaultContext, &access_model.Access{RepoID: 1, UserID: 5, Mode: perm.AccessModeRead}))
		_, body := getAscChangesetNew(t, user5)
		assert.NotContains(t, body, "/user2/repo1/_new/")

		_, err := db.GetEngine(db.DefaultContext).Where("repo_id = 1 AND user_id = 5").Cols("mode").Update(&access_model.Access{Mode: perm.AccessModeWrite})
		require.NoError(t, err)
		_, err = db.GetEngine(db.DefaultContext).Where("repo_id = 1 AND user_id = 5").Cols("mode").Update(&repo_model.Collaboration{Mode: perm.AccessModeWrite})
		require.NoError(t, err)
		_, body = getAscChangesetNew(t, user5)
		assert.Contains(t, body, `href="/user2/repo1/_new/master/"`)
	})
	t.Run("anonymous and disabled", func(t *testing.T) {
		code, _ := getAscChangesetNew(t, nil)
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _ = getAscChangesetNew(t, user2)
		assert.Equal(t, http.StatusNotFound, code)
	})
}
