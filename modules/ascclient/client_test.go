// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"context"
	"strings"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestParseTarget(t *testing.T) {
	for raw, want := range map[string]target{
		"127.0.0.1:7890":          {addr: "127.0.0.1:7890", loopback: true},
		"localhost":               {addr: "localhost:7890", loopback: true},
		"http://[::1]:9":          {addr: "[::1]:9", loopback: true},
		"https://asc.example.com": {addr: "asc.example.com", useTLS: true},
		"asc.internal:7890":       {addr: "asc.internal:7890"},
	} {
		got, err := parseTarget(raw)
		require.NoError(t, err, raw)
		if raw == "https://asc.example.com" {
			// no port: a bare host gets the asc serve default
			want.addr = "asc.example.com:7890"
		}
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		"", " ", "ftp://x:1", "https://u:p@x", "https://x/path", "https://x?q", "https://x#f", "http://",
	} {
		_, err := parseTarget(raw)
		assert.Error(t, err, raw)
	}
}

func TestNewRefusesPlaintextToARemoteHost(t *testing.T) {
	_, err := New(Config{Target: "asc.example.com:7890"})
	require.ErrorContains(t, err, "plaintext")
	_, err = New(Config{Target: "http://10.1.2.3:7890"})
	require.ErrorContains(t, err, "plaintext")

	c, err := New(Config{Target: "10.1.2.3:7890", AllowInsecure: true})
	require.NoError(t, err)
	require.NoError(t, c.Close())
	c, err = New(Config{Target: "https://asc.example.com"})
	require.NoError(t, err)
	require.NoError(t, c.Close())
	c, err = New(Config{Target: "127.0.0.1:1"})
	require.NoError(t, err)
	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "Close is idempotent")
}

func TestConfigValidation(t *testing.T) {
	for name, cfg := range map[string]Config{
		"pool too big":     {Target: "127.0.0.1:1", PoolSize: MaxPoolSize + 1},
		"negative pool":    {Target: "127.0.0.1:1", PoolSize: -1},
		"decoded too big":  {Target: "127.0.0.1:1", MaxDecodedBytes: 2 << 30},
		"batch too big":    {Target: "127.0.0.1:1", PutBatchBytes: 32 << 20},
		"attempts":         {Target: "127.0.0.1:1", Retry: RetryConfig{MaxAttempts: 99}},
		"cap below base":   {Target: "127.0.0.1:1", Retry: RetryConfig{Base: time.Second, Cap: time.Millisecond}},
		"bad token":        {Target: "127.0.0.1:1", InternalToken: "a b"},
		"negative timeout": {Target: "127.0.0.1:1", CallTimeout: -time.Second},
	} {
		_, err := New(cfg)
		assert.Error(t, err, name)
	}
}

func TestVersionCheckAndRefusalWithoutCredential(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)

	_, err := c.Repo().Version(t.Context(), &ascv1.VersionRequest{})
	require.ErrorIs(t, err, ErrNoCredential)
	assert.Empty(t, s.repo.seenCalls(), "an unauthenticated call never reaches the wire")

	resp, err := c.CheckServer(WithSystem(t.Context()), "search.v1")
	require.NoError(t, err)
	assert.Equal(t, "9.9.9", resp.GetServerVersion())

	_, err = c.CheckServer(WithSystem(t.Context()), "search.v1", "nope.v9")
	require.ErrorContains(t, err, "nope.v9")
}

