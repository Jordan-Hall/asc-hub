// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"context"
	"errors"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Access to an asc repository is the hub's access, not another rule. The matrix below takes a repository, grants a
// person something, and asks every way asc is reached what it says: the endpoint asc serve asks about a command line
// credential (ascAuthorize: read, write, and administrator as asc asks it, write access with the permission read from
// the answer) and the decisions the hub makes for its own pages (asctrust.UserCall: list and search read the code,
// settings and the audit log need an administrator of the repository, an agent run needs write).
//
// Each outcome is compared with two things: a table written down by hand (what the hub's model gives that grant), and
// the decision of the model on the same repository while it is stored in git, so a change of the model moves the table
// and the git outcome together, and a divergence of asc from the model shows as a failure.

type parityOutcome struct{ read, write, admin bool }

var (
	denied  = parityOutcome{}
	reader  = parityOutcome{read: true}
	writer  = parityOutcome{read: true, write: true}
	manager = parityOutcome{read: true, write: true, admin: true}
)

type parityPerson struct {
	name string
	uid  int64 // 0 is anonymous
	// grant changes the repository or the person's relation to it before the outcome is computed.
	grant func(t *testing.T)
	want  parityOutcome
}

func sql(t *testing.T, q string, args ...any) {
	t.Helper()
	_, err := db.GetEngine(db.DefaultContext).Exec(append([]any{q}, args...)...)
	require.NoError(t, err)
}

func collaborate(t *testing.T, repoID, uid int64, mode perm.AccessMode) {
	t.Helper()
	require.NoError(t, db.Insert(db.DefaultContext, &repo_model.Collaboration{RepoID: repoID, UserID: uid, Mode: mode}))
	require.NoError(t, db.Insert(db.DefaultContext, &access_model.Access{RepoID: repoID, UserID: uid, Mode: mode}))
}

func teamMode(t *testing.T, teamID int64, mode perm.AccessMode) {
	t.Helper()
	sql(t, "UPDATE team SET authorize = ? WHERE id = ?", int(mode), teamID)
	sql(t, "UPDATE team_unit SET access_mode = ? WHERE team_id = ?", int(mode), teamID)
}

// baseline is the hub's model for a person on a repository stored in git.
func baseline(t *testing.T, repoID, uid int64) parityOutcome {
	t.Helper()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repoID})
	var u *user_model.User
	if uid != 0 {
		u = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: uid})
	}
	p, err := access_model.GetUserRepoPermission(db.DefaultContext, repo, u)
	require.NoError(t, err)
	usable := u == nil || (u.IsActive && !u.ProhibitLogin)
	if u == nil && setting.Service.RequireSignInView {
		usable = false
	}
	return parityOutcome{
		read:  usable && p.CanAccess(perm.AccessModeRead, unit.TypeCode),
		write: usable && p.CanAccess(perm.AccessModeWrite, unit.TypeCode) && !repo.IsArchived && !repo.IsMirror,
		// asc asks "administrator" as a write and reads the permission of the answer, so an administrator of a
		// repository that cannot be changed is not asked as one.
		admin: usable && u != nil && p.IsAdmin() && p.CanAccess(perm.AccessModeWrite, unit.TypeCode) && !repo.IsArchived && !repo.IsMirror,
	}
}

// isAdmin is the model's own answer, for the pages of the hub: the settings of an archived repository are open to
// its administrator.
func isAdmin(t *testing.T, repoID, uid int64) bool {
	t.Helper()
	if uid == 0 {
		return false
	}
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repoID})
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: uid})
	if !u.IsActive || u.ProhibitLogin {
		return false
	}
	p, err := access_model.GetUserRepoPermission(db.DefaultContext, repo, u)
	require.NoError(t, err)
	return p.IsAdmin()
}

// viaEndpoint is what asc serve is told for a command line credential.
func viaEndpoint(t *testing.T, owner, name string, uid int64) parityOutcome {
	t.Helper()
	req := func(mode string) private.AscAuthzResponse {
		r := private.AscAuthzRequest{Owner: owner, Repo: name, Mode: mode}
		if uid != 0 {
			r.Token = ascNewToken(t, uid, auth_model.AccessTokenScopeAll)
		}
		return ascAuthzDo(t, r)
	}
	r, w := req("read"), req("write")
	return parityOutcome{
		read:  r.Allowed,
		write: w.Allowed,
		// asc decides "administrator" by asking for write and reading the permission of the answer.
		admin: w.Allowed && (w.Perm == "admin" || w.Perm == "owner"),
	}
}

// viaPages is what the hub decides for its own pages: list and search read, an agent run writes, settings and the
// audit log administer.
func viaPages(t *testing.T, repoID, uid int64) (list, run, settings bool) {
	t.Helper()
	if uid == 0 {
		return false, false, false // a page call is made for a signed-in person only
	}
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repoID})
	u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: uid})
	ok := func(kind asctrust.CallKind) bool {
		_, release, err := asctrust.UserCall(context.Background(), u, kind, repo)
		release()
		if err != nil {
			assert.True(t, errors.Is(err, asctrust.ErrForbidden) || errors.Is(err, asctrust.ErrNoDoer), "a refusal is the hub's, never another failure: %v", err)
		}
		return err == nil
	}
	return ok(asctrust.CallRepoRead), ok(asctrust.CallRepoWrite), ok(asctrust.CallRepoAdmin)
}

