// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/ascbridge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `[stacks] collaborators_may_push` opens a stack to the writers of the repository; without it a writer who did
// not start the stack may update it only when the pull request allows maintainer edits.
func TestAscMayRestoreVersionCollaboratorsMayPush(t *testing.T) {
	pr, _, _, _ := setupAscRestore(t)
	require.NoError(t, pr.LoadBaseRepo(t.Context()))
	pr.AllowMaintainerEdit = false
	_, err := db.GetEngine(t.Context()).ID(pr.ID).Cols("allow_maintainer_edit").Update(pr)
	require.NoError(t, err)
	require.NoError(t, db.Insert(t.Context(), &repo_model.Collaboration{RepoID: pr.BaseRepoID, UserID: 4, Mode: perm.AccessModeWrite}))
	require.NoError(t, access_model.RecalculateUserAccess(t.Context(), pr.BaseRepo, 4))
	writer := ascTestUser(t, 4)
	reader := ascTestUser(t, 5)
	t.Cleanup(UseAscStackBacked(func(*repo_model.Repository) (bool, error) { return true, nil }))

	var rules ascbridge.StackRules
	var rulesErr error
	asked := 0
	t.Cleanup(ascbridge.UseStackRules(func(_ context.Context, repo string) (ascbridge.StackRules, error) {
		asked++
		assert.Equal(t, pr.BaseRepo.FullName(), repo)
		return rules, rulesErr
	}))
	may := func(u int64) bool {
		ok, err := AscMayRestoreVersion(t.Context(), pr, ascTestUser(t, u))
		require.NoError(t, err)
		return ok
	}

	assert.False(t, may(writer.ID), "off by default")
	rules = ascbridge.StackRules{CollaboratorsMayPush: true}
	assert.True(t, may(writer.ID))
	assert.False(t, may(reader.ID), "the switch opens a stack to writers only")

	rulesErr = errors.New("asc unavailable")
	assert.False(t, may(writer.ID), "an unreadable policy never opens a stack")

	// The poster's and an admin's right do not depend on the policy, so it is not read for them.
	asked = 0
	assert.True(t, may(1))
	assert.Zero(t, asked)
}
