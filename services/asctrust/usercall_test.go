// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

var testForwardedKey = []byte("0123456789abcdef0123456789abcdef")

// useForwardedKey gives the hub the key its claims are signed with.
func useForwardedKey(t *testing.T) {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, testForwardedKey))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
}

func ascRepo(t *testing.T) *repo_model.Repository {
	t.Helper()
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)
	return unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
}

// claimIn verifies the claim in the outgoing metadata of ctx for subject, as the server would.
func claimIn(t *testing.T, ctx context.Context, subject string) (user, role string, err error) {
	t.Helper()
	md, _ := metadata.FromOutgoingContext(ctx)
	return ascidentity.Verify(testForwardedKey, func(k string) string { return strings.Join(md.Get(k), ",") }, subject, time.Now())
}

// What the hub decides, for a person and a repository, is what the claim says. The repository 1 (user2/repo1) is
// public and owned by user2.
func TestUserCallSignsWhatTheHubDecided(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	reader := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	call := func(u *user_model.User, kind CallKind) (string, error) {
		ctx, release, err := UserCall(t.Context(), u, kind, repo)
		defer release()
		if err != nil {
			return "", err
		}
		user, role, verr := claimIn(t, ctx, "user2/repo1")
		require.NoError(t, verr)
		assert.Equal(t, strconv.FormatInt(u.ID, 10)+":"+u.Name, user, "the claim names the person")
		return role, nil
	}

	// The owner is an administrator, whatever the call asked for.
	for _, kind := range []CallKind{CallRepoRead, CallRepoWrite, CallRepoAdmin} {
		role, err := call(owner, kind)
		require.NoError(t, err)
		assert.Equal(t, ascidentity.RoleAdmin, role, "kind %d", kind)
	}
	// A person who can only read a public repository: read, and not write or administration.
	role, err := call(reader, CallRepoRead)
	require.NoError(t, err)
	assert.Equal(t, ascidentity.RoleRead, role)
	_, err = call(reader, CallRepoWrite)
	assert.ErrorIs(t, err, ErrForbidden)
	_, err = call(reader, CallRepoAdmin)
	assert.ErrorIs(t, err, ErrForbidden)

	// A write collaborator: write, and still not administration.
	require.NoError(t, db.Insert(t.Context(), &repo_model.Collaboration{RepoID: repo.ID, UserID: reader.ID, Mode: perm.AccessModeWrite}))
	require.NoError(t, db.Insert(t.Context(), &access_model.Access{RepoID: repo.ID, UserID: reader.ID, Mode: perm.AccessModeWrite}))
	role, err = call(reader, CallRepoRead)
	require.NoError(t, err)
	assert.Equal(t, ascidentity.RoleWrite, role, "the claim says the highest the hub gives")
	role, err = call(reader, CallRepoWrite)
	require.NoError(t, err)
	assert.Equal(t, ascidentity.RoleWrite, role)
	_, err = call(reader, CallRepoAdmin)
	assert.ErrorIs(t, err, ErrForbidden)
}

func TestUserCallFollowsVisibilityAndAccountState(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	_, release, err := UserCall(t.Context(), stranger, CallRepoRead, repo)
	release()
	require.NoError(t, err, "a public repository can be read by a signed-in person")

	// Private: the hub's model hides it, so no claim is signed.
	_, err = db.GetEngine(t.Context()).Exec("UPDATE repository SET is_private = ? WHERE id = 1", true)
	require.NoError(t, err)
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	_, _, err = UserCall(t.Context(), stranger, CallRepoRead, repo)
	assert.ErrorIs(t, err, ErrForbidden)

	// A restricted user sees only what they were given.
	_, err = db.GetEngine(t.Context()).Exec("UPDATE repository SET is_private = ? WHERE id = 1", false)
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `user` SET is_restricted = ? WHERE id = 4", true)
	require.NoError(t, err)
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	stranger = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	_, _, err = UserCall(t.Context(), stranger, CallRepoRead, repo)
	assert.ErrorIs(t, err, ErrForbidden, "a restricted user")

	// An account that is not active, or may not sign in, is nobody.
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	for name, mutate := range map[string]func(*user_model.User){
		"inactive":       func(u *user_model.User) { u.IsActive = false },
		"cannot sign in": func(u *user_model.User) { u.ProhibitLogin = true },
		"no id":          func(u *user_model.User) { u.ID = 0 },
	} {
		u := *owner
		mutate(&u)
		_, _, err := UserCall(t.Context(), &u, CallRepoRead, repo)
		assert.ErrorIs(t, err, ErrNoDoer, name)
	}
	_, _, err = UserCall(t.Context(), nil, CallRepoRead, repo)
	assert.ErrorIs(t, err, ErrNoDoer)
}

