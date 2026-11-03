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

// The footer of every page and the status page (/-/status) say whether the hub is well. The hub is "ok" when the
// database answers and, with [asc] enabled, asc serve answers a Version call, and "degraded" otherwise. The probe is
// made at most once in statusTTL for the whole hub, with a short timeout, so a page never waits for it more than once
// in that time and a broken asc cannot slow every page.

const (
	statusTTL     = 20 * time.Second
	statusTimeout = 2 * time.Second
)

// StatusReport is the answer of one probe: which parts of the hub answered, and when it was made.
type StatusReport struct {
	Database   bool
	AscEnabled bool
	Asc        bool
	CheckedAt  time.Time
}

// Value is "ok" when every part that is in use answered, and "degraded" otherwise.
func (r StatusReport) Value() string {
	if r.Database && (!r.AscEnabled || r.Asc) {
		return "ok"
	}
	return "degraded"
}

var (
	statusMu     sync.Mutex
	statusReport = StatusReport{Database: true}
	// statusProbe is the probe; tests replace it.
	statusProbe = probeStatus
)

func probeStatus(ctx context.Context) StatusReport {
	r := StatusReport{AscEnabled: setting.Asc.Enabled, CheckedAt: time.Now()}
	if err := db.GetEngine(ctx).Ping(); err != nil {
		log.Warn("status: the database does not answer: %v", err)
	} else {
		r.Database = true
	}
	if !r.AscEnabled {
		return r
	}
	conn, err := Conn()
	if err != nil {
		log.Warn("status: asc serve cannot be reached: %v", err)
		return r
	}
	cctx, cancel := SystemContext(ctx, statusTimeout)
	defer cancel()
	if _, err := ascv1.NewRepoClient(conn).Version(cctx, &ascv1.VersionRequest{}); err != nil {
		log.Warn("status: asc serve does not answer: %v", err)
		return r
	}
	r.Asc = true
	return r
}

// SystemStatus is the latest report, made again when it is older than statusTTL.
func SystemStatus() StatusReport {
	statusMu.Lock()
	defer statusMu.Unlock()
	if time.Since(statusReport.CheckedAt) < statusTTL {
		return statusReport
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*statusTimeout)
	defer cancel()
	statusReport = statusProbe(ctx)
	if statusReport.CheckedAt.IsZero() {
		statusReport.CheckedAt = time.Now()
	}
	return statusReport
}

// FooterStatus is "ok" or "degraded", kept for statusTTL.
func FooterStatus() string {
	return SystemStatus().Value()
}