func TestAscPermissionParityMatrix(t *testing.T) {
	type repoCase struct {
		label       string
		repoID      int64
		owner, name string
		prepare     func(t *testing.T)
		people      []parityPerson
	}
	cases := []repoCase{
		{
			label: "private repository of an organisation", repoID: 3, owner: "org3", name: "repo3",
			people: []parityPerson{
				{name: "organisation owner", uid: 2, want: manager},
				{name: "team member, write", uid: 4, want: writer},
				{name: "team member, read", uid: 4, grant: func(t *testing.T) { teamMode(t, 2, perm.AccessModeRead) }, want: reader},
				{name: "team member, admin", uid: 4, grant: func(t *testing.T) { teamMode(t, 2, perm.AccessModeAdmin) }, want: manager},
				{name: "outsider", uid: 5, want: denied},
				{name: "restricted outsider", uid: 29, want: denied},
				{name: "site administrator", uid: 1, want: manager},
				{name: "anonymous", uid: 0, want: denied},
			},
		},
		{
			label: "private repository of a user", repoID: 2, owner: "user2", name: "repo2",
			people: []parityPerson{
				{name: "owner", uid: 2, want: manager},
				{name: "collaborator, read", uid: 5, grant: func(t *testing.T) { collaborate(t, 2, 5, perm.AccessModeRead) }, want: reader},
				{name: "collaborator, write", uid: 5, grant: func(t *testing.T) { collaborate(t, 2, 5, perm.AccessModeWrite) }, want: writer},
				{name: "collaborator, admin", uid: 5, grant: func(t *testing.T) { collaborate(t, 2, 5, perm.AccessModeAdmin) }, want: manager},
				{name: "outsider", uid: 5, want: denied},
				{name: "restricted outsider", uid: 29, want: denied},
				{name: "site administrator", uid: 1, want: manager},
				{name: "anonymous", uid: 0, want: denied},
				{name: "inactive owner account", uid: 2, grant: func(t *testing.T) { sql(t, "UPDATE `user` SET prohibit_login = ? WHERE id = 2", true) }, want: denied},
			},
		},
		{
			label: "public repository", repoID: 1, owner: "user2", name: "repo1",
			people: []parityPerson{
				{name: "owner", uid: 2, want: manager},
				{name: "outsider", uid: 5, want: reader},
				{name: "restricted outsider", uid: 29, want: denied},
				{name: "collaborator, write", uid: 5, grant: func(t *testing.T) { collaborate(t, 1, 5, perm.AccessModeWrite) }, want: writer},
				{name: "site administrator", uid: 1, want: manager},
				{name: "anonymous", uid: 0, want: reader},
			},
		},
		{
			label: "archived repository", repoID: 2, owner: "user2", name: "repo2",
			prepare: func(t *testing.T) { sql(t, "UPDATE repository SET is_archived = ? WHERE id = 2", true) },
			people: []parityPerson{
				{name: "owner reads, no change", uid: 2, want: reader},
				{name: "outsider", uid: 5, want: denied},
			},
		},
	}

	for _, c := range cases {
		for _, p := range c.people {
			t.Run(c.label+"/"+p.name, func(t *testing.T) {
				require.NoError(t, unittest.PrepareTestDatabase())
				t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
				t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
				if c.prepare != nil {
					c.prepare(t)
				}
				if p.grant != nil {
					p.grant(t)
				}

				// Stored in git: the model alone.
				git := baseline(t, c.repoID, p.uid)
				// The same repository, stored in asc.
				sql(t, "UPDATE repository SET storage_backend = ? WHERE id = ?", string(gitrepo.StorageBackendAsc), c.repoID)
				endpoint := viaEndpoint(t, c.owner, c.name, p.uid)

				assert.Equal(t, git, endpoint, "asc serve is told what the hub's model gives a git repository")
				assert.Equal(t, p.want, endpoint, "the table written down by hand")

				list, run, settings := viaPages(t, c.repoID, p.uid)
				if p.uid != 0 {
					assert.Equal(t, git.read, list, "list and search read what the model lets the person read")
					assert.Equal(t, git.write, run, "an agent run needs what a push needs")
					assert.Equal(t, isAdmin(t, c.repoID, p.uid), settings, "settings and the audit log need an administrator of the repository")
				}
			})
		}
	}
}

// A listing is filtered by the same visibility rules: a person never learns of a private repository of another by a
// difference of answer. The endpoint answers a repository that does not exist, one that is private to the asker, and
// one stored in git with the same empty refusal.
func TestAscRefusalsDoNotTellWhatExists(t *testing.T) {
	ascAuthzSetup(t)
	outsider := ascNewToken(t, 5, auth_model.AccessTokenScopeAll)
	missing := ascAuthzDo(t, private.AscAuthzRequest{Token: outsider, Owner: "user2", Repo: "no-such-repo", Mode: "read"})
	hidden := ascAuthzDo(t, private.AscAuthzRequest{Token: outsider, Owner: "user2", Repo: "repo2", Mode: "read"})
	assert.Equal(t, missing, hidden, "a private repository of another looks like a missing one")
	assert.False(t, hidden.Allowed)
	anon := ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo2", Mode: "read"})
	assert.Equal(t, private.AscAuthzResponse{}, anon)
}
