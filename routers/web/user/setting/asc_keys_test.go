// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// fakeKeysAsc is an asc serve with Repo.Version and the Signing calls the keys page makes. With noDefaults set it
// answers the sign-by-default calls Unimplemented, as an older asc does.
type fakeKeysAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedSigningServer

	mu         sync.Mutex
	noDefaults bool
	on         bool
	sets       []bool
	setOps     []string
}

func (f *fakeKeysAsc) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeatureSigning}}, nil
}

func (f *fakeKeysAsc) ListKeys(gocontext.Context, *ascv1.ListSigningKeysRequest) (*ascv1.ListSigningKeysResponse, error) {
	return &ascv1.ListSigningKeysResponse{}, nil
}

func (f *fakeKeysAsc) GetSigningDefaults(ctx gocontext.Context, r *ascv1.GetSigningDefaultsRequest) (*ascv1.GetSigningDefaultsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noDefaults {
		return f.UnimplementedSigningServer.GetSigningDefaults(ctx, r)
	}
	return &ascv1.GetSigningDefaultsResponse{SignByDefault: f.on}, nil
}

func (f *fakeKeysAsc) SetSigningDefaults(ctx gocontext.Context, r *ascv1.SetSigningDefaultsRequest) (*ascv1.SetSigningDefaultsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.on = r.GetSignByDefault()
	f.sets = append(f.sets, f.on)
	f.setOps = append(f.setOps, md.Get("x-asc-op-id")...)
	return &ascv1.SetSigningDefaultsResponse{SignByDefault: f.on, Updated: 1_794_000_000}, nil
}

func startKeysAsc(t *testing.T) *fakeKeysAsc {
	t.Helper()
	f := &fakeKeysAsc{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterSigningServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: accountToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, accountForwardedKey))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

func keysSwitch(t *testing.T, body string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	require.NoError(t, err)
	return doc.Find(`[data-testid="asc-keys-default"]`)
}

// The Signing keys page carries the "Sign my changesets by default" switch, which posts to asc as the person.
func TestAscKeysSignByDefaultSwitch(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startKeysAsc(t)

	r := runAccount(t, "GET", "/user/settings/asc/keys", 2, nil, nil, AscKeys)
	require.Equal(t, http.StatusOK, r.status, r.body)
	card := keysSwitch(t, r.body)
	require.Equal(t, 1, card.Length())
	assert.Equal(t, "asc_shell.key_default_title", card.Find("h2").Text(), "the mock locale names the key")
	assert.Contains(t, card.Text(), "asc_shell.key_default_body")
	assert.Equal(t, "/user/settings/asc/keys/default", card.Find("form").AttrOr("action", ""))
	box := card.Find(`input[type="checkbox"][name="value"][value="1"]`)
	require.Equal(t, 1, box.Length())
	assert.False(t, box.Is("[checked]"), "off until switched on")
	assert.NotEmpty(t, card.Find(`input[name="op"]`).AttrOr("value", ""))

	on := opForm()
	on["value"] = []string{"0", "1"}
	r = runAccount(t, "POST", "/user/settings/asc/keys/default", 2, on, nil, AscKeysDefault)
	require.Equal(t, http.StatusSeeOther, r.status, r.body)
	assert.Equal(t, "/user/settings/asc/keys", r.loc)
	assert.Equal(t, "asc_shell.key_default_on", r.flash)
	assert.Equal(t, []bool{true}, f.sets)
	assert.Equal(t, []string{on.Get("op")}, f.setOps, "the switch carries its operation id")

	r = runAccount(t, "GET", "/user/settings/asc/keys", 2, nil, nil, AscKeys)
	assert.True(t, keysSwitch(t, r.body).Find(`input[type="checkbox"]`).Is("[checked]"))

	off := opForm()
	off["value"] = []string{"0"}
	r = runAccount(t, "POST", "/user/settings/asc/keys/default", 2, off, nil, AscKeysDefault)
	require.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "asc_shell.key_default_off", r.flash)
	assert.Equal(t, []bool{true, false}, f.sets)

	for _, bad := range []url.Values{
		{"op": on["op"]},
		{"op": on["op"], "value": {"1"}},
		{"op": on["op"], "value": {"0", "2"}},
		{"op": {"not-a-uuid"}, "value": {"0"}},
	} {
		r = runAccount(t, "POST", "/user/settings/asc/keys/default", 2, bad, nil, AscKeysDefault)
		assert.Equal(t, http.StatusSeeOther, r.status, "%v", bad)
		assert.NotEmpty(t, r.flash, "%v", bad)
	}
	assert.Len(t, f.sets, 2, "a bad form never changes the value")

	r = runAccount(t, "POST", "/user/settings/asc/keys/default", 0, off, nil, AscKeysDefault)
	assert.Equal(t, http.StatusForbidden, r.status)
	assert.Len(t, f.sets, 2)
}

// An asc without the switch leaves the card out and the rest of the page works.
func TestAscKeysWithoutSignByDefault(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startKeysAsc(t)
	f.noDefaults = true
	r := runAccount(t, "GET", "/user/settings/asc/keys", 2, nil, nil, AscKeys)
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Zero(t, keysSwitch(t, r.body).Length())
	assert.Contains(t, r.body, `data-testid="asc-keys-list"`)
}
