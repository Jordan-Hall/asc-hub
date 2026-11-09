// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"net"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/mailer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeUsage struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedUsageServer

	mu      sync.Mutex
	storage *ascv1.UsageMeter
	fail    bool
	asked   []*ascv1.GetUsageRequest
}

func (*fakeUsage) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeatureUsage}}, nil
}

func (f *fakeUsage) GetUsage(_ gocontext.Context, r *ascv1.GetUsageRequest) (*ascv1.GetUsageResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, r)
	if f.fail {
		return nil, status.Error(codes.Internal, "no")
	}
	return &ascv1.GetUsageResponse{Period: "2026-11", Storage: f.storage}, nil
}

func TestTheSystemCardShowsObjectStorage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeUsage{storage: &ascv1.UsageMeter{Used: 3 << 30}}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterUsageServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	defer c.Close()
	defer asctrust.UseConn(c)()
	defer test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef"))()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	row := func(user int64) (statusRow, bool) {
		ctx, _ := contexttest.MockContext(t, "GET /admin")
		contexttest.LoadUser(t, ctx, user)
		return storageRow(ctx)
	}
	r, ok := row(1)
	require.True(t, ok)
	assert.Equal(t, "ok", r.State)
	assert.Contains(t, string(r.Text), "sys_storage_used")
	require.Len(t, f.asked, 1)
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_INSTANCE, f.asked[0].GetOwner().GetKind(), "the whole server")

	f.storage = &ascv1.UsageMeter{Used: 95, Limit: 100}
	r, _ = row(1)
	assert.Equal(t, "warn", r.State)
	assert.Contains(t, string(r.Label), "state_nearly_full")
	f.storage = &ascv1.UsageMeter{Used: 120, Limit: 100, Over: true}
	r, _ = row(1)
	assert.Equal(t, "err", r.State)
	f.fail = true
	r, ok = row(1)
	require.True(t, ok)
	assert.Equal(t, "warn", r.State)
	assert.Contains(t, string(r.Text), "sys_storage_unread")

	// Someone who is not a site administrator gets no row, and asc is not asked.
	n := len(f.asked)
	_, ok = row(2)
	assert.False(t, ok)
	assert.Len(t, f.asked, n)
}

func TestTheSystemCardShowsTheMailQueue(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin")
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		q     mailer.QueueState
		state string
		text  string
	}{
		{mailer.QueueState{}, "warn", "sys_mail_off"},
		{mailer.QueueState{Enabled: true}, "ok", "sys_mail_empty"},
		{mailer.QueueState{Enabled: true, Waiting: 3}, "ok", "sys_mail_waiting"},
		{mailer.QueueState{Enabled: true, Waiting: 3, Oldest: now.Add(-time.Minute)}, "ok", "sys_mail_waiting_age"},
		{mailer.QueueState{Enabled: true, Waiting: 3, Oldest: now.Add(-10 * time.Minute)}, "warn", "sys_mail_waiting_age"},
		{mailer.QueueState{Enabled: true, LastFailed: now}, "err", "sys_mail_failing"},
		{mailer.QueueState{Enabled: true, Waiting: 2, LastSent: now.Add(-time.Hour), LastFailed: now}, "err", "sys_mail_failing_waiting"},
		{mailer.QueueState{Enabled: true, LastSent: now, LastFailed: now.Add(-time.Hour)}, "ok", "sys_mail_empty"},
	} {
		r := mailRow(ctx, c.q, now)
		assert.Equal(t, c.state, r.State, c.text)
		assert.Contains(t, string(r.Text), c.text)
	}
}
