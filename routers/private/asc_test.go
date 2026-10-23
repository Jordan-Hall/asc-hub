// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	asymkey_model "forgejo.org/models/asymkey"
	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// The running task 47 of repo4 (models/fixtures/action_task.yml).
	ascTaskToken = "8061e833a55f6fc0157c98b883e91fcfeeb1a71a"

	ascDeployKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHnbqnHNKh/Td/1O6t9Q8qdJmitCAApnvHImHV8TkptX deploy@example.com"
)

// ascAuthzCall posts a raw body to the endpoint and returns the status and body.
func ascAuthzCall(t *testing.T, body string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/authz")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	AscAuthz(ctx)
	return resp.Code, resp.Body.String()
}

func ascAuthzDo(t *testing.T, req private.AscAuthzRequest) private.AscAuthzResponse {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	code, out := ascAuthzCall(t, string(body))
	require.Equal(t, http.StatusOK, code, out)
	var resp private.AscAuthzResponse
	require.NoError(t, json.Unmarshal([]byte(out), &resp))
	return resp
}

// ascNewToken creates a personal access token with the given scope.
func ascNewToken(t *testing.T, uid int64, scope auth_model.AccessTokenScope) string {
	t.Helper()
	token := &auth_model.AccessToken{UID: uid, Name: "asc-" + string(scope), Scope: scope, ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, token))
	return token.Token
}

func ascAuthzSetup(t *testing.T) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
}

func TestAscAuthzDisabled(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()

	code, _ := ascAuthzCall(t, `{"owner":"user2","repo":"repo1","mode":"read"}`)
	assert.Equal(t, http.StatusNotFound, code)
}

func TestAscAuthzBadRequest(t *testing.T) {
	ascAuthzSetup(t)

	for name, body := range map[string]string{
		"empty":            ``,
		"not json":         `nope`,
		"unknown field":    `{"owner":"user2","repo":"repo1","mode":"read","admin":true}`,
		"trailing data":    `{"owner":"user2","repo":"repo1","mode":"read"}{}`,
		"bad mode":         `{"owner":"user2","repo":"repo1","mode":"admin"}`,
		"no mode":          `{"owner":"user2","repo":"repo1"}`,
		"no owner":         `{"repo":"repo1","mode":"read"}`,
		"no repo":          `{"owner":"user2","mode":"read"}`,
		"long owner":       `{"owner":"` + strings.Repeat("a", 256) + `","repo":"repo1","mode":"read"}`,
		"long ref":         `{"owner":"user2","repo":"repo1","mode":"read","ref":"` + strings.Repeat("a", 1025) + `"}`,
		"token and key":    `{"owner":"user2","repo":"repo1","mode":"read","token":"abc","key_id":1}`,
		"negative key":     `{"owner":"user2","repo":"repo1","mode":"read","key_id":-1}`,
		"long token":       `{"owner":"user2","repo":"repo1","mode":"read","token":"` + strings.Repeat("a", 4097) + `"}`,
		"token with space": `{"owner":"user2","repo":"repo1","mode":"read","token":"a b"}`,
		"token with ctl":   `{"owner":"user2","repo":"repo1","mode":"read","token":"a\r\nb"}`,
		"user no token":    `{"owner":"user2","repo":"repo1","mode":"read","user":"user2"}`,
		"user colon":       `{"owner":"user2","repo":"repo1","mode":"read","user":"a:b","token":"abc"}`,
		"user ctl": `{"owner":"user2","repo":"repo1","mode":"read","user":"a
b","token":"abc"}`,
		"user long":     `{"owner":"user2","repo":"repo1","mode":"read","user":"` + strings.Repeat("a", 256) + `","token":"abc"}`,
		"user and key":  `{"owner":"user2","repo":"repo1","mode":"read","user":"user2","key_id":1}`,
		"password ctl":  `{"owner":"user2","repo":"repo1","mode":"read","user":"user2","token":"a\u0000b"}`,
		"oversize body": `{"owner":"user2","repo":"repo1","mode":"read","ref":"` + strings.Repeat("a", private.AscAuthzMaxBodySize) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			code, _ := ascAuthzCall(t, body)
			assert.Equal(t, http.StatusBadRequest, code)
		})
	}
}

func TestAscAuthzAnonymous(t *testing.T) {
	ascAuthzSetup(t)

	// repo1 is public: read only.
	resp := ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo1", Mode: "read"})
	assert.True(t, resp.Allowed)
	assert.Equal(t, "read", resp.Perm)
	assert.Zero(t, resp.UserID)
	assert.Empty(t, resp.Username)
	assert.False(t, resp.IsActionsTask)
	assert.Nil(t, resp.Task)

	assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo1", Mode: "write"}).Allowed)

	t.Run("sign-in required", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.RequireSignInView, true)()
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed)
	})
}

