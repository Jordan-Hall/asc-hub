// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// FeatureAuditAppend is the Version.features string of Audit.Append.
const FeatureAuditAppend = "audit.append.v1"

// The actions the hub appends to the audit log of asc. asc accepts only names that start with "host." and are in its catalogue.
const (
	AuditHostSettingUpdated     = "host.setting.updated"
	AuditHostMaintenanceUpdated = "host.maintenance.updated"
	AuditHostUserSuspended      = "host.user.suspended"
	AuditHostUserUnsuspended    = "host.user.unsuspended"
	AuditHostUser2FAReset       = "host.user.2fa_reset"
	AuditHostModerationActed    = "host.moderation.acted"
)

const auditAppendTimeout = 5 * time.Second

// HostAudit is a change of a setting of the hub that the audit log of asc should hold: who did it (the login the hub verified),
// what changed and where the request came from.
type HostAudit struct {
	Action  string
	Actor   string
	Target  string
	Details map[string]string
	// Request supplies the address and the user agent of the person; nil leaves them out.
	Request *http.Request
}

// auditAppender sends the request to asc. Tests replace it.
var auditAppender = func(ctx context.Context, req *ascv1.AppendAuditRequest) error {
	conn, err := Conn()
	if err != nil {
		return err
	}
	cctx, cancel := SystemContext(ctx, auditAppendTimeout)
	defer cancel()
	_, err = ascv1.NewAuditClient(conn).Append(cctx, req)
	return err
}

// UseAuditAppender replaces the sender until the returned function is called. It is for tests.
func UseAuditAppender(f func(context.Context, *ascv1.AppendAuditRequest) error) (restore func()) {
	prev := auditAppender
	auditAppender = f
	return func() { auditAppender = prev }
}

// RecordHostAudit appends the change to the audit log of asc, as the hub (the relay) for the person. It is made after the
// change; a log that cannot be written is reported in the hub's own log and never undoes or blocks the change. Without asc, or
// with an asc that does not offer Audit.Append, it does nothing.
func RecordHostAudit(ctx context.Context, ev HostAudit) {
	if !setting.Asc.Enabled || ev.Actor == "" {
		return
	}
	// Detached from the request: the person's change is made, and the record should not be lost to a closed connection.
	ctx = context.WithoutCancel(ctx)
	if !HasFeature(ctx, FeatureAuditAppend) {
		return
	}
	req := &ascv1.AppendAuditRequest{Action: ev.Action, Actor: ev.Actor, Target: ev.Target, Details: ev.Details}
	if r := ev.Request; r != nil {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			req.RemoteAddr = host
		} else {
			req.RemoteAddr = strings.TrimSpace(r.RemoteAddr)
		}
		req.UserAgent = r.UserAgent()
	}
	if err := auditAppender(ctx, req); err != nil {
		log.Error("asc audit: the change %s by %s could not be recorded: %v", ev.Action, ev.Actor, err)
	}
}
