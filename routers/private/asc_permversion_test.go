// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/perm"
	"forgejo.org/modules/private"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The version is the fingerprint of what the decision rested on: the same while nothing that matters changes, another
// as soon as a collaborator, a team, the visibility, the name, the owner, the account or the scope of the token does.
func TestAscAuthzPermVersion(t *testing.T) {
	ascAuthzSetup(t)
	tok5 := ascNewToken(t, 5, auth_model.AccessTokenScopeAll)
	version := func(token, owner, repo string) uint64 {
		t.Helper()
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: token, Owner: owner, Repo: repo, Mode: "read"})
		require.True(t, resp.Allowed)
		require.NotZero(t, resp.PermVersion)
		return resp.PermVersion
	}

	base := version(tok5, "user2", "repo1")
	assert.Equal(t, base, version(tok5, "user2", "repo1"), "nothing changed")

	// Another asker is another version.
	anon := ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo1", Mode: "read"})
	require.True(t, anon.Allowed)
	assert.NotEqual(t, base, anon.PermVersion, "the asker is part of it")

	changes := map[string]func(){
		"a collaborator": func() { collaborate(t, 1, 5, perm.AccessModeWrite) },
		"the visibility": func() {
			sql(t, "UPDATE repository SET is_private = ? WHERE id = 1", true)
			collaborate(t, 1, 5, perm.AccessModeRead)
		},
		"the archive":     func() { sql(t, "UPDATE repository SET is_archived = ? WHERE id = 1", true) },
		"a rename":        func() { sql(t, "UPDATE repository SET lower_name = ?, name = ? WHERE id = 1", "renamed", "renamed") },
		"a transfer":      func() { sql(t, "UPDATE repository SET owner_name = ?, owner_id = 9 WHERE id = 1", "user9") },
		"the account":     func() { sql(t, "UPDATE `user` SET is_restricted = ? WHERE id = 5", true) },
		"the unit access": func() { sql(t, "DELETE FROM repo_unit WHERE repo_id = 1 AND type = 2") },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			ascAuthzSetup(t)
			tok := ascNewToken(t, 5, auth_model.AccessTokenScopeAll)
			before := ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "read"})
			require.True(t, before.Allowed)
			change()
			owner, repo := "user2", "repo1"
			switch name {
			case "a rename":
				repo = "renamed"
			case "a transfer":
				owner = "user9"
			}
			after := ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: owner, Repo: repo, Mode: "read"})
			if !after.Allowed {
				return // the change took the access away; a refusal carries no version
			}
			assert.NotEqual(t, before.PermVersion, after.PermVersion)
		})
	}

	t.Run("the scope of the token", func(t *testing.T) {
		ascAuthzSetup(t)
		all := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
		readOnly := ascNewToken(t, 2, auth_model.AccessTokenScopeReadRepository)
		assert.NotEqual(t, version(all, "user2", "repo1"), version(readOnly, "user2", "repo1"))
	})
}