func TestAscAuthzDenialsLookAlike(t *testing.T) {
	ascAuthzSetup(t)

	call := func(owner, repo string) string {
		body, err := json.Marshal(private.AscAuthzRequest{Owner: owner, Repo: repo, Mode: "read"})
		require.NoError(t, err)
		code, out := ascAuthzCall(t, string(body))
		require.Equal(t, http.StatusOK, code)
		return out
	}
	// A private repo, a missing repo and a missing owner answer the same.
	denied := call("user2", "repo2")
	assert.Equal(t, denied, call("user2", "does-not-exist"))
	assert.Equal(t, denied, call("nobody-here", "repo2"))
	assert.Contains(t, denied, `"allowed":false`)
}

func TestAscAuthzToken(t *testing.T) {
	ascAuthzSetup(t)
	tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)

	t.Run("owner reads and writes a private repo", func(t *testing.T) {
		for _, mode := range []string{"read", "write"} {
			resp := ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo2", Mode: mode})
			assert.True(t, resp.Allowed, mode)
			assert.EqualValues(t, 2, resp.UserID)
			assert.Equal(t, "user2", resp.Username)
			assert.Equal(t, "owner", resp.Perm)
			assert.Zero(t, resp.DeployKeyID)
			assert.False(t, resp.IsActionsTask)
		}
	})

	t.Run("no access to another private repo", func(t *testing.T) {
		other := ascNewToken(t, 9, auth_model.AccessTokenScopeAll)
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: other, Owner: "user2", Repo: "repo2", Mode: "read"})
		assert.False(t, resp.Allowed)
	})

	t.Run("scopes limit the token", func(t *testing.T) {
		readOnly := ascNewToken(t, 2, auth_model.AccessTokenScopeReadRepository)
		assert.True(t, ascAuthzDo(t, private.AscAuthzRequest{Token: readOnly, Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: readOnly, Owner: "user2", Repo: "repo2", Mode: "write"}).Allowed)

		write := ascNewToken(t, 2, auth_model.AccessTokenScopeWriteRepository)
		assert.True(t, ascAuthzDo(t, private.AscAuthzRequest{Token: write, Owner: "user2", Repo: "repo2", Mode: "write"}).Allowed)

		wrongScope := ascNewToken(t, 2, auth_model.AccessTokenScopeReadUser)
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: wrongScope, Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed)

		publicOnly := ascNewToken(t, 2, auth_model.AccessTokenScope(string(auth_model.AccessTokenScopeReadRepository)+","+string(auth_model.AccessTokenScopePublicOnly)))
		assert.True(t, ascAuthzDo(t, private.AscAuthzRequest{Token: publicOnly, Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed)
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: publicOnly, Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
	})

	t.Run("a token limited to specific repos, with none listed", func(t *testing.T) {
		limited := &auth_model.AccessToken{UID: 2, Name: "asc-limited", Scope: auth_model.AccessTokenScopeAll}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, limited))
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: limited.Token, Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
	})

	t.Run("a bad token is denied, not an anonymous read", func(t *testing.T) {
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: strings.Repeat("0", 40), Owner: "user2", Repo: "repo1", Mode: "read"})
		assert.False(t, resp.Allowed)
	})

	t.Run("a password is not a token", func(t *testing.T) {
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: "user2:password", Owner: "user2", Repo: "repo1", Mode: "read"})
		assert.False(t, resp.Allowed)
	})

	t.Run("archived and mirror repos are read only", func(t *testing.T) {
		_, err := db.GetEngine(db.DefaultContext).ID(2).Cols("is_archived").Update(&repo_model.Repository{IsArchived: true})
		require.NoError(t, err)
		assert.True(t, ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo2", Mode: "write"}).Allowed)
	})

}

func TestAscAuthzProhibitedUser(t *testing.T) {
	ascAuthzSetup(t)
	tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)

	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = ?", true, 2)
	require.NoError(t, err)
	resp := ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "read"})
	assert.False(t, resp.Allowed)
}

