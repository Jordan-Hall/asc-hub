// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeCloud struct {
	ascv1.UnimplementedCloudServer
	mu    sync.Mutex
	reqs  []*ascv1.GetWorkspaceRequest
	heads []*ascv1.CloudHead
	local []*ascv1.LocalDraft
	// version is the workspace version (1 when unset); stale is how many updates answer ABORTED first.
	version uint64
	stale   int
	updates []*ascv1.UpdateWorkspaceRequest
	// users is the forwarded user of each update.
	users []string
}

func (f *fakeCloud) GetWorkspace(_ context.Context, req *ascv1.GetWorkspaceRequest) (*ascv1.GetWorkspaceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	heads := make([]*ascv1.CloudHead, 0, len(f.heads))
	for _, h := range f.heads {
		if h.GetHiddenAt() == 0 || req.GetIncludeHidden() {
			heads = append(heads, h)
		}
	}
	return &ascv1.GetWorkspaceResponse{Version: max(f.version, 1), Heads: heads, Local: f.local}, nil
}

func (f *fakeCloud) UpdateWorkspace(ctx context.Context, req *ascv1.UpdateWorkspaceRequest) (*ascv1.UpdateWorkspaceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	md, _ := metadata.FromIncomingContext(ctx)
	f.users = append(f.users, strings.Join(md.Get("x-asc-forwarded-user"), ","))
	f.updates = append(f.updates, req)
	if f.stale > 0 {
		f.stale--
		f.version = max(f.version, 1) + 1 // another device wrote in between
		return nil, status.Error(codes.Aborted, "version conflict")
	}
	if req.GetBaseVersion() != max(f.version, 1) {
		return nil, status.Error(codes.Aborted, "version conflict")
	}
	for _, h := range f.heads {
		for _, cs := range req.GetHide() {
			if bytes.Equal(cs, h.GetCs()) {
				h.HiddenAt = 1700000500
			}
		}
		for _, cs := range req.GetUnhide() {
			if bytes.Equal(cs, h.GetCs()) {
				h.HiddenAt = 0
			}
		}
	}
	f.version = max(f.version, 1) + 1
	return &ascv1.UpdateWorkspaceResponse{Version: f.version}, nil
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

func TestAscDraftsLocation(t *testing.T) {
	rows, omitted := ascDraftsBuild(&ascv1.GetWorkspaceResponse{
		Heads: []*ascv1.CloudHead{
			{Cs: []byte{1}, Added: 300, Devices: []string{"laptop", "<b>", "desk", "laptop"}},
			{Cs: []byte{2}, Added: 200},
		},
		Local: []*ascv1.LocalDraft{
			{Change: []byte{7}, Added: 100, Device: "laptop", State: ascv1.LocalDraftState_LOCAL_DRAFT_STATE_TOO_LARGE},
			{Change: []byte{8}, Added: 50, Device: "a/b"},
			{Change: nil, Device: "laptop"},
			{Change: []byte{9}, Added: 40, Device: "desk", State: 99},
		},
	})
	assert.Equal(t, 2, omitted, "a bad device name and a missing change id")
	require.Len(t, rows, 4)
	assert.Empty(t, rows[3].Reason, "an unknown state gives no reason")
	assert.True(t, rows[0].OnHub)
	assert.Equal(t, []string{"desk", "laptop"}, rows[0].Devices, "valid, distinct, sorted")
	assert.True(t, rows[1].OnHub)
	assert.Empty(t, rows[1].Devices)
	assert.False(t, rows[2].OnHub)
	assert.Equal(t, "07", rows[2].Change)
	assert.Empty(t, rows[2].Commit)
	assert.Equal(t, []string{"laptop"}, rows[2].Devices)
	assert.Equal(t, "asc_work.drafts_why_too_large", rows[2].Reason)

	many := make([]string, 0, 20)
	for i := range 20 {
		many = append(many, strings.Repeat("d", i+1))
	}
	assert.Len(t, ascDraftsDevices(many), ascDraftsMaxDevices)
	assert.False(t, ascDraftsDeviceOK(strings.Repeat("d", 65)))
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
		ctx.Data["AscDraftsAnyVisible"] = true
		ctx.HTML(http.StatusOK, tplAscDrafts)
		assert.NotContains(t, resp.Body.String(), "<img src=x")
		assert.Contains(t, resp.Body.String(), "&lt;img src=x")
	})

	t.Run("says where each draft is", func(t *testing.T) {
		f := &fakeCloud{
			heads: []*ascv1.CloudHead{
				{Cs: []byte{1}, Labels: []string{"both"}, Added: 1700000300, Devices: []string{"laptop"}},
				{Cs: []byte{2}, Labels: []string{"hubonly"}, Added: 1700000200},
			},
			local: []*ascv1.LocalDraft{{Change: []byte{0xab}, Added: 1700000100, Device: "desk", State: ascv1.LocalDraftState_LOCAL_DRAFT_STATE_WAITING}},
		}
		useFakeCloud(t, f)
		code, body := getAscDrafts(t, user2, "")
		assert.Equal(t, http.StatusOK, code)
		// The test renderer shows locale keys.
		assert.Contains(t, body, "asc_work.drafts_on_devices_and_hub")
		assert.Contains(t, body, "asc_work.drafts_on_hub")
		assert.Contains(t, body, "asc_work.drafts_only_on")
		assert.Contains(t, body, "asc_work.drafts_why_waiting")
		assert.Contains(t, body, "asc stack resume ab")
		assert.Contains(t, body, `data-testid="drafts-sync"`, "the page says drafts upload only when the cloud is on")
		assert.Contains(t, body, "asc_work.drafts_sync")
		assert.Contains(t, body, "asc config asc.cloud off")
		f.mu.Lock()
		defer f.mu.Unlock()
		require.NotEmpty(t, f.reqs)
		assert.True(t, f.reqs[0].GetIncludeDevices())
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

func TestAscDraftsRefs(t *testing.T) {
	assert.Equal(t, []byte{0xab, 0x01}, ascDraftsCommit("ab01"))
	for _, bad := range []string{"", "AB01", "abc", "zz", strings.Repeat("ab", ascDraftsMaxIDBytes+1)} {
		assert.Nil(t, ascDraftsCommit(bad), bad)
	}
	assert.Equal(t, ascDraftsRef{repo: 7, cs: "ab"}, ascDraftsParseDiscarded("7.ab"))
	for _, bad := range []string{"", "7", "x.ab", "0.ab", "-1.ab", "7.AB", "7.<b>"} {
		assert.Equal(t, ascDraftsRef{}, ascDraftsParseDiscarded(bad), bad)
	}
}

func postAscDrafts(t *testing.T, doer *user_model.User, action, query string) (resp *httptest.ResponseRecorder, ok, failed string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /-/asc/drafts/"+action+query)
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	if action == "discard" {
		AscDraftsDiscard(ctx)
	} else {
		AscDraftsUndo(ctx)
	}
	return resp, ctx.Flash.SuccessMsg, ctx.Flash.ErrorMsg
}

func TestAscDraftsDiscardAndUndo(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	setAscBackend(t, 1, "asc")
	setAscBackend(t, 2, "asc")
	defer setAscBackend(t, 2, "git")
	defer test.MockVariableValue(&setting.Asc.ForwardedKey, bytes.Repeat([]byte{7}, 32))()

	t.Run("discard hides the draft as the signed-in person, the page offers undo, undo shows it again", func(t *testing.T) {
		f := &fakeCloud{heads: []*ascv1.CloudHead{{Cs: []byte{1}, Labels: []string{"spike"}, Added: 1700000000}}}
		useFakeCloud(t, f)
		resp, _, failed := postAscDrafts(t, user2, "discard", "?repo=1&cs=01")
		assert.Empty(t, failed)
		assert.Equal(t, http.StatusSeeOther, resp.Code)
		assert.Equal(t, "/-/asc/drafts?discarded=1.01", resp.Header().Get("Location"))
		f.mu.Lock()
		require.Len(t, f.updates, 1)
		assert.Equal(t, [][]byte{{1}}, f.updates[0].GetHide())
		assert.Equal(t, uint64(1), f.updates[0].GetBaseVersion())
		assert.Equal(t, "2:user2", f.users[0], "the claim names the signed-in person")
		f.mu.Unlock()

		code, body := getAscDrafts(t, user2, "?discarded=1.01")
		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, `data-testid="drafts-discarded"`, "the banner")
		assert.Contains(t, body, `data-testid="drafts-discarded-list"`)
		assert.Contains(t, body, "asc_work.drafts_empty_title", "nothing left in the main list")
		assert.Contains(t, body, `action="/-/asc/drafts/undo"`)
		assert.NotContains(t, body, `action="/-/asc/drafts/discard"`)

		// A value that names no discarded draft gives no banner, only the list.
		_, body = getAscDrafts(t, user2, "?discarded=1.02")
		assert.NotContains(t, body, `data-testid="drafts-discarded"`)
		assert.Contains(t, body, `data-testid="drafts-discarded-list"`)

		resp, ok, failed := postAscDrafts(t, user2, "undo", "?repo=1&cs=01")
		assert.Empty(t, failed)
		assert.Equal(t, "asc_work.drafts_restored", ok)
		assert.Equal(t, "/-/asc/drafts", resp.Header().Get("Location"))
		f.mu.Lock()
		require.Len(t, f.updates, 2)
		assert.Equal(t, [][]byte{{1}}, f.updates[1].GetUnhide())
		f.mu.Unlock()

		_, body = getAscDrafts(t, user2, "")
		assert.NotContains(t, body, `data-testid="drafts-discarded-list"`)
		assert.Contains(t, body, `action="/-/asc/drafts/discard"`)
		assert.Contains(t, body, "asc stack resume 01", "Continue shows the command")
	})

	t.Run("a second click changes nothing and a lost race is tried again", func(t *testing.T) {
		f := &fakeCloud{heads: []*ascv1.CloudHead{{Cs: []byte{1}, Added: 1700000000, HiddenAt: 1700000100}}}
		useFakeCloud(t, f)
		resp, _, failed := postAscDrafts(t, user2, "discard", "?repo=1&cs=01")
		assert.Empty(t, failed)
		assert.Equal(t, "/-/asc/drafts?discarded=1.01", resp.Header().Get("Location"))
		f.mu.Lock()
		assert.Empty(t, f.updates, "already discarded")
		f.stale = 1
		f.mu.Unlock()
		_, ok, failed := postAscDrafts(t, user2, "undo", "?repo=1&cs=01")
		assert.Empty(t, failed)
		assert.Equal(t, "asc_work.drafts_restored", ok)
		f.mu.Lock()
		defer f.mu.Unlock()
		require.Len(t, f.updates, 2)
		assert.Equal(t, uint64(2), f.updates[1].GetBaseVersion(), "read again after the conflict")
		assert.Zero(t, f.heads[0].GetHiddenAt())
	})

	t.Run("a draft that is not there, a bad id or a repository the person cannot read changes nothing", func(t *testing.T) {
		f := &fakeCloud{heads: []*ascv1.CloudHead{{Cs: []byte{1}, Added: 1700000000}}}
		useFakeCloud(t, f)
		for _, q := range []string{"?repo=1&cs=02", "?repo=1&cs=ZZ", "?repo=1&cs=", "?repo=99999&cs=01", "?repo=3&cs=01"} {
			resp, _, failed := postAscDrafts(t, user2, "discard", q)
			assert.Equal(t, "asc_work.drafts_act_gone", failed, q)
			assert.Equal(t, "/-/asc/drafts", resp.Header().Get("Location"), q)
		}
		// Repository 2 is user2's private repository: user4 cannot read it.
		_, _, failed := postAscDrafts(t, user4, "discard", "?repo=2&cs=01")
		assert.Equal(t, "asc_work.drafts_act_gone", failed)
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Empty(t, f.updates)
	})

	t.Run("asc failing is said", func(t *testing.T) {
		f := &fakeCloud{heads: []*ascv1.CloudHead{{Cs: []byte{1}, Added: 1700000000}}, stale: 5}
		useFakeCloud(t, f)
		_, _, failed := postAscDrafts(t, user2, "discard", "?repo=1&cs=01")
		assert.Equal(t, "asc_work.drafts_act_failed", failed)
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Len(t, f.updates, ascDraftsStaleTries)
	})

	t.Run("anonymous and disabled", func(t *testing.T) {
		useFakeCloud(t, &fakeCloud{})
		resp, _, _ := postAscDrafts(t, nil, "discard", "?repo=1&cs=01")
		assert.Equal(t, http.StatusNotFound, resp.Code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		resp, _, _ = postAscDrafts(t, user2, "undo", "?repo=1&cs=01")
		assert.Equal(t, http.StatusNotFound, resp.Code)
	})
}
