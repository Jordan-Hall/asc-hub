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
	assertScope(t, row.Scope, []auth_model.AccessTokenScope{"read:repository", "write:repository", "read:user"}, []auth_model.AccessTokenScope{"write:user", "read:organization", "read:admin", "write:organization"})

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
		// Every kind has read:user: asc asks who the credential is before it decides anything else.
		CallRepoRead:  {[]S{"read:repository", "read:user"}, []S{"write:repository", "write:user", "read:organization", "read:admin"}},
		CallRepoWrite: {[]S{"read:repository", "write:repository", "read:user"}, []S{"write:user", "read:organization", "read:admin", "write:issue"}},
		// asc decides "administrator of the repository" by asking for write access, so the audit token may write.
		CallRepoAdmin: {[]S{"read:repository", "write:repository", "read:user"}, []S{"write:user", "read:organization", "read:admin", "write:issue"}},
		CallOrg:       {[]S{"write:repository", "read:user"}, []S{"read:organization", "write:organization", "write:user", "read:admin"}},
		CallAccount:   {[]S{"read:user", "write:user"}, []S{"read:repository", "read:organization", "read:admin"}},
		CallInstance:  {[]S{"read:admin", "read:user"}, []S{"write:admin", "read:repository", "write:user"}},
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

// The scope of each kind of call, exactly: nothing a call does not need. Writing is for the calls asc lets only an
// administrator make, and then only on the repository (or the repositories of one organisation) of the call.
func TestScopeOfEachKindOfCall(t *testing.T) {
	for kind, want := range map[CallKind][]string{
		CallRepoRead:  {"read:repository", "read:user"},
		CallRepoWrite: {"write:repository", "read:user"},
		CallRepoAdmin: {"write:repository", "read:user"},
		CallOrg:       {"write:repository", "read:user"},
		CallAccount:   {"write:user"},
		CallInstance:  {"read:admin", "read:user"},
	} {
		s, err := scopeOf(kind)
		require.NoError(t, err)
		got := strings.Split(string(s), ",")
		assert.ElementsMatch(t, want, got, "kind %d: %s", kind, s)
	}
}

func TestUserCallOrgIssuesATokenForTheRepositoriesOfTheOrganisationOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	ctx := t.Context()

	// No repository of the organisation is stored in asc: no token at all.
	before := countCallTokens(t)
	_, _, err := UserCallOrg(ctx, doer, 3)
	require.ErrorIs(t, err, ErrNoOrgRepos)
	assert.Equal(t, before, countCallTokens(t))

	_, err = db.GetEngine(ctx).Exec("UPDATE repository SET storage_backend = ? WHERE owner_id = ? AND id = ?", "asc", 3, 3)
	require.NoError(t, err)
	_, release, err := UserCallOrg(ctx, doer, 3)
	require.NoError(t, err)
	assert.Equal(t, before+1, countCallTokens(t), "one token while the call lives")

	var row auth_model.AccessToken
	has, err := db.GetEngine(ctx).Where("uid = ? AND name LIKE ?", doer.ID, tokenPrefix+"%").Get(&row)
	require.NoError(t, err)
	require.True(t, has)
	assert.False(t, row.ResourceAllRepos, "never all repositories of the person")
	resources, err := auth_model.GetRepositoriesAccessibleWithToken(ctx, row.ID)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.EqualValues(t, 3, resources[0].RepoID, "the repositories of the organisation, one row each")
	assertScope(t, row.Scope, []auth_model.AccessTokenScope{"write:repository", "read:user"}, []auth_model.AccessTokenScope{"write:user", "read:admin", "write:organization", "read:organization"})

	release()
	assert.Equal(t, before, countCallTokens(t), "the token goes with the request")

	_, _, err = UserCallOrg(ctx, nil, 3)
	assert.ErrorIs(t, err, ErrNoDoer)
}

func TestUserCallOrgRefusesAnOrganisationAscWouldRefuse(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	// Over the limit of asc: no token with so many rows.
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET storage_backend = ? WHERE owner_id = ?", "asc", 2)
	require.NoError(t, err)
	var n int64
	n, err = db.GetEngine(t.Context()).Table("repository").Where("owner_id = ?", 2).Count()
	require.NoError(t, err)
	if n <= MaxOrgRepos {
		t.Skip("the fixtures have too few repositories to pass the limit")
	}
	_, _, err = UserCallOrg(t.Context(), doer, 2)
	assert.ErrorIs(t, err, ErrNoOrgRepos)
}
