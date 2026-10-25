// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type fakeCloud struct {
	ascv1.UnimplementedCloudServer
	mu    sync.Mutex
	reqs  []*ascv1.GetWorkspaceRequest
	heads []*ascv1.CloudHead
}

func (f *fakeCloud) GetWorkspace(_ context.Context, req *ascv1.GetWorkspaceRequest) (*ascv1.GetWorkspaceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return &ascv1.GetWorkspaceResponse{Version: 1, Heads: f.heads}, nil
}

func useFakeCloud(t *testing.T, f *fakeCloud) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterCloudServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ascDraftsConnMu.Lock()
	oldNew, oldConn := ascDraftsNewConn, ascDraftsConnVal
	ascDraftsConnVal = nil
	ascDraftsNewConn = func() (grpc.ClientConnInterface, error) {
		return ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "test-internal-token", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	}
	ascDraftsConnMu.Unlock()
	t.Cleanup(func() {
		ascDraftsConnMu.Lock()
		if c, ok := ascDraftsConnVal.(*ascclient.Client); ok {
			_ = c.Close()
		}
		ascDraftsNewConn, ascDraftsConnVal = oldNew, oldConn
		ascDraftsConnMu.Unlock()
	})
}

func setAscBackend(t *testing.T, repoID int64, backend string) {
	t.Helper()
	_, err := db.GetEngine(db.DefaultContext).ID(repoID).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: backend})
	require.NoError(t, err)
}

func getAscDrafts(t *testing.T, doer *user_model.User, query string) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/-/asc/drafts"+query)
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	AscDrafts(ctx)
	return resp.Code, resp.Body.String()
}

func TestAscDraftsLabelOK(t *testing.T) {
	for _, ok := range []string{"a", "wip/x-1.2_3", strings.Repeat("a", 64)} {
		assert.True(t, ascDraftsLabelOK(ok), ok)
	}
	for _, bad := range []string{"", "<script>", "a b", "a\n", "é", "a\"b", strings.Repeat("a", 65)} {
		assert.False(t, ascDraftsLabelOK(bad), bad)
	}
}

func TestAscDraftsBuild(t *testing.T) {
	labels := []string{"<b>x</b>", "ok"}
	for range 10 {
		labels = append(labels, "l")
	}
	rows, omitted := ascDraftsBuild(&ascv1.GetWorkspaceResponse{Omitted: 2, Heads: []*ascv1.CloudHead{
		{Cs: []byte{1, 2}, Change: []byte{9}, Labels: labels, Added: 100},
		{Cs: nil},
		{Cs: make([]byte, 65)},
		{Cs: []byte{3}, Added: 200},
	}})
	assert.Equal(t, 4, omitted)
	require.Len(t, rows, 2)
	assert.Equal(t, "03", rows[0].Commit, "newest first")
	assert.Equal(t, "0102", rows[1].Commit)
	assert.Equal(t, "09", rows[1].Change)
	assert.Len(t, rows[1].Labels, ascDraftsMaxLabels)
	assert.Equal(t, "ok", rows[1].Labels[0], "the invalid label is dropped")
}

func TestAscDraftsPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	setAscBackend(t, 1, "asc")

	t.Run("escapes labels and asks for the signed-in user only", func(t *testing.T) {
		f := &fakeCloud{heads: []*ascv1.CloudHead{{Cs: []byte{1}, Labels: []string{"<script>alert(1)</script>", "fine"}, Added: 1700000000}}}
		useFakeCloud(t, f)
		code, body := getAscDrafts(t, user2, "?owner=user1&name=x")
		assert.Equal(t, http.StatusOK, code)
		assert.NotContains(t, body, "<script>alert(1)</script>")
		assert.Contains(t, body, "fine")
		f.mu.Lock()
		defer f.mu.Unlock()
		require.NotEmpty(t, f.reqs)
		for _, r := range f.reqs {
			assert.Equal(t, user2.Name, r.Owner, "the owner is never taken from the request")
			assert.Empty(t, r.Name)
		}
		_ = user1
	})

	t.Run("escapes a hostile label even if the grammar check were missing", func(t *testing.T) {
		// The template is exercised directly with a label the grammar would drop.
		ctx, resp := contexttest.MockContext(t, "/-/asc/drafts")
		ctx.Render = templates.HTMLRenderer()
		ctx.Doer = user2
		repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
		ctx.Data["AscDraftsRepos"] = []AscDraftsRepo{{Repo: repo, Drafts: []AscDraft{{Commit: "01", Labels: []string{"<img src=x onerror=alert(1)>"}}}}}
		ctx.HTML(http.StatusOK, tplAscDrafts)
		assert.NotContains(t, resp.Body.String(), "<img src=x")
		assert.Contains(t, resp.Body.String(), "&lt;img src=x")
	})

	t.Run("anonymous and disabled", func(t *testing.T) {
		useFakeCloud(t, &fakeCloud{})
		code, _ := getAscDrafts(t, nil, "")
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _ = getAscDrafts(t, user2, "")
		assert.Equal(t, http.StatusNotFound, code)
	})

	t.Run("repositories that are not asc-backed are not asked", func(t *testing.T) {
		setAscBackend(t, 1, "git")
		defer setAscBackend(t, 1, "asc")
		f := &fakeCloud{}
		useFakeCloud(t, f)
		code, _ := getAscDrafts(t, user2, "")
		assert.Equal(t, http.StatusOK, code)
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, r := range f.reqs {
			assert.NotEqual(t, "user2/repo1", r.GetRepo().GetName())
		}
	})
}
