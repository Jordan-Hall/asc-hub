// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascclient is the Forgejo-side client of `asc serve` (SPEC section 7, PLAN unit K06).
//
// A Client owns a small pool of HTTP/2 connections and talks to every asc.v1 service through the
// stubs in forgejo.org/modules/ascproto/ascv1. It adds, to every call:
//
//   - bearer authentication: the user's token for user-initiated calls (WithUserToken) or the
//     internal token for system calls (WithSystem). A context with neither is refused;
//   - x-asc-client, and x-asc-op-id (a UUIDv7 fixed per logical call) on every mutating call;
//   - retries of UNAVAILABLE, DEADLINE_EXCEEDED and RATE_LIMITED, with full-jitter backoff, the
//     server's retry-after-ms hint, and a budget of 10% of requests in a 10 s window.
//
// On top of the raw stubs it offers HasObjects, GetObjects and PutObjects helpers that bound the
// batch sizes, decode the stored bytes under a size cap, verify every object against its BLAKE3
// id, and keep an LRU of the decoded, verified objects (default 256 MiB).
package ascclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// ErrClosed is returned by calls made after Close.
var ErrClosed = errors.New("ascclient: client closed")

// Client is safe for concurrent use. It implements grpc.ClientConnInterface, so the generated
// constructors (ascv1.NewRepoClient and so on) work on it directly.
type Client struct {
	cfg    Config
	retry  *retrier
	conns  []*grpc.ClientConn
	next   atomic.Uint32
	cache  *objectCache
	dec    *zstd.Decoder
	enc    *zstd.Encoder
	closed atomic.Bool
}

var _ grpc.ClientConnInterface = (*Client)(nil)

// New builds a Client. It does not dial: connections open on first use.
func New(cfg Config) (*Client, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	t, err := parseTarget(cfg.Target)
	if err != nil {
		return nil, err
	}
	var creds credentials.TransportCredentials
	switch {
	case t.useTLS || cfg.TLS != nil:
		tc := cfg.TLS
		if tc == nil {
			tc = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			tc = tc.Clone()
			tc.MinVersion = max(tc.MinVersion, tls.VersionTLS12)
		}
		creds = credentials.NewTLS(tc)
	case t.loopback || cfg.AllowInsecure:
		creds = insecure.NewCredentials()
	default:
		return nil, fmt.Errorf("ascclient: refusing plaintext to non-loopback target %q (use https or AllowInsecure)", t.addr)
	}

	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(uint64(cfg.MaxDecodedBytes)), zstd.WithDecoderMaxWindow(zstdMaxWindow))
	if err != nil {
		return nil, fmt.Errorf("ascclient: zstd decoder: %w", err)
	}
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		dec.Close()
		return nil, fmt.Errorf("ascclient: zstd encoder: %w", err)
	}
	c := &Client{cfg: cfg, retry: newRetrier(cfg.Retry), cache: newObjectCache(cfg.CacheBytes), dec: dec, enc: enc}
	for range cfg.PoolSize {
		conn, err := grpc.NewClient(t.addr,
			grpc.WithTransportCredentials(creds),
			grpc.WithUserAgent(userAgent()),
			grpc.WithChainUnaryInterceptor(c.unaryInterceptor),
			grpc.WithChainStreamInterceptor(c.streamInterceptor),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(MaxMessageBytes),
				grpc.MaxCallSendMsgSize(MaxMessageBytes),
			),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: false}),
		)
		if err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("ascclient: %w", err)
		}
		c.conns = append(c.conns, conn)
	}
	return c, nil
}

// Close closes every connection. Calls in flight fail.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	var errs []error
	for _, conn := range c.conns {
		errs = append(errs, conn.Close())
	}
	c.dec.Close()
	_ = c.enc.Close()
	return errors.Join(errs...)
}

func (c *Client) pick() *grpc.ClientConn {
	return c.conns[int(c.next.Add(1)-1)%len(c.conns)]
}

// Invoke implements grpc.ClientConnInterface.
func (c *Client) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	if c.closed.Load() {
		return ErrClosed
	}
	return c.pick().Invoke(ctx, method, args, reply, opts...)
}

// NewStream implements grpc.ClientConnInterface.
func (c *Client) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	return c.pick().NewStream(ctx, desc, method, opts...)
}

// Repo, Changes, Projects, Cloud and CodeIntel are the asc.v1 service clients. Workspace is an
// IPC-only service and has no accessor here.
func (c *Client) Repo() ascv1.RepoClient           { return ascv1.NewRepoClient(c) }
func (c *Client) Changes() ascv1.ChangesClient     { return ascv1.NewChangesClient(c) }
func (c *Client) Projects() ascv1.ProjectsClient   { return ascv1.NewProjectsClient(c) }
func (c *Client) Cloud() ascv1.CloudClient         { return ascv1.NewCloudClient(c) }
func (c *Client) CodeIntel() ascv1.CodeIntelClient { return ascv1.NewCodeIntelClient(c) }

