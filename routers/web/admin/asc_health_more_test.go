// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestTheDiskCheckSaysHowSoonTheVolumeFills(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
	diskHistory.Lock()
	diskHistory.samples = nil
	diskHistory.Unlock()
	t0 := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)

	recordDisk(t0, 100<<30)
	_, known := diskFillDays(t0.Add(30*time.Minute), 101<<30, 50<<30)
	assert.False(t, known, "less than an hour seen")
	recordDisk(t0.Add(5*time.Minute), 1)
	diskHistory.Lock()
	assert.Len(t, diskHistory.samples, 1, "at most one sample every ten minutes")
	diskHistory.Unlock()

	// 2 GiB in two hours is 24 GiB a day: 48 GiB left fills in two days.
	days, known := diskFillDays(t0.Add(2*time.Hour), 102<<30, 48<<30)
	require.True(t, known)
	assert.InDelta(t, 2.0, days, 0.001)
	_, known = diskFillDays(t0.Add(2*time.Hour), 99<<30, 48<<30)
	assert.False(t, known, "a volume that shrank does not fill")

	// Samples older than a day are dropped.
	recordDisk(t0.Add(25*time.Hour), 103<<30)
	diskHistory.Lock()
	assert.Len(t, diskHistory.samples, 1)
	diskHistory.Unlock()

	for _, c := range []struct {
		pct   int
		days  float64
		known bool
		state string
		text  string
	}{
		{40, 0, false, checkOK, "check_disk_full"},
		{40, 41, true, checkOK, "check_disk_rate"},
		{40, 10, true, checkWarn, "check_disk_rate"},
		{40, 1, true, checkErr, "check_disk_rate"},
		{86, 0, false, checkWarn, "check_disk_full"},
		{96, 0, false, checkErr, "check_disk_full"},
	} {
		r := diskRow(ctx, c.pct, c.days, c.known)
		assert.Equal(t, c.state, r.State, "%+v", c)
		assert.Contains(t, string(r.Text), c.text)
	}
}

func TestTheDiskCheckOfTheDataVolume(t *testing.T) {
	defer test.MockVariableValue(&setting.AppDataPath, t.TempDir())()
	ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
	r, ok := checkDisk(ctx)
	require.True(t, ok)
	assert.Contains(t, string(r.Name), "check_disk")

	defer test.MockVariableValue(&setting.AppDataPath, "/definitely/not/here")()
	_, ok = checkDisk(ctx)
	assert.False(t, ok, "a host that does not give the numbers has no disk check")
}

func TestTheMailCheckIsTheOutgoingQueue(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
	defer test.MockVariableValue(&setting.MailService, nil)()
	_, ok := checkMail(ctx)
	assert.False(t, ok, "no mail set up, no check")
}

func writeCert(t *testing.T, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hub.example"}, NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "cert.pem")
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	// A key block first: the check reads the first certificate, wherever it is.
	b := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	require.NoError(t, os.WriteFile(path, b, 0o600))
	return path
}

func TestTheTLSCheckReadsTheCertificateTheHubServes(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
	now := time.Now()

	defer test.MockVariableValue(&setting.Protocol, setting.HTTP)()
	_, ok := checkTLS(ctx)
	assert.False(t, ok, "plain HTTP behind a proxy: the proxy's certificate is not the hub's")

	defer test.MockVariableValue(&setting.Protocol, setting.HTTPS)()
	defer test.MockVariableValue(&setting.EnableAcme, false)()
	for _, c := range []struct {
		left  time.Duration
		state string
		text  string
	}{
		{60 * 24 * time.Hour, checkOK, "check_tls_ok"},
		{5 * 24 * time.Hour, checkWarn, "check_tls_soon"},
		{-time.Hour, checkErr, "check_tls_expired"},
	} {
		defer test.MockVariableValue(&setting.CertFile, writeCert(t, now.Add(c.left)))()
		r, ok := checkTLS(ctx)
		require.True(t, ok)
		assert.Equal(t, c.state, r.State, c.text)
		assert.Contains(t, string(r.Text), c.text)
	}

	defer test.MockVariableValue(&setting.CertFile, filepath.Join(t.TempDir(), "none.pem"))()
	r, _ := checkTLS(ctx)
	assert.Equal(t, checkErr, r.State)
	assert.Contains(t, string(r.Text), "check_tls_unread")

	defer test.MockVariableValue(&setting.EnableAcme, true)()
	r, _ = checkTLS(ctx)
	assert.Equal(t, checkOK, r.State)
	assert.Contains(t, string(r.Text), "check_tls_acme")
}

func TestTheClockCheck(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
	for _, c := range []struct {
		synced, known bool
		skew, unc     time.Duration
		skewKnown     bool
		state, text   string
	}{
		{true, true, 0, 0, false, checkOK, "check_clock_ok"},
		{false, true, 0, 0, false, checkWarn, "check_clock_unsynced"},
		{true, true, 300 * time.Millisecond, 5 * time.Millisecond, true, checkOK, "check_clock_ok_asc"},
		{true, true, -5 * time.Second, 5 * time.Millisecond, true, checkWarn, "check_clock_apart"},
		{true, true, 3 * time.Second, 2 * time.Second, true, checkOK, "check_clock_ok_asc"}, // within what the call can tell
		{true, true, time.Minute, 0, true, checkErr, "check_clock_apart"},
		{false, true, 0, 0, true, checkWarn, "check_clock_unsynced"},
	} {
		r, ok := clockRow(ctx, c.synced, c.known, c.skew, c.unc, c.skewKnown)
		require.True(t, ok)
		assert.Equal(t, c.state, r.State, "%+v", c)
		assert.Contains(t, string(r.Text), c.text)
	}
	_, ok := clockRow(ctx, false, false, 0, 0, false)
	assert.False(t, ok, "nothing to compare: no check")
}

