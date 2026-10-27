// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type authSpy struct {
	ascv1.UnimplementedAgentsServer
	auth []string
	md   []metadata.MD
}

func (s *authSpy) GetRun(ctx context.Context, _ *ascv1.GetAgentRunRequest) (*ascv1.GetAgentRunResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.auth = append(s.auth, md.Get("authorization")...)
	s.md = append(s.md, md)
	return nil, status.Error(codes.NotFound, "none")
}

func callTokens(t *testing.T, uid int64) []*auth_model.AccessToken {
	t.Helper()
	var out []*auth_model.AccessToken
	require.NoError(t, db.GetEngine(db.DefaultContext).Where("uid = ? AND name LIKE ?", uid, callTokenPrefix+"%").Find(&out))
	return out
}

func TestUserCall(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	other := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	t.Run("needs a signed-in user and a repository", func(t *testing.T) {
		_, done, err := UserCall(db.DefaultContext, nil, repo)
		require.ErrorIs(t, err, ErrNoDoer)
		done()
		_, done, err = UserCall(db.DefaultContext, doer, nil)
		require.ErrorIs(t, err, ErrNoDoer)
		done()
		_, done, err = UserCall(db.DefaultContext, &user_model.User{}, repo)
		require.ErrorIs(t, err, ErrNoDoer)
		done()
		assert.Empty(t, callTokens(t, doer.ID))
	})

	t.Run("the call carries a one-use token of the person, limited to one repository, deleted afterwards", func(t *testing.T) {
		spy := &authSpy{}
		srv := grpc.NewServer()
		ascv1.RegisterAgentsServer(srv, spy)
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		go func() { _ = srv.Serve(l) }()
		t.Cleanup(srv.Stop)
		client, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-the-test", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		oldToken := setting.InternalToken
		setting.InternalToken = "internal-token-for-the-test"
		t.Cleanup(func() { setting.InternalToken = oldToken })
		callCtx, done, err := UserCall(db.DefaultContext, doer, repo)
		require.NoError(t, err)

		during := callTokens(t, doer.ID)
		require.Len(t, during, 1)
		tok := during[0]
		assert.Equal(t, auth_model.AccessTokenScopeWriteRepository, tok.Scope)
		assert.False(t, tok.ResourceAllRepos, "limited to the repositories listed for it")
		res, err := auth_model.GetRepositoriesAccessibleWithToken(db.DefaultContext, tok.ID)
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, repo.ID, res[0].RepoID)

		_, err = ascv1.NewAgentsClient(client).GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}, Id: 1})
		require.Error(t, err)
		require.Len(t, spy.auth, 1)
		bearer, ok := strings.CutPrefix(spy.auth[0], "Bearer ")
		require.True(t, ok)
		assert.NotEqual(t, "internal-token-for-the-test", bearer, "a person's call never carries the internal token")
		signed, err := ascidentity.Verify("internal-token-for-the-test", func(k string) string { return strings.Join(spy.md[0].Get(k), ",") }, time.Now())
		require.NoError(t, err, "the identity the hub signed in is signed for the server")
		assert.Equal(t, ascidentity.Identity{ID: doer.ID, Login: doer.Name, Admin: doer.IsAdmin}, signed)
		found, err := auth_model.GetAccessTokenBySHA(db.DefaultContext, bearer)
		require.NoError(t, err)
		assert.Equal(t, tok.ID, found.ID)
		assert.Equal(t, doer.ID, found.UID)

		done()
		assert.Empty(t, callTokens(t, doer.ID), "deleted when the call is over")
		_, err = auth_model.GetAccessTokenBySHA(db.DefaultContext, bearer)
		require.Error(t, err)
	})

	t.Run("a cancelled request still deletes its token", func(t *testing.T) {
		ctx, cancel := context.WithCancel(db.DefaultContext)
		_, done, err := UserCall(ctx, doer, repo)
		require.NoError(t, err)
		require.Len(t, callTokens(t, doer.ID), 1)
		cancel()
		done()
		assert.Empty(t, callTokens(t, doer.ID))
	})

	t.Run("two calls get two tokens", func(t *testing.T) {
		_, done1, err := UserCall(db.DefaultContext, doer, repo)
		require.NoError(t, err)
		_, done2, err := UserCall(db.DefaultContext, doer, repo)
		require.NoError(t, err)
		assert.Len(t, callTokens(t, doer.ID), 2)
		done1()
		assert.Len(t, callTokens(t, doer.ID), 1)
		done2()
		assert.Empty(t, callTokens(t, doer.ID))
	})

	t.Run("a crash leftover of this person is swept, nobody else's token is", func(t *testing.T) {
		old := &auth_model.AccessToken{UID: doer.ID, Name: callTokenPrefix + "leftover", Scope: auth_model.AccessTokenScopeWriteRepository}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, old))
		_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE access_token SET created_unix = ? WHERE id = ?", time.Now().Add(-time.Hour).Unix(), old.ID)
		require.NoError(t, err)
		fresh := &auth_model.AccessToken{UID: doer.ID, Name: callTokenPrefix + "in-flight", Scope: auth_model.AccessTokenScopeWriteRepository}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, fresh))
		mine := &auth_model.AccessToken{UID: doer.ID, Name: "my own token", Scope: auth_model.AccessTokenScopeWriteRepository}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, mine))
		theirs := &auth_model.AccessToken{UID: other.ID, Name: callTokenPrefix + "leftover", Scope: auth_model.AccessTokenScopeWriteRepository}
		require.NoError(t, auth_model.NewAccessToken(db.DefaultContext, theirs))
		_, err = db.GetEngine(db.DefaultContext).Exec("UPDATE access_token SET created_unix = ? WHERE id = ?", time.Now().Add(-time.Hour).Unix(), theirs.ID)
		require.NoError(t, err)

		_, done, err := UserCall(db.DefaultContext, doer, repo)
		require.NoError(t, err)
		done()

		unittest.AssertNotExistsBean(t, &auth_model.AccessToken{ID: old.ID})
		unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: fresh.ID})
		unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: mine.ID})
		unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: theirs.ID})
	})
}