func TestUserTokenIsForwardedAndSystemUsesTheInternalToken(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)

	ctx := metadata.AppendToOutgoingContext(userCtx(t, "user-pat"), "authorization", "Bearer smuggled", "traceparent", "00-abc")
	_, err := c.Repo().Version(ctx, &ascv1.VersionRequest{})
	require.NoError(t, err)
	_, err = c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.NoError(t, err)

	calls := s.repo.seenCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"Bearer user-pat"}, calls[0].md.Get("authorization"), "a caller cannot smuggle its own header")
	assert.Equal(t, []string{"00-abc"}, calls[0].md.Get("traceparent"), "other metadata passes through")
	assert.Equal(t, []string{"Bearer internal-token"}, calls[1].md.Get("authorization"))
	for _, call := range calls {
		assert.True(t, strings.HasPrefix(call.md.Get("x-asc-client")[0], ClientName+"/"))
		assert.Empty(t, call.md.Get("x-asc-op-id"), "Version is read-only")
	}
}

func TestSystemCallWithoutInternalTokenFails(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.InternalToken = "" })
	_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.ErrorIs(t, err, ErrNoInternalToken)
	assert.Empty(t, s.repo.seenCalls())
}

func TestTokenValidation(t *testing.T) {
	for _, bad := range []string{"", "a b", "a\r\nx-evil: 1", "tab\t", "nul\x00", "café", strings.Repeat("a", MaxTokenBytes+1)} {
		_, err := WithUserToken(t.Context(), bad)
		assert.Error(t, err, "%q", bad)
	}
	_, err := WithUserToken(t.Context(), strings.Repeat("a", MaxTokenBytes))
	assert.NoError(t, err)
}

func TestMutatingCallsCarryOneOpIDAcrossRetries(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	primeBudget(c, 100)
	s.repo.failures = []error{status.Error(codes.Unavailable, "down"), status.Error(codes.Unavailable, "down")}

	_, err := c.Repo().UpdateRefs(userCtx(t, "tok"), &ascv1.UpdateRefsRequest{})
	require.NoError(t, err)

	calls := s.repo.seenCalls()
	require.Len(t, calls, 3)
	id := calls[0].md.Get("x-asc-op-id")
	require.Len(t, id, 1)
	parsed, err := uuid.Parse(id[0])
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), parsed.Version())
	for _, call := range calls {
		assert.Equal(t, id, call.md.Get("x-asc-op-id"), "a retry reuses the op id")
	}

	want := uuid.MustParse("0190b7f5-8d5e-7000-8000-000000000001")
	_, err = c.Repo().UpdateRefs(WithOpID(userCtx(t, "tok"), want), &ascv1.UpdateRefsRequest{})
	require.NoError(t, err)
	last := s.repo.seenCalls()[3]
	assert.Equal(t, []string{want.String()}, last.md.Get("x-asc-op-id"))
}

func TestOnlyTransientCodesAreRetried(t *testing.T) {
	withDetail := func(code codes.Code, detail string) error {
		st, err := status.New(code, "x").WithDetails(&ascv1.ErrorDetail{Code: detail})
		require.NoError(t, err)
		return st.Err()
	}
	for name, tc := range map[string]struct {
		err   error
		calls int
	}{
		"unavailable":       {status.Error(codes.Unavailable, "x"), 2},
		"deadline":          {status.Error(codes.DeadlineExceeded, "x"), 2},
		"exhausted":         {status.Error(codes.ResourceExhausted, "x"), 2},
		"rate limited":      {withDetail(codes.ResourceExhausted, CodeRateLimited), 2},
		"quota":             {withDetail(codes.ResourceExhausted, "QUOTA_EXCEEDED"), 1},
		"permission denied": {status.Error(codes.PermissionDenied, "x"), 1},
		"unauthenticated":   {status.Error(codes.Unauthenticated, "x"), 1},
		"failed precond":    {withDetail(codes.FailedPrecondition, "REF_CONFLICT"), 1},
		"internal":          {status.Error(codes.Internal, "x"), 1},
	} {
		t.Run(name, func(t *testing.T) {
			s := startFake(t)
			c := newTestClient(t, s, nil)
			primeBudget(c, 100)
			s.repo.failures = []error{tc.err}
			_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
			assert.Equal(t, tc.calls, len(s.repo.seenCalls()))
			if tc.calls == 1 {
				require.Error(t, err)
				assert.Equal(t, status.Code(tc.err), status.Code(err))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestErrorDetailAccessors(t *testing.T) {
	st, err := status.New(codes.FailedPrecondition, "x").WithDetails(&ascv1.ErrorDetail{Code: "REF_CONFLICT", Paths: []string{"a"}})
	require.NoError(t, err)
	assert.Equal(t, "REF_CONFLICT", ErrorCode(st.Err()))
	assert.Equal(t, []string{"a"}, ErrorDetail(st.Err()).GetPaths())
	assert.Empty(t, ErrorCode(status.Error(codes.Internal, "x")))
	assert.Empty(t, ErrorCode(context.Canceled))
}

func TestRetriesGiveUpAfterMaxAttempts(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.Retry.MaxAttempts = 3 })
	primeBudget(c, 100)
	s.repo.failures = []error{
		status.Error(codes.Unavailable, "1"), status.Error(codes.Unavailable, "2"),
		status.Error(codes.Unavailable, "3"), status.Error(codes.Unavailable, "4"),
	}
	_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))
	assert.Len(t, s.repo.seenCalls(), 3)
}

func TestRetryBudgetFailsFast(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil) // no priming: 1 request, 10% allows no retry
	s.repo.failures = []error{status.Error(codes.Unavailable, "down")}
	_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))
	assert.Len(t, s.repo.seenCalls(), 1)
}

