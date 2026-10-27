// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	"forgejo.org/models/perm"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

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

// Request level and credential level inputs move the version too, so an answer kept past a rule or credential change
// is recognised as another one.
func TestAscAuthzPermVersionCoversTheRequestAndTheCredential(t *testing.T) {
	version := func(r private.AscAuthzRequest) uint64 {
		t.Helper()
		resp := ascAuthzDo(t, r)
		require.True(t, resp.Allowed)
		return resp.PermVersion
	}

	t.Run("a branch rule", func(t *testing.T) {
		ascAuthzSetup(t)
		tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
		req := private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "write", Ref: "refs/heads/master"}
		none := version(req)
		rule := &git_model.ProtectedBranch{RepoID: 1, RuleName: "master"}
		_, err := db.GetEngine(db.DefaultContext).Insert(rule)
		require.NoError(t, err)
		withRule := version(req)
		assert.NotEqual(t, none, withRule, "a rule appears")
		_, err = db.GetEngine(db.DefaultContext).ID(rule.ID).Cols("require_signed_commits").Update(&git_model.ProtectedBranch{RequireSignedCommits: true})
		require.NoError(t, err)
		assert.NotEqual(t, withRule, version(req), "a rule changes")
		req.Ref = "refs/heads/other"
		assert.NotEqual(t, withRule, version(req), "another ref is another answer")
	})

	t.Run("a token", func(t *testing.T) {
		ascAuthzSetup(t)
		tok := &auth_model.AccessToken{UID: 2, Name: "asc-v", Scope: auth_model.AccessTokenScopeAll, ResourceAllRepos: true}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, tok))
		req := private.AscAuthzRequest{Token: tok.Token, Owner: "user2", Repo: "repo1", Mode: "read"}
		before := version(req)
		// The same person with another token of the same scope is another credential.
		other := &auth_model.AccessToken{UID: 2, Name: "asc-v2", Scope: auth_model.AccessTokenScopeAll, ResourceAllRepos: true}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, other))
		assert.NotEqual(t, before, version(private.AscAuthzRequest{Token: other.Token, Owner: "user2", Repo: "repo1", Mode: "read"}))
		// A change of the stored token (its scope) moves the value, and a revoked token is refused.
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE access_token SET scope = ?, updated_unix = updated_unix + 1 WHERE id = ?", string(auth_model.AccessTokenScopeReadRepository), tok.ID)
		require.NoError(t, err)
		assert.NotEqual(t, before, version(req))
		_, err = db.GetEngine(db.DefaultContext).Exec("DELETE FROM access_token WHERE id = ?", tok.ID)
		require.NoError(t, err)
		revoked := ascAuthzDo(t, req)
		assert.False(t, revoked.Allowed)
		assert.Zero(t, revoked.PermVersion, "a refusal has no version")
	})

	t.Run("two-factor authentication", func(t *testing.T) {
		ascAuthzSetup(t)
		tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
		req := private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "read"}
		off := version(req)
		require.NoError(t, db.Insert(db.DefaultContext, &auth_model.TwoFactor{UID: 2, Secret: []byte("x")}))
		enrolled := version(req)
		assert.NotEqual(t, off, enrolled, "enrolment")
		defer test.MockVariableValue(&setting.GlobalTwoFactorRequirement, setting.NewTwoFactorRequirementType("all"))()
		assert.NotEqual(t, enrolled, version(req), "the requirement")
	})

	t.Run("the status of an Actions task", func(t *testing.T) {
		ascAuthzSetup(t)
		req := private.AscAuthzRequest{Token: ascTaskToken, Owner: "user5", Repo: "repo4", Mode: "read"}
		running := version(req)
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE action_task SET stopped = ? WHERE id = ?", 1700000000, 47)
		require.NoError(t, err)
		resp := ascAuthzDo(t, req)
		if resp.Allowed {
			assert.NotEqual(t, running, resp.PermVersion)
		}
	})
}
