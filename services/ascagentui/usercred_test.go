// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"

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

func TestUserCall(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	key := []byte("0123456789abcdef0123456789abcdef")
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, key))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)
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
	})

	t.Run("a person who may not write the repository makes no call", func(t *testing.T) {
		_, done, err := UserCall(db.DefaultContext, other, repo)
		done()
		require.ErrorIs(t, err, asctrust.ErrForbidden)
	})

	t.Run("the call carries the hub credential and the signed claim, and no token of the person", func(t *testing.T) {
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

		callCtx, done, err := UserCall(db.DefaultContext, doer, repo)
		require.NoError(t, err)
		defer done()

		_, err = ascv1.NewAgentsClient(client).GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: &ascv1.RepoRef{Name: "user2/repo1"}, Id: 1})
		require.Error(t, err)
		require.Len(t, spy.auth, 1)
		assert.Equal(t, "Bearer internal-token-for-the-test", spy.auth[0], "the hub's own credential, no access token of the person")
		user, role, err := ascidentity.Verify(key, func(k string) string { return strings.Join(spy.md[0].Get(k), ",") }, "user2/repo1", time.Now())
		require.NoError(t, err, "the claim is signed for the repository")
		assert.Equal(t, "2:user2", user)
		assert.Equal(t, ascidentity.RoleAdmin, role, "the owner, as the hub decides")
	})

	t.Run("no access token is written to the database", func(t *testing.T) {
		before, err := db.GetEngine(db.DefaultContext).Table("access_token").Count()
		require.NoError(t, err)
		_, done, err := UserCall(db.DefaultContext, doer, repo)
		require.NoError(t, err)
		done()
		after, err := db.GetEngine(db.DefaultContext).Table("access_token").Count()
		require.NoError(t, err)
		assert.Equal(t, before, after)
	})
}