func TestUserCallOrganisationInstanceAndAccount(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	org := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})      // org3
	member := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})   // an owner of org3
	outsider := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5}) // not in org3
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	verify := func(ctx context.Context, subject, role string) {
		t.Helper()
		_, got, err := claimIn(t, ctx, subject)
		require.NoError(t, err, subject)
		assert.Equal(t, role, got, subject)
	}

	ctx, _, err := UserCallOrg(t.Context(), member, org)
	require.NoError(t, err)
	verify(ctx, "org:org3", ascidentity.RoleAdmin)

	_, _, err = UserCallOrg(t.Context(), outsider, org)
	assert.ErrorIs(t, err, ErrForbidden, "an outsider")
	_, _, err = UserCallOrg(t.Context(), admin, org)
	assert.NoError(t, err, "a site administrator")
	_, _, err = UserCallOrg(t.Context(), member, member)
	assert.Error(t, err, "a user is not an organisation")

	// The server: site administrators only.
	ctx, _, err = UserCall(t.Context(), admin, CallInstance, nil)
	require.NoError(t, err)
	verify(ctx, "instance", ascidentity.RoleAdmin)
	_, _, err = UserCall(t.Context(), member, CallInstance, nil)
	assert.ErrorIs(t, err, ErrForbidden)

	// The account of the person.
	ctx, _, err = UserCall(t.Context(), outsider, CallAccount, nil)
	require.NoError(t, err)
	verify(ctx, "user:"+strings.ToLower(outsider.Name), ascidentity.RoleWrite)

	// The kinds take a repository or not, as they say.
	_, _, err = UserCall(t.Context(), member, CallRepoRead, nil)
	assert.Error(t, err)
	_, _, err = UserCall(t.Context(), member, CallAccount, &repo_model.Repository{ID: 1})
	assert.Error(t, err)
	_, _, err = UserCall(t.Context(), member, CallKind(0), nil)
	assert.Error(t, err)
}

// The claim is the only thing that says who the person is: without the key of the hub no call is made.
func TestUserCallWithoutAKeyIsNotMade(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte(nil)))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	repo := ascRepo(t)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	_, _, err := UserCall(t.Context(), owner, CallRepoRead, repo)
	assert.ErrorIs(t, err, ascidentity.ErrNoKey)
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("short")))
	_, _, err = UserCall(t.Context(), owner, CallRepoRead, repo)
	assert.ErrorIs(t, err, ascidentity.ErrNoKey)
}

// On the wire: the hub's service credential, the claim, and no credential of the person.
func TestTheCallReachesTheServerWithTheServiceCredentialAndTheClaim(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	f, c := startFake(t)
	f.listKeys = func(*ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error) {
		return &ascv1.ListSigningKeysResponse{}, nil
	}
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	ctx, release, err := UserCall(t.Context(), user, CallAccount, nil)
	require.NoError(t, err)
	defer release()
	_, err = ascv1.NewSigningClient(c).ListKeys(ctx, &ascv1.ListSigningKeysRequest{})
	require.NoError(t, err)

	call := f.last(t, "ListKeys")
	assert.Equal(t, "Bearer "+testInternalToken, call.Auth, "the hub's own credential")
	got, role, err := ascidentity.Verify(testForwardedKey, func(k string) string { return strings.Join(call.MD.Get(k), ",") }, "user:user2", time.Now())
	require.NoError(t, err, "the server can verify what the hub signed")
	assert.Equal(t, "2:user2", got)
	assert.Equal(t, ascidentity.RoleWrite, role)
}