func TestAscAuthzActionsTask(t *testing.T) {
	ascAuthzSetup(t)

	// Task 47 runs in repo4 (user5), run 791, event push on master.
	resp := ascAuthzDo(t, private.AscAuthzRequest{Token: ascTaskToken, Owner: "user5", Repo: "repo4", Mode: "write"})
	require.True(t, resp.Allowed)
	assert.True(t, resp.IsActionsTask)
	require.NotNil(t, resp.Task)
	assert.EqualValues(t, 791, resp.Task.RunID)
	assert.Equal(t, "user5/repo4", resp.Task.Repo)
	assert.Equal(t, "push", resp.Task.Event)
	assert.Equal(t, "refs/heads/master", resp.Task.Ref)
	assert.Zero(t, resp.Task.PRIndex)
	assert.False(t, resp.Task.IsForkPR)
	assert.EqualValues(t, -2, resp.UserID)

	// Another private repo is out of reach for the task token.
	assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: ascTaskToken, Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)

	// A fork pull request task is read only.
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE action_task SET is_fork_pull_request = ? WHERE id = ?", true, 47)
	require.NoError(t, err)
	assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: ascTaskToken, Owner: "user5", Repo: "repo4", Mode: "write"}).Allowed)
	resp = ascAuthzDo(t, private.AscAuthzRequest{Token: ascTaskToken, Owner: "user5", Repo: "repo4", Mode: "read"})
	assert.True(t, resp.Allowed)
	assert.True(t, resp.Task.IsForkPR)
}

func TestAscAuthzKeys(t *testing.T) {
	ascAuthzSetup(t)

	t.Run("user key", func(t *testing.T) {
		resp := ascAuthzDo(t, private.AscAuthzRequest{KeyID: 1, Owner: "user2", Repo: "repo2", Mode: "write"})
		assert.True(t, resp.Allowed)
		assert.EqualValues(t, 2, resp.UserID)
		assert.Zero(t, resp.DeployKeyID)

		// user9 has no access to user2/repo2.
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{KeyID: 3, Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{KeyID: 999999, Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed)
	})

	t.Run("read-only deploy key", func(t *testing.T) {
		dk, err := asymkey_model.AddDeployKey(db.DefaultContext, 2, "dk", ascDeployKey, true)
		require.NoError(t, err)

		resp := ascAuthzDo(t, private.AscAuthzRequest{KeyID: dk.KeyID, Owner: "user2", Repo: "repo2", Mode: "read"})
		assert.True(t, resp.Allowed)
		assert.Equal(t, dk.ID, resp.DeployKeyID)
		assert.Equal(t, "user2", resp.Username)
		assert.Equal(t, "read", resp.Perm)

		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{KeyID: dk.KeyID, Owner: "user2", Repo: "repo2", Mode: "write"}).Allowed)
		// The key belongs to repo2 only.
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{KeyID: dk.KeyID, Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed)
	})
}

func TestAscAuthzBranchRules(t *testing.T) {
	ascAuthzSetup(t)
	tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)

	_, err := db.GetEngine(db.DefaultContext).Insert(&git_model.ProtectedBranch{RepoID: 1, RuleName: "master", RequireSignedCommits: true})
	require.NoError(t, err)
	_, err = db.GetEngine(db.DefaultContext).Insert(&git_model.ProtectedBranch{RepoID: 1, RuleName: "feature/*"})
	require.NoError(t, err)

	req := private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "write"}
	assert.False(t, ascAuthzDo(t, req).BranchRules.RequireSigned, "no ref")

	req.Ref = "master"
	assert.True(t, ascAuthzDo(t, req).BranchRules.RequireSigned)
	req.Ref = "refs/heads/master"
	assert.True(t, ascAuthzDo(t, req).BranchRules.RequireSigned)
	req.Ref = "feature/x"
	assert.False(t, ascAuthzDo(t, req).BranchRules.RequireSigned)
	req.Ref = "other"
	assert.False(t, ascAuthzDo(t, req).BranchRules.RequireSigned)

	// The body of a denial carries no rule data.
	denied := ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo2", Mode: "read", Ref: "master"})
	assert.Equal(t, private.AscAuthzResponse{}, denied)
}

func TestAscAuthzRoundTripShape(t *testing.T) {
	ascAuthzSetup(t)
	tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)

	body, err := json.Marshal(private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "read"})
	require.NoError(t, err)
	code, out := ascAuthzCall(t, string(body))
	require.Equal(t, http.StatusOK, code)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace([]byte(out)), &generic))
	for _, key := range []string{"allowed", "user_id", "username", "perm", "deploy_key_id", "is_actions_task", "task", "branch_rules"} {
		assert.Contains(t, generic, key)
	}
	assert.Equal(t, map[string]any{"require_signed": false}, generic["branch_rules"])
}

