// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"sync"
	"time"

	"forgejo.org/models/db"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// The footer of every page says whether the hub is well. The answer is "ok" when the database answers and, with [asc] enabled,
// asc serve answers a Version call, and "degraded" otherwise. The probe is made at most once in statusTTL for the whole hub,
// with a short timeout, so a page never waits for it more than once in that time and a broken asc cannot slow every page.

const (
	statusTTL     = 20 * time.Second
	statusTimeout = 2 * time.Second
)

var (
	statusMu    sync.Mutex
	statusAt    time.Time
	statusValue = "ok"
	// statusProbe is the probe; tests replace it.
	statusProbe = probeStatus
)

func probeStatus(ctx context.Context) string {
	if err := db.GetEngine(ctx).Ping(); err != nil {
		log.Warn("status: the database does not answer: %v", err)
		return "degraded"
	}
	if !setting.Asc.Enabled {
		return "ok"
	}
	conn, err := Conn()
	if err != nil {
		return "degraded"
	}
	cctx, cancel := SystemContext(ctx, statusTimeout)
	defer cancel()
	if _, err := ascv1.NewRepoClient(conn).Version(cctx, &ascv1.VersionRequest{}); err != nil {
		return "degraded"
	}
	return "ok"
}

// FooterStatus is "ok" or "degraded", kept for statusTTL.
func FooterStatus() string {
	statusMu.Lock()
	defer statusMu.Unlock()
	if time.Since(statusAt) < statusTTL {
		return statusValue
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*statusTimeout)
	defer cancel()
	statusValue, statusAt = statusProbe(ctx), time.Now()
	return statusValue
}
