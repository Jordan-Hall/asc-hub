// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countCallTokens(t *testing.T) int {
	t.Helper()
	n, err := db.GetEngine(t.Context()).Table("access_token").Where("name LIKE ?", tokenPrefix+"%").Count()
	require.NoError(t, err)
	return int(n)
}

func TestUserCallIssuesAShortLivedTokenAndDeletesIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	before := countCallTokens(t)

	ctx, release, err := UserCall(t.Context(), doer, CallRepoWrite, repo)
	require.NoError(t, err)
	assert.Equal(t, before+1, countCallTokens(t), "one token while the call lives")

	var row auth_model.AccessToken
	has, err := db.GetEngine(t.Context()).Where("uid = ? AND name LIKE ?", doer.ID, tokenPrefix+"%").Get(&row)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, doer.ID, row.UID)
	assert.False(t, row.ResourceAllRepos, "a repository call is limited to that repository")
	resources, err := auth_model.GetRepositoriesAccessibleWithToken(t.Context(), row.ID)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, repo.ID, resources[0].RepoID)
	assertScope(t, row.Scope, []auth_model.AccessTokenScope{"read:repository", "write:repository"}, []auth_model.AccessTokenScope{"read:user", "write:user", "read:organization", "read:admin", "write:organization"})

	// The credential is in the context and is not the internal token.
	require.NotNil(t, ctx)

	release()
	assert.Equal(t, before, countCallTokens(t), "the token is deleted when the call is done")
	release() // a second release is harmless
	assert.Equal(t, before, countCallTokens(t))
}

func TestUserCallDeletesTheTokenEvenWhenTheRequestWasCancelled(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	before := countCallTokens(t)

	reqCtx, cancel := context.WithCancel(t.Context())
	_, release, err := UserCall(reqCtx, doer, CallAccount, nil)
	require.NoError(t, err)
	assert.Equal(t, before+1, countCallTokens(t))
	cancel() // the browser went away
	release()
	assert.Equal(t, before, countCallTokens(t), "a cancelled request does not leave a token behind")
}

// assertScope checks that scope has every scope of has and none of hasNot. A write scope includes its read scope, so
// the normal form of "read:x,write:x" is "write:x"; the test asks what the token can do, not how it is spelled.
func assertScope(t *testing.T, scope auth_model.AccessTokenScope, has, hasNot []auth_model.AccessTokenScope) {
	t.Helper()
	for _, s := range has {
		ok, err := scope.HasScope(s)
		require.NoError(t, err)
		assert.True(t, ok, "%s should include %s", scope, s)
	}
	for _, s := range hasNot {
		ok, err := scope.HasScope(s)
		require.NoError(t, err)
		assert.False(t, ok, "%s should not include %s", scope, s)
	}
}

func TestUserCallScopesPerKind(t *testing.T) {
	type S = auth_model.AccessTokenScope
	cases := map[CallKind]struct{ has, hasNot []S }{
		CallRepoRead:  {[]S{"read:repository"}, []S{"write:repository", "read:user", "read:organization", "read:admin"}},
		CallRepoWrite: {[]S{"read:repository", "write:repository"}, []S{"write:user", "read:organization", "read:admin", "write:issue"}},
		CallOrg:       {[]S{"read:organization"}, []S{"write:organization", "read:repository", "read:user"}},
		CallAccount:   {[]S{"read:user", "write:user"}, []S{"read:repository", "read:organization", "read:admin"}},
		CallInstance:  {[]S{"read:admin"}, []S{"write:admin", "read:repository"}},
	}
	for kind, want := range cases {
		s, err := scopeOf(kind)
		require.NoError(t, err)
		assertScope(t, s, want.has, want.hasNot)
	}
	_, err := scopeOf(CallKind(99))
	assert.Error(t, err)
}

func TestUserCallRefusesWhatItCannotDoSafely(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	before := countCallTokens(t)

	_, _, err := UserCall(t.Context(), nil, CallRepoRead, repo)
	assert.ErrorIs(t, err, ErrNoDoer)
	_, _, err = UserCall(t.Context(), &user_model.User{ID: 0}, CallRepoRead, repo)
	assert.ErrorIs(t, err, ErrNoDoer)
	_, _, err = UserCall(t.Context(), doer, CallRepoRead, nil)
	assert.Error(t, err, "a repository call without a repository")
	_, _, err = UserCall(t.Context(), doer, CallOrg, repo)
	assert.Error(t, err, "an organisation call does not take a repository")
	_, _, err = UserCall(t.Context(), doer, CallKind(0), nil)
	assert.Error(t, err)
	assert.Equal(t, before, countCallTokens(t), "nothing was issued")
}

