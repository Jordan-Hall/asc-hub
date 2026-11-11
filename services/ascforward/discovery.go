// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascforward

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
)

// Named limits of the discovery probe.
const (
	// DiscoveryTTL is how long an answer of asc serve's discovery document is kept.
	DiscoveryTTL = 5 * time.Minute
	// DiscoveryRetry is how long a failed read is kept before the hub asks again.
	DiscoveryRetry = 30 * time.Second
	// DiscoveryTimeout bounds one read.
	DiscoveryTimeout = 3 * time.Second
	// MaxDiscoveryBytes bounds the document.
	MaxDiscoveryBytes = 64 << 10
)

// Discovery reads the discovery document of asc serve (GET /.well-known/asc) to learn what it offers beside its gRPC
// API. The hub announces itself as the asc server, so its own document says what asc serve says: today whether asc
// publishes seed snapshots, which it does only when its object store can presign downloads. The answer is kept for
// DiscoveryTTL; a failure counts as "no" for DiscoveryRetry, so a clone is never held up by the probe.
type Discovery struct {
	client *http.Client
	url    string

	mu    sync.Mutex
	until time.Time
	seeds bool
}

// Upstream is the discovery of the asc serve the hub forwards to, set when [asc] ENABLED is on and nil otherwise.
var Upstream *Discovery

// NewDiscovery returns the discovery of the asc serve at upstream (see New); tr nil makes the HTTP/2 transport.
func NewDiscovery(upstream string, tr http.RoundTripper) (*Discovery, error) {
	u, err := url.Parse(upstream)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("ascforward: the asc server address must be an http or https address without credentials, query or fragment")
	}
	if tr == nil {
		tr = newTransport(u)
	}
	return &Discovery{
		client: &http.Client{
			Transport: tr,
			Timeout:   DiscoveryTimeout,
			// The document is asc serve's own; a redirect elsewhere is not followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		url: strings.TrimRight(u.String(), "/") + "/.well-known/asc",
	}, nil
}

// Seeds reports whether asc serve publishes seed snapshots under /seeds.
func (d *Discovery) Seeds(ctx context.Context) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if now.Before(d.until) {
		return d.seeds
	}
	seeds, err := d.read(ctx)
	if err != nil {
		log.Warn("asc discovery: %v", err)
		d.seeds, d.until = false, now.Add(DiscoveryRetry)
		return false
	}
	d.seeds, d.until = seeds, now.Add(DiscoveryTTL)
	return seeds
}

func (d *Discovery) read(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return false, errors.New("the asc server cannot be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, errors.New("the asc server answered " + resp.Status)
	}
	var doc struct {
		Seeds string `json:"seeds"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxDiscoveryBytes+1))
	if err != nil {
		return false, err
	}
	if len(body) > MaxDiscoveryBytes {
		return false, errors.New("the discovery document is too large")
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return false, errors.New("the discovery document is not JSON")
	}
	return doc.Seeds != "", nil
}
