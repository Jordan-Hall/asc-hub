// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net"
	"net/http"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeReleaseList answers Releases.List with err.
type fakeReleaseList struct {
	ascv1.UnimplementedReleasesServer
	err error
}

func (f *fakeReleaseList) List(gocontext.Context, *ascv1.ListReleasesRequest) (*ascv1.ListReleasesResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &ascv1.ListReleasesResponse{}, nil
}

func startFakeReleaseList(t *testing.T, err error) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterReleasesServer(srv, &fakeReleaseList{err: err})
	l, lerr := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, lerr)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, cerr := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, cerr)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, testForwardedKey))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
}

func releasesPage(t *testing.T, path string, user int64) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, path)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, user)
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadGitRepo(t, ctx)
	defer ctx.Repo.GitRepo.Close()
	// asc serves the repository; its git is read from the fixture on disk.
	ctx.Repo.Repository.StorageBackend = "asc"
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	Releases(ctx)
	return resp.Code, resp.Body.String()
}

func TestReleasesStates(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	t.Run("asc failing to read its records says so and offers a retry", func(t *testing.T) {
		startFakeReleaseList(t, status.Error(codes.Unavailable, "storage down"))
		code, body := releasesPage(t, "/user2/repo1/releases?q=v1", 2)
		require.Equal(t, http.StatusOK, code, body)
		assert.Contains(t, body, `data-testid="release-storage-error"`)
		assert.Contains(t, body, `href="/user2/repo1/releases?q=v1">asc_rp.rel_retry</a>`)
	})

	t.Run("a repository asc does not know is not an error", func(t *testing.T) {
		startFakeReleaseList(t, status.Error(codes.NotFound, "no repo"))
		code, body := releasesPage(t, "user2/repo1/releases", 2)
		require.Equal(t, http.StatusOK, code, body)
		assert.NotContains(t, body, "release-storage-error")
	})

	t.Run("no releases shows the asc command to whoever may create one", func(t *testing.T) {
		startFakeReleaseList(t, nil)
		_, err := db.GetEngine(db.DefaultContext).Where("repo_id = ?", 1).Delete(&repo_model.Release{})
		require.NoError(t, err)
		code, body := releasesPage(t, "user2/repo1/releases", 2)
		require.Equal(t, http.StatusOK, code, body)
		assert.Contains(t, body, "asc_rp.no_releases_title")
		assert.Contains(t, body, `data-clipboard-text="asc release create v1.0.0"`)
		assert.Contains(t, body, `data-testid="release-new-dialog"`)
		_, body = releasesPage(t, "user2/repo1/releases", 4)
		assert.NotContains(t, body, "release-empty-command", "a reader cannot create a release")
	})
}
