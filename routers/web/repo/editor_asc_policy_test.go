// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"
	files_service "forgejo.org/services/repository/files"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditorAnswersAPolicyEditThatWasProposed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	t.Run("with its pull request", func(t *testing.T) {
		ctx, resp := contexttest.MockContext(t, "user2/repo1/_edit/master/.asc/policy.toml")
		contexttest.LoadRepo(t, ctx, 1)
		handled := ascPolicyEditOutcome(ctx, &files_service.ErrAscPolicyProposed{Change: "kz4npqrstuvw", Link: "/user2/repo1/pulls/7"}, tplEditFile, nil)
		assert.True(t, handled)
		assert.Equal(t, http.StatusSeeOther, resp.Code)
		assert.Equal(t, "/user2/repo1/pulls/7", resp.Header().Get("Location"))
		assert.Contains(t, ctx.Flash.SuccessMsg, "repo.editor.asc_policy_proposed", "the flash is the localized sentence that names the change")
	})
	t.Run("before the git view has the change", func(t *testing.T) {
		ctx, resp := contexttest.MockContext(t, "user2/repo1/_edit/master/.asc/policy.toml")
		contexttest.LoadRepo(t, ctx, 1)
		handled := ascPolicyEditOutcome(ctx, &files_service.ErrAscPolicyProposed{Change: "kz4npqrstuvw"}, tplEditFile, nil)
		assert.True(t, handled)
		assert.Equal(t, "/user2/repo1/settings/asc/policy", resp.Header().Get("Location"))
	})
	t.Run("an error that is none of its business", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/_edit/master/a.txt")
		assert.False(t, ascPolicyEditOutcome(ctx, errors.New("boom"), tplEditFile, nil))
	})
}