type fakeHealth struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedMaintenanceServer

	mu    sync.Mutex
	resp  *ascv1.CheckHealthResponse
	fail  bool
	asked int
}

func (*fakeHealth) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeatureHealth}}, nil
}

func (f *fakeHealth) CheckHealth(gocontext.Context, *ascv1.CheckHealthRequest) (*ascv1.CheckHealthResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	if f.fail {
		return nil, status.Error(codes.Unavailable, "no")
	}
	return &ascv1.CheckHealthResponse{
		ObjectStore: f.resp.GetObjectStore(), ServerKey: f.resp.GetServerKey(),
		ServerKeyFingerprint: f.resp.GetServerKeyFingerprint(), NowMs: time.Now().UnixMilli(),
	}, nil
}

func (f *fakeHealth) set(r *ascv1.CheckHealthResponse, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resp, f.fail = r, fail
}

func probe(s ascv1.HealthState, ms uint32, detail string) *ascv1.HealthProbe {
	return &ascv1.HealthProbe{State: s, Millis: ms, Detail: detail}
}

func TestTheAscChecksAskAscServeToProbeItself(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeHealth{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterMaintenanceServer(srv, f)
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

	ok := ascv1.HealthState_HEALTH_STATE_OK
	run := func(user int64, check func(*context.Context) (checkRow, bool)) (checkRow, bool) {
		ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
		contexttest.LoadUser(t, ctx, user)
		return check(ctx)
	}

	f.set(&ascv1.CheckHealthResponse{ObjectStore: probe(ok, 42, ""), ServerKey: probe(ok, 1, ""), ServerKeyFingerprint: "SHA256:abc"}, false)
	r, applies := run(1, checkObjectStore)
	require.True(t, applies)
	assert.Equal(t, checkOK, r.State)
	assert.Contains(t, string(r.Text), "check_store_ok")
	r, _ = run(1, checkSigningKey)
	assert.Equal(t, checkOK, r.State)
	assert.Contains(t, string(r.Text), "check_key_ok")
	r, applies = run(1, checkClock)
	require.True(t, applies)
	assert.NotEqual(t, checkErr, r.State, "the fake answers with the hub's own clock")

	f.set(&ascv1.CheckHealthResponse{
		ObjectStore: probe(ascv1.HealthState_HEALTH_STATE_FAILED, 10, "write: access denied"),
		ServerKey:   probe(ascv1.HealthState_HEALTH_STATE_NOT_CONFIGURED, 0, ""),
	}, false)
	r, _ = run(1, checkObjectStore)
	assert.Equal(t, checkErr, r.State)
	assert.Contains(t, string(r.Text), "check_store_bad")
	r, _ = run(1, checkSigningKey)
	assert.Equal(t, checkWarn, r.State)
	assert.Contains(t, string(r.Text), "check_key_none")

	f.set(&ascv1.CheckHealthResponse{ObjectStore: probe(ok, 1, ""), ServerKey: probe(ascv1.HealthState_HEALTH_STATE_FAILED, 1, "the key does not sign")}, false)
	r, _ = run(1, checkSigningKey)
	assert.Equal(t, checkErr, r.State)
	assert.Contains(t, string(r.Text), "check_key_bad")

	// asc serve does not answer: the checks could not run.
	f.set(nil, true)
	r, applies = run(1, checkObjectStore)
	require.True(t, applies)
	assert.Equal(t, checkNoRun, r.State)
	assert.Contains(t, string(r.Text), "check_asc_unread")
	r, _ = run(1, checkSigningKey)
	assert.Equal(t, checkNoRun, r.State)

	// An answer without the probes is a bad answer.
	f.set(&ascv1.CheckHealthResponse{}, false)
	r, _ = run(1, checkObjectStore)
	assert.Equal(t, checkNoRun, r.State)

	// Someone who is not a site administrator does not ask asc serve.
	f.mu.Lock()
	n := f.asked
	f.mu.Unlock()
	_, applies = run(2, checkObjectStore)
	assert.False(t, applies)
	f.mu.Lock()
	assert.Equal(t, n, f.asked)
	f.mu.Unlock()

	// With asc off there is no asc check.
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	_, applies = run(1, checkSigningKey)
	assert.False(t, applies)
}

func TestLoadHealthMeasuresTheClocks(t *testing.T) {
	f := &fakeHealth{}
	srv := grpc.NewServer()
	ascv1.RegisterMaintenanceServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	defer srv.Stop()
	conn, err := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	ok := ascv1.HealthState_HEALTH_STATE_OK
	f.set(&ascv1.CheckHealthResponse{ObjectStore: probe(ok, 1, ""), ServerKey: probe(ok, 1, "")}, false)
	// The hub's clock is ten minutes behind asc serve's.
	behind := func() time.Time { return time.Now().Add(-10 * time.Minute) }
	h, err := asctrust.LoadHealth(gocontext.Background(), conn, behind)
	require.NoError(t, err)
	assert.InDelta(t, (10 * time.Minute).Seconds(), h.Skew.Seconds(), 1)
	assert.Positive(t, h.Uncertainty)
}