func TestAscAuthzBasic(t *testing.T) {
	ascAuthzSetup(t)
	// Each case starts from the fixtures, as several of them edit users.
	run := func(name string, f func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			ascAuthzSetup(t)
			// Password sign-in is a setting; the unit-test configuration leaves it off.
			t.Cleanup(test.MockVariableValue(&setting.Service.EnableBasicAuth, true))
			f(t)
		})
	}
	basic := func(user, token, owner, repo, mode string) private.AscAuthzResponse {
		return ascAuthzDo(t, private.AscAuthzRequest{User: user, Token: token, Owner: owner, Repo: repo, Mode: mode})
	}

	run("personal access token as the password", func(t *testing.T) {
		tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
		// The user name is a hint only, as for git.
		for _, user := range []string{"user2", "x-access-token", "someone-else"} {
			for _, mode := range []string{"read", "write"} {
				resp := basic(user, tok, "user2", "repo2", mode)
				assert.True(t, resp.Allowed, user+" "+mode)
				assert.EqualValues(t, 2, resp.UserID)
				assert.Equal(t, "user2", resp.Username)
				assert.Equal(t, "owner", resp.Perm)
			}
		}
	})

	run("token scopes still apply", func(t *testing.T) {
		readOnly := ascNewToken(t, 2, auth_model.AccessTokenScopeReadRepository)
		assert.True(t, basic("user2", readOnly, "user2", "repo2", "read").Allowed)
		assert.False(t, basic("user2", readOnly, "user2", "repo2", "write").Allowed)
		other := ascNewToken(t, 4, auth_model.AccessTokenScopeAll)
		assert.False(t, basic("user4", other, "user2", "repo2", "read").Allowed)
	})

	run("account password", func(t *testing.T) {
		for _, mode := range []string{"read", "write"} {
			resp := basic("user2", "password", "user2", "repo2", mode)
			assert.True(t, resp.Allowed, mode)
			assert.EqualValues(t, 2, resp.UserID)
			assert.Equal(t, "user2", resp.Username)
			assert.Equal(t, "owner", resp.Perm)
			assert.False(t, resp.IsActionsTask)
		}
		// By e-mail address, as on the web sign-in.
		assert.True(t, basic("user2@example.com", "password", "user2", "repo2", "read").Allowed)
		// The password of one user gives no access to another user's repository.
		assert.False(t, basic("user4", "password", "user2", "repo2", "read").Allowed)
	})

	run("password with spaces and non-ASCII text", func(t *testing.T) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		const password = "pa ss \u00fc\u4e16 word"
		require.NoError(t, user.SetPassword(password))
		require.NoError(t, user_model.UpdateUserCols(db.DefaultContext, user, "passwd", "passwd_hash_algo", "salt"))
		assert.True(t, basic("user2", password, "user2", "repo2", "write").Allowed)
		// The same text is refused as a bearer token: no user, no password.
		code, _ := ascAuthzCall(t, `{"owner":"user2","repo":"repo2","mode":"read","token":"pa ss"}`)
		assert.Equal(t, http.StatusBadRequest, code)
	})

	run("wrong password and unknown user look alike", func(t *testing.T) {
		call := func(user, token string) string {
			body, err := json.Marshal(private.AscAuthzRequest{User: user, Token: token, Owner: "user2", Repo: "repo1", Mode: "read"})
			require.NoError(t, err)
			code, out := ascAuthzCall(t, string(body))
			require.Equal(t, http.StatusOK, code)
			return out
		}
		denied := call("user2", "not-the-password")
		assert.Contains(t, denied, `"allowed":false`)
		assert.Equal(t, denied, call("nobody-here", "password"))
		assert.Equal(t, denied, call("user2", strings.Repeat("0", 40)))
		assert.NotContains(t, denied, "password")
		// A public repository stays closed to a failed login: it is not an anonymous read.
		assert.False(t, basic("user2", "not-the-password", "user2", "repo1", "read").Allowed)
	})

	run("two-factor account is refused for a password", func(t *testing.T) {
		// user24 has two-factor authentication and no one-time password can be sent.
		assert.False(t, basic("user24", "password", "user2", "repo1", "read").Allowed)
		// Its personal access token is not a password login.
		tok := ascNewToken(t, 24, auth_model.AccessTokenScopeAll)
		assert.True(t, basic("user24", tok, "user2", "repo1", "read").Allowed)
	})

	run("prohibited user", func(t *testing.T) {
		tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = ?", true, 2)
		require.NoError(t, err)
		assert.False(t, basic("user2", "password", "user2", "repo1", "read").Allowed)
		assert.False(t, basic("user2", tok, "user2", "repo1", "read").Allowed)
	})

	run("password sign-in off", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Service.EnableBasicAuth, false)()
		assert.False(t, basic("user4", "password", "user2", "repo1", "read").Allowed)
		tok := ascNewToken(t, 4, auth_model.AccessTokenScopeAll)
		assert.True(t, basic("user4", tok, "user2", "repo1", "read").Allowed)
	})

	run("actions task token as the password", func(t *testing.T) {
		resp := basic("gitea-actions", ascTaskToken, "user5", "repo4", "write")
		require.True(t, resp.Allowed)
		assert.True(t, resp.IsActionsTask)
		assert.False(t, basic("gitea-actions", ascTaskToken, "user2", "repo2", "read").Allowed)
	})

	run("user absent keeps bearer behaviour", func(t *testing.T) {
		tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
		assert.True(t, ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo2", Mode: "write"}).Allowed)
		// A password is not a bearer token, and neither is user:password.
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: "password", Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
		assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: "user2:password", Owner: "user2", Repo: "repo2", Mode: "read"}).Allowed)
	})
}

