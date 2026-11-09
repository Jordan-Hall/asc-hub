// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package secret

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascscope"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A secret reaches a run only when its scopes include the run's; a repository secret the run may not have hides
// the owner secret of the same name, so a narrower repository secret never falls back to a wider owner one.
func TestFetchActionSecretsByScope(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	insert := func(ownerID, repoID int64, name, data string, scopes ascscope.Scope) {
		s, err := InsertEncryptedSecret(ctx, ownerID, repoID, name, data)
		require.NoError(t, err)
		assert.Equal(t, ascscope.DefaultScope, s.Scopes, "a new secret has Checks and Releases")
		if scopes != ascscope.DefaultScope {
			require.NoError(t, SetScopes(ctx, s.ID, scopes))
		}
	}
	insert(2, 0, "PLAIN", "owner plain", ascscope.DefaultScope)
	insert(2, 0, "FOR_AGENTS", "owner agents", ascscope.AllScopes)
	insert(2, 0, "SHADOWED", "owner wide", ascscope.AllScopes)
	insert(0, 1, "SHADOWED", "repo releases", ascscope.ScopeReleases)
	insert(0, 1, "AGENTS_ONLY", "repo agents", ascscope.ScopeAgents)

	checks, err := FetchActionSecrets(ctx, 2, 1, ascscope.ScopeChecks)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"PLAIN": "owner plain", "FOR_AGENTS": "owner agents"}, checks)

	agents, err := FetchActionSecrets(ctx, 2, 1, ascscope.ScopeAgents)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FOR_AGENTS": "owner agents", "AGENTS_ONLY": "repo agents"}, agents)

	releases, err := FetchActionSecrets(ctx, 2, 1, ascscope.ScopeReleases)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"PLAIN": "owner plain", "FOR_AGENTS": "owner agents", "SHADOWED": "repo releases"}, releases)
}

func TestSetScopes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	s, err := InsertEncryptedSecret(t.Context(), 0, 1, "SCOPED", "value")
	require.NoError(t, err)

	require.NoError(t, SetScopes(t.Context(), s.ID, ascscope.ScopeAgents|ascscope.ScopeChecks))
	got := unittest.AssertExistsAndLoadBean(t, &Secret{ID: s.ID})
	assert.Equal(t, ascscope.ScopeAgents|ascscope.ScopeChecks, got.Scopes)
	assert.Equal(t, s.Data, got.Data, "the value stays")

	require.ErrorIs(t, SetScopes(t.Context(), s.ID, 0), util.ErrInvalidArgument)
	require.ErrorIs(t, SetScopes(t.Context(), s.ID, 8), util.ErrInvalidArgument)
	got = unittest.AssertExistsAndLoadBean(t, &Secret{ID: s.ID})
	assert.Equal(t, ascscope.ScopeAgents|ascscope.ScopeChecks, got.Scopes, "a refused set changes nothing")
}