func TestRetryAfterHintIsHonouredAndCapped(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.Retry.Cap = 50 * time.Millisecond })
	primeBudget(c, 100)
	c.retry.jitter = func() float64 { return 0 }
	var slept []time.Duration
	c.retry.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }

	s.repo.failures = []error{status.Error(codes.Unavailable, "a"), status.Error(codes.Unavailable, "b")}
	s.repo.trailer = metadata.Pairs(RetryAfterTrailer, "30")
	_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{30 * time.Millisecond, 30 * time.Millisecond}, slept)

	slept = nil
	s.repo.failures = []error{status.Error(codes.Unavailable, "a")}
	s.repo.trailer = metadata.Pairs(RetryAfterTrailer, "3600000")
	_, err = c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{50 * time.Millisecond}, slept, "a hostile hint is capped")

	slept = nil
	s.repo.failures = []error{status.Error(codes.Unavailable, "a")}
	s.repo.trailer = metadata.Pairs(RetryAfterTrailer, "-5")
	_, err = c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.NoError(t, err)
	assert.Equal(t, []time.Duration{0}, slept, "a malformed hint is ignored")
}

func TestRetryStopsWhenTheDeadlineCannotCoverTheBackoff(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.Retry.Base, cfg.Retry.Cap = time.Second, time.Second })
	primeBudget(c, 100)
	c.retry.jitter = func() float64 { return 1 }
	s.repo.failures = []error{status.Error(codes.Unavailable, "down"), status.Error(codes.Unavailable, "down")}
	ctx, cancel := context.WithTimeout(WithSystem(t.Context()), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Repo().Version(ctx, &ascv1.VersionRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err))
	assert.Less(t, time.Since(start), 250*time.Millisecond)
	assert.Len(t, s.repo.seenCalls(), 1)
}

func TestDelayIsFullJitterUnderAnExponentialCeiling(t *testing.T) {
	r := newRetrier(RetryConfig{MaxAttempts: 5, Base: 200 * time.Millisecond, Cap: 10 * time.Second})
	r.jitter = func() float64 { return 0.5 }
	assert.Equal(t, 100*time.Millisecond, r.delay(1, 0))
	assert.Equal(t, 200*time.Millisecond, r.delay(2, 0))
	assert.Equal(t, 5*time.Second, r.delay(30, 0))
	assert.Equal(t, 5*time.Second, r.delay(1<<30, 0), "no overflow")
	r.jitter = func() float64 { return 0 }
	assert.Zero(t, r.delay(3, 0))
	assert.Equal(t, 2*time.Second, r.delay(1, 2*time.Second))
	real := newRetrier(RetryConfig{Base: time.Second, Cap: time.Second})
	for range 1000 {
		d := real.delay(1, 0)
		require.True(t, d >= 0 && d < time.Second)
	}
}

func TestBudgetIsTenPercentOverASlidingWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	b := newBudget()
	b.now = func() time.Time { return now }
	assert.False(t, b.tryRetry(), "no traffic, no retries")
	for range 20 {
		b.record()
	}
	assert.True(t, b.tryRetry())
	assert.True(t, b.tryRetry())
	assert.False(t, b.tryRetry(), "3 retries need 30 requests")
	for range 10 {
		b.record()
	}
	assert.True(t, b.tryRetry())
	now = now.Add(11 * time.Second)
	assert.False(t, b.tryRetry(), "the window has slid past every request")
	b.record()
	assert.False(t, b.tryRetry())
	for range 10 {
		b.record()
	}
	assert.True(t, b.tryRetry())
}

func TestCallTimeoutIsPerAttemptAndRetryable(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.CallTimeout = 50 * time.Millisecond })
	primeBudget(c, 100)
	s.repo.failures = []error{status.Error(codes.DeadlineExceeded, "slow")}
	_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.NoError(t, err)
	assert.Len(t, s.repo.seenCalls(), 2)
}

func TestClosedClientRefusesCalls(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, nil)
	require.NoError(t, c.Close())
	_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
	require.ErrorIs(t, err, ErrClosed)
}

func TestPoolSpreadsCallsAcrossConnections(t *testing.T) {
	s := startFake(t)
	c := newTestClient(t, s, func(cfg *Config) { cfg.PoolSize = 3 })
	for range 6 {
		_, err := c.Repo().Version(WithSystem(t.Context()), &ascv1.VersionRequest{})
		require.NoError(t, err)
	}
	assert.Equal(t, uint32(6), c.next.Load(), "round robin over the pool")
	assert.Len(t, c.conns, 3)
}

func TestReadOnlyTableMatchesTheProtoServices(t *testing.T) {
	// Every RPC the table lists must exist, so a rename in the proto cannot silently turn a
	// read into an op-id-bearing call or, worse, leave a write marked read-only.
	known := map[string]bool{}
	for _, d := range []struct {
		name string
		desc []string
	}{
		{"Repo", methodNames(&ascv1.Repo_ServiceDesc)},
		{"Changes", methodNames(&ascv1.Changes_ServiceDesc)},
		{"Cloud", methodNames(&ascv1.Cloud_ServiceDesc)},
		{"Projects", methodNames(&ascv1.Projects_ServiceDesc)},
		{"CodeIntel", methodNames(&ascv1.CodeIntel_ServiceDesc)},
	} {
		for _, m := range d.desc {
			known["/asc.v1."+d.name+"/"+m] = true
		}
	}
	for m := range readOnly {
		assert.True(t, known[m], "readOnly lists unknown method %s", m)
	}
	for _, write := range []string{
		"UpdateRefs", "Commit", "Merge", "Apply", "CopyClosure", "RepoAdmin", "ImportGitPush", "Ingest", "Rebase",
	} {
		assert.False(t, readOnly["/asc.v1.Repo/"+write], write)
	}
	for _, write := range []string{"RestackChanges", "SetReviewState", "RequestMerge"} {
		assert.False(t, readOnly["/asc.v1.Changes/"+write], write)
	}
	for _, write := range []string{"UpdateWorkspace", "Purge"} {
		assert.False(t, readOnly["/asc.v1.Cloud/"+write], write)
	}
}

func methodNames(d *grpc.ServiceDesc) []string {
	var names []string
	for _, m := range d.Methods {
		names = append(names, m.MethodName)
	}
	for _, m := range d.Streams {
		names = append(names, m.StreamName)
	}
	return names
}