// A refusal after authentication carries the user's own identity; a credential
// that did not authenticate gives the all-zero denial. asc maps the first to 403
// and the second to 401 with a challenge.
func TestAscAuthzRefusalIdentity(t *testing.T) {
	ascAuthzSetup(t)
	t.Cleanup(test.MockVariableValue(&setting.Service.EnableBasicAuth, true))

	// user4 may not see user2/repo2.
	other := ascNewToken(t, 4, auth_model.AccessTokenScopeAll)
	noPerm := ascAuthzDo(t, private.AscAuthzRequest{Token: other, Owner: "user2", Repo: "repo2", Mode: "read"})
	assert.Equal(t, private.AscAuthzResponse{UserID: 4, Username: "user4"}, noPerm)

	t.Run("a missing repo looks like a missing permission", func(t *testing.T) {
		assert.Equal(t, noPerm, ascAuthzDo(t, private.AscAuthzRequest{Token: other, Owner: "user2", Repo: "does-not-exist", Mode: "read"}))
		assert.Equal(t, noPerm, ascAuthzDo(t, private.AscAuthzRequest{Token: other, Owner: "nobody-here", Repo: "repo2", Mode: "read"}))
	})

	t.Run("write on a public repo without permission", func(t *testing.T) {
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: other, Owner: "user2", Repo: "repo1", Mode: "write"})
		assert.Equal(t, noPerm, resp)
	})

	t.Run("a token scope refusal", func(t *testing.T) {
		readOnly := ascNewToken(t, 2, auth_model.AccessTokenScopeReadRepository)
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: readOnly, Owner: "user2", Repo: "repo2", Mode: "write"})
		assert.Equal(t, private.AscAuthzResponse{UserID: 2, Username: "user2"}, resp)
	})

	t.Run("basic login with a good password and no permission", func(t *testing.T) {
		resp := ascAuthzDo(t, private.AscAuthzRequest{User: "user4", Token: "password", Owner: "user2", Repo: "repo2", Mode: "read"})
		assert.Equal(t, private.AscAuthzResponse{UserID: 4, Username: "user4"}, resp)
	})

	t.Run("a task token for another repo", func(t *testing.T) {
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: ascTaskToken, Owner: "user2", Repo: "repo2", Mode: "read"})
		assert.False(t, resp.Allowed)
		assert.NotZero(t, resp.UserID)
	})

	t.Run("a credential that did not authenticate gives all zero", func(t *testing.T) {
		zero := private.AscAuthzResponse{}
		for name, req := range map[string]private.AscAuthzRequest{
			"wrong password":       {User: "user2", Token: "not-the-password"},
			"unknown user":         {User: "nobody-here", Token: "password"},
			"bad token as basic":   {User: "user2", Token: strings.Repeat("0", 40)},
			"bad bearer token":     {Token: strings.Repeat("0", 40)},
			"two-factor password":  {User: "user24", Token: "password"},
			"password sign-in off": {User: "user4", Token: "password"},
			"anonymous":            {},
		} {
			t.Run(name, func(t *testing.T) {
				defer test.MockVariableValue(&setting.Service.EnableBasicAuth, name != "password sign-in off")()
				for _, repo := range []string{"repo2", "does-not-exist"} {
					req.Owner, req.Repo, req.Mode = "user2", repo, "read"
					assert.Equal(t, zero, ascAuthzDo(t, req), repo)
				}
			})
		}

		t.Run("prohibited user", func(t *testing.T) {
			tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
			_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE `user` SET prohibit_login = ? WHERE id = ?", true, 2)
			require.NoError(t, err)
			assert.Equal(t, zero, ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo2", Mode: "read"}))
			assert.Equal(t, zero, ascAuthzDo(t, private.AscAuthzRequest{User: "user2", Token: "password", Owner: "user2", Repo: "repo2", Mode: "read"}))
		})
	})
}