// CacheStats reports object cache traffic.
func (c *Client) CacheStats() CacheStats { return c.cache.stats() }

// readOnly lists the methods that need no x-asc-op-id: they change nothing, or (PutObjects) are
// idempotent by content address. Every other method, including one added later, is treated as
// mutating, so it carries an op id and is safe to retry.
var readOnly = map[string]bool{
	"/asc.v1.Repo/Version": true, "/asc.v1.Repo/HasObjects": true, "/asc.v1.Repo/GetObjects": true,
	"/asc.v1.Repo/PutObjects": true, "/asc.v1.Repo/ListRefs": true, "/asc.v1.Repo/Resolve": true,
	"/asc.v1.Repo/GitMap": true, "/asc.v1.Repo/Log": true, "/asc.v1.Repo/MergeBase": true,
	"/asc.v1.Repo/IsAncestor": true, "/asc.v1.Repo/Diff": true, "/asc.v1.Repo/Blame": true,
	"/asc.v1.Repo/LastCommits": true, "/asc.v1.Repo/ReadChangeset": true, "/asc.v1.Repo/ReadTree": true,
	"/asc.v1.Repo/ReadFile": true, "/asc.v1.Repo/Stat": true, "/asc.v1.Repo/CheckAttr": true,
	"/asc.v1.Repo/Grep": true, "/asc.v1.Repo/Archive": true, "/asc.v1.Repo/FindLanding": true,
	"/asc.v1.Repo/Classify": true, "/asc.v1.Repo/CommitGraph": true, "/asc.v1.Repo/LineHistory": true,
	"/asc.v1.Repo/Compare": true, "/asc.v1.Repo/RepoConfig": true,
	"/asc.v1.Changes/ResolveChanges": true, "/asc.v1.Changes/Stack": true, "/asc.v1.Changes/Versions": true,
	"/asc.v1.Changes/InterDiff": true, "/asc.v1.Changes/Mergeability": true, "/asc.v1.Changes/Overlaps": true,
	"/asc.v1.Cloud/GetWorkspace": true, "/asc.v1.Cloud/ListWorkspaces": true,
	"/asc.v1.Projects/ProjectGraph": true, "/asc.v1.Projects/Affected": true, "/asc.v1.Projects/ProjectHashes": true,
	"/asc.v1.CodeIntel/Search": true, "/asc.v1.CodeIntel/Symbols": true, "/asc.v1.CodeIntel/Semantic": true,
	"/asc.v1.CodeIntel/IndexStatus": true,
}

// prepare authenticates the call and adds the common metadata. It reports whether the method is
// mutating. A caller's own authorization header is replaced, never forwarded.
func (c *Client) prepare(ctx context.Context, method string) (context.Context, bool, error) {
	if c.closed.Load() {
		return ctx, false, ErrClosed
	}
	token, err := c.bearer(ctx)
	if err != nil {
		return ctx, false, err
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("authorization", "Bearer "+token)
	md.Set("x-asc-client", userAgent())
	mutating := !readOnly[method]
	if mutating {
		id, ok := ctx.Value(opIDKey{}).(uuid.UUID)
		if !ok {
			if id, err = uuid.NewV7(); err != nil {
				return ctx, false, fmt.Errorf("ascclient: op id: %w", err)
			}
		}
		md.Set("x-asc-op-id", id.String())
	}
	return metadata.NewOutgoingContext(ctx, md), mutating, nil
}

func (c *Client) unaryInterceptor(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, _, err := c.prepare(ctx, method)
	if err != nil {
		return err
	}
	return c.retry.do(ctx, true, func(ctx context.Context) (time.Duration, error) {
		actx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
		defer cancel()
		var trailer metadata.MD
		err := invoker(actx, method, req, reply, cc, append(slices.Clip(opts), grpc.Trailer(&trailer))...)
		return retryHint(trailer), err
	})
}

// streamInterceptor authenticates streams but does not retry them: a retry must restart the
// whole exchange, so the helpers that know what is safe to replay do that (see GetObjects).
func (c *Client) streamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, _, err := c.prepare(ctx, method)
	if err != nil {
		return nil, err
	}
	return streamer(ctx, desc, cc, method, opts...)
}

// CheckServer calls Version and fails unless the server offers every wanted feature (the
// Forgejo fork refuses to enable [asc] otherwise). It returns the server's answer.
func (c *Client) CheckServer(ctx context.Context, wantFeatures ...string) (*ascv1.VersionResponse, error) {
	resp, err := c.Repo().Version(ctx, &ascv1.VersionRequest{})
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, f := range wantFeatures {
		if !slices.Contains(resp.GetFeatures(), f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return resp, fmt.Errorf("ascclient: asc serve %s lacks required features %v", resp.GetServerVersion(), missing)
	}
	return resp, nil
}
