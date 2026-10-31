// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// ascAuditRecentRows is how many entries the dashboard card shows.
const ascAuditRecentRows = 5

// ascRecentAudit reads the newest entries of the audit log of the whole server for the dashboard card, as the signed-in
// site administrator (the same call as the audit page). It never fails the dashboard: without asc, without the audit
// service or on any asc problem the card is left out.
func ascRecentAudit(ctx *context.Context) []asctrust.AuditRow {
	if !setting.Asc.Enabled || ctx.Doer == nil || !ctx.Doer.IsAdmin || !asctrust.HasFeature(ctx, asctrust.FeatureAudit) {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		return nil
	}
	defer release()
	q, filter, _ := asctrust.AuditQueryOf(func(string) string { return "" })
	q.Limit = ascAuditRecentRows
	res, err := asctrust.ListAudit(callCtx, conn, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}, q, filter)
	if err != nil {
		log.Warn("admin dashboard: recent audit entries: %v", err)
		return nil
	}
	rows := res.Page.Rows
	if len(rows) > ascAuditRecentRows {
		rows = rows[:ascAuditRecentRows]
	}
	return rows
}
