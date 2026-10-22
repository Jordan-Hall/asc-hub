// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// Defaults and hard limits. The wire limits come from SPEC section 7: a message is at most
// 17 MiB, GetObjects takes at most 256 keys per message, HasObjects at most 4096.
const (
	DefaultPoolSize        = 4
	MaxPoolSize            = 64
	DefaultCacheBytes      = 256 << 20
	DefaultMaxDecodedBytes = 64 << 20
	DefaultCallTimeout     = 30 * time.Second
	DefaultStreamTimeout   = 2 * time.Minute
	DefaultPutBatchBytes   = 8 << 20

	MaxMessageBytes = 17 << 20
	MaxGetKeys      = 256
	MaxHasKeys      = 4096
	MaxPutObjects   = 4096
	// MaxKeysPerCall bounds the work of one GetObjects or HasObjects helper call.
	MaxKeysPerCall = 1 << 16
	// MaxTokenBytes bounds a bearer token.
	MaxTokenBytes = 4096

	// ClientName is sent as `x-asc-client: asc-forgejo/<version> <os>-<arch>`. The name differs from
	// `asc`, so the server does not apply its min_client check to it. Use Client.CheckServer.
	ClientName    = "asc-forgejo"
	ClientVersion = "1.0.0"
)

// RetryConfig sets the retry policy (SPEC section 16.3). Zero values take the SPEC defaults.
type RetryConfig struct {
	// MaxAttempts counts the first attempt (default 5).
	MaxAttempts int
	// Base is the first backoff ceiling (default 200 ms).
	Base time.Duration
	// Cap bounds both the backoff and the server's retry-after-ms hint (default 10 s).
	Cap time.Duration
}

// Config configures a Client.
type Config struct {
	// Target is the asc serve address: host:port, http://host:port or https://host:port.
	Target string
	// TLS configures TLS. Nil means the system roots with TLS 1.2 or later for https targets.
	TLS *tls.Config
	// AllowInsecure permits plaintext to a non-loopback host. Bearer tokens then cross the
	// network in the clear, so leave it off outside a private test network.
	AllowInsecure bool
	// InternalToken is the Forgejo internal token used by system calls (see WithSystem).
	InternalToken string
	// PoolSize is the number of HTTP/2 connections (default 4, at most 64).
	PoolSize int
	// CacheBytes is the object cache budget (default 256 MiB, negative disables the cache).
	CacheBytes int64
	// MaxDecodedBytes bounds one decoded object, so a hostile frame cannot expand without
	// limit (default 64 MiB).
	MaxDecodedBytes int64
	// CallTimeout bounds one attempt of a unary call (default 30 s).
	CallTimeout time.Duration
	// StreamTimeout bounds one attempt of a streaming helper (default 2 min).
	StreamTimeout time.Duration
	// PutBatchBytes is the payload of one PutObjects message (default 8 MiB, at most 16 MiB).
	PutBatchBytes int
	Retry         RetryConfig
}

type target struct {
	addr     string
	useTLS   bool
	loopback bool
}

func parseTarget(raw string) (target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return target{}, errors.New("ascclient: empty target")
	}
	t := target{addr: raw}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return target{}, fmt.Errorf("ascclient: bad target: %w", err)
		}
		if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
			return target{}, errors.New("ascclient: target must be scheme://host[:port]")
		}
		switch u.Scheme {
		case "https":
			t.useTLS = true
		case "http":
		default:
			return target{}, fmt.Errorf("ascclient: unsupported target scheme %q", u.Scheme)
		}
		t.addr = u.Host
	}
	host, _, err := net.SplitHostPort(t.addr)
	if err != nil {
		host = strings.Trim(t.addr, "[]")
		t.addr = net.JoinHostPort(host, "7890")
	}
	if host == "" {
		return target{}, errors.New("ascclient: target has no host")
	}
	t.loopback = host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		t.loopback = ip.IsLoopback()
	}
	return t, nil
}

func (c Config) withDefaults() (Config, error) {
	if c.PoolSize == 0 {
		c.PoolSize = DefaultPoolSize
	}
	if c.PoolSize < 0 || c.PoolSize > MaxPoolSize {
		return c, fmt.Errorf("ascclient: PoolSize must be between 1 and %d", MaxPoolSize)
	}
	if c.CacheBytes == 0 {
		c.CacheBytes = DefaultCacheBytes
	}
	if c.MaxDecodedBytes == 0 {
		c.MaxDecodedBytes = DefaultMaxDecodedBytes
	}
	if c.MaxDecodedBytes < 0 || c.MaxDecodedBytes > 1<<30 {
		return c, errors.New("ascclient: MaxDecodedBytes must be between 1 and 1 GiB")
	}
	if c.CallTimeout == 0 {
		c.CallTimeout = DefaultCallTimeout
	}
	if c.StreamTimeout == 0 {
		c.StreamTimeout = DefaultStreamTimeout
	}
	if c.CallTimeout < 0 || c.StreamTimeout < 0 {
		return c, errors.New("ascclient: timeouts must be positive")
	}
	if c.PutBatchBytes == 0 {
		c.PutBatchBytes = DefaultPutBatchBytes
	}
	if c.PutBatchBytes < 0 || c.PutBatchBytes > 16<<20 {
		return c, errors.New("ascclient: PutBatchBytes must be between 1 and 16 MiB")
	}
	if c.Retry.MaxAttempts == 0 {
		c.Retry.MaxAttempts = 5
	}
	if c.Retry.Base == 0 {
		c.Retry.Base = 200 * time.Millisecond
	}
	if c.Retry.Cap == 0 {
		c.Retry.Cap = 10 * time.Second
	}
	if c.Retry.MaxAttempts < 1 || c.Retry.MaxAttempts > 10 || c.Retry.Base < 0 || c.Retry.Cap < c.Retry.Base {
		return c, errors.New("ascclient: bad retry configuration")
	}
	if c.InternalToken != "" {
		if err := validateToken(c.InternalToken); err != nil {
			return c, fmt.Errorf("ascclient: internal token: %w", err)
		}
	}
	return c, nil
}

func userAgent() string {
	return fmt.Sprintf("%s/%s %s-%s", ClientName, ClientVersion, runtime.GOOS, runtime.GOARCH)
}