func TestUserCallRevokesAnIssuedTokenThatTheClientRefuses(t *testing.T) {
	// A token that is not visible ASCII cannot be sent; the issued credential must not be left behind.
	revoked := 0
	restore := UseTokenIssuer(func(context.Context, *user_model.User, auth_model.AccessTokenScope, int64) (string, func(), error) {
		return "has a space", func() { revoked++ }, nil
	})
	defer restore()
	_, _, err := UserCall(t.Context(), &user_model.User{ID: 5}, CallAccount, nil)
	require.Error(t, err)
	assert.Equal(t, 1, revoked)

	restore2 := UseTokenIssuer(func(context.Context, *user_model.User, auth_model.AccessTokenScope, int64) (string, func(), error) {
		return "", nil, errors.New("database is down")
	})
	defer restore2()
	_, _, err = UserCall(t.Context(), &user_model.User{ID: 5}, CallAccount, nil)
	require.Error(t, err)
}

func TestTheCredentialReachesTheServerAsThePerson(t *testing.T) {
	f, c := startFake(t)
	f.listKeys = func(*ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error) {
		return &ascv1.ListSigningKeysResponse{}, nil
	}
	restore := UseTokenIssuer(func(context.Context, *user_model.User, auth_model.AccessTokenScope, int64) (string, func(), error) {
		return "per-request-token", func() {}, nil
	})
	defer restore()
	ctx, release, err := UserCall(t.Context(), &user_model.User{ID: 5, Name: "alice"}, CallAccount, nil)
	require.NoError(t, err)
	defer release()
	_, err = LoadKeys(ctx, c, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "Bearer per-request-token", f.last(t, "ListKeys").Auth)
	assert.NotContains(t, f.last(t, "ListKeys").Auth, testInternalToken, "a person's call never carries the internal token")
}

func TestTheSignedIdentityReachesTheServer(t *testing.T) {
	f, c := startFake(t)
	f.listKeys = func(*ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error) {
		return &ascv1.ListSigningKeysResponse{}, nil
	}
	old := setting.InternalToken
	setting.InternalToken = testInternalToken
	defer func() { setting.InternalToken = old }()
	restore := UseTokenIssuer(func(context.Context, *user_model.User, auth_model.AccessTokenScope, int64) (string, func(), error) {
		return "per-request-token", func() {}, nil
	})
	defer restore()
	ctx, release, err := UserCall(t.Context(), &user_model.User{ID: 5, Name: "alice", IsAdmin: true}, CallAccount, nil)
	require.NoError(t, err)
	defer release()
	_, err = LoadKeys(ctx, c, time.Now())
	require.NoError(t, err)

	md := f.last(t, "ListKeys").MD
	id, err := ascidentity.Verify(testInternalToken, func(k string) string { return strings.Join(md.Get(k), ",") }, time.Now())
	require.NoError(t, err, "the server can verify what the hub signed")
	assert.Equal(t, ascidentity.Identity{ID: 5, Login: "alice", Admin: true}, id)
	assert.Equal(t, "Bearer per-request-token", f.last(t, "ListKeys").Auth)

	// A secret that is set but too short is an error: never a weak key.
	setting.InternalToken = "short"
	_, _, err = UserCall(t.Context(), &user_model.User{ID: 5, Name: "alice"}, CallAccount, nil)
	require.ErrorIs(t, err, ascidentity.ErrNoSecret)
}

func TestStaleCallTokensAreSweptAndOnlyThose(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	old := &auth_model.AccessToken{UID: doer.ID, Name: tokenPrefix + "stale", Scope: "read:user", ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), old))
	fresh := &auth_model.AccessToken{UID: doer.ID, Name: tokenPrefix + "fresh", Scope: "read:user", ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), fresh))
	mine := &auth_model.AccessToken{UID: doer.ID, Name: "my own token", Scope: "read:user", ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), mine))
	longAgo := timeutil.TimeStamp(time.Now().Add(-2 * staleAfter).Unix())
	_, err := db.GetEngine(t.Context()).Exec("UPDATE access_token SET created_unix = ? WHERE id IN (?, ?)", int64(longAgo), old.ID, mine.ID)
	require.NoError(t, err)

	lastSweep.Store(0)
	sweepStale(t.Context())

	exists := func(id int64) bool {
		ok, err := db.GetEngine(t.Context()).Table("access_token").Where("id = ?", id).Exist()
		require.NoError(t, err)
		return ok
	}
	assert.False(t, exists(old.ID), "an old call token is deleted")
	assert.True(t, exists(fresh.ID), "a young one may still be in use")
	assert.True(t, exists(mine.ID), "a token the person made is never touched, however old")

	// A second sweep within the minute does nothing.
	again := &auth_model.AccessToken{UID: doer.ID, Name: tokenPrefix + "again", Scope: "read:user", ResourceAllRepos: true}
	require.NoError(t, auth_model.NewAccessToken(t.Context(), again))
	_, err = db.GetEngine(t.Context()).Exec("UPDATE access_token SET created_unix = ? WHERE id = ?", int64(longAgo), again.ID)
	require.NoError(t, err)
	sweepStale(t.Context())
	assert.True(t, exists(again.ID))
}
