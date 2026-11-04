// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"slices"
	"strings"
	"time"

	"forgejo.org/modules/log"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// ActivityRow is one line of the Activity section of a key: when, who, what happened and how it ended.
type ActivityRow struct {
	Time  time.Time
	Actor string
	// System is set for an event asc wrote itself (the cap warning), which has no person behind it.
	System bool
	// LabelKey is the locale key of what happened; Action is shown when asc names an action the hub does not know.
	LabelKey, Action string
	// OutcomeKey is the locale key of the outcome and Chip the class of its chip.
	OutcomeKey, Chip string
	Details          []asctrust.KV
}

// activityLabelKey is the locale key of an ai.key.* action, "" for any other action.
func activityLabelKey(action string) string {
	if !slices.Contains(svc.KeyActivityActions, action) {
		return ""
	}
	return "ascbyo.ai.activity." + strings.ReplaceAll(strings.TrimPrefix(action, "ai.key."), ".", "_")
}

// activityRowsOf turns cleaned audit rows into lines of the Activity section.
func activityRowsOf(rows []asctrust.AuditRow) []ActivityRow {
	out := make([]ActivityRow, 0, len(rows))
	for _, r := range rows {
		a := ActivityRow{
			Time: r.Time, Actor: r.Actor, System: r.ActorKind == "system", LabelKey: activityLabelKey(r.Action), Action: r.Action,
			Details: r.Details,
		}
		switch r.Tone {
		case "ok":
			a.OutcomeKey, a.Chip = "ascbyo.ai.activity.outcome_ok", "ds-chip-ok"
		case "no":
			a.OutcomeKey, a.Chip = "ascbyo.ai.activity.outcome_denied", "ds-chip-err"
		case "wn":
			a.OutcomeKey, a.Chip = "ascbyo.ai.activity.outcome_failed", "ds-chip-warn"
		default:
			a.OutcomeKey = "ascbyo.ai.activity.outcome_unknown"
		}
		out = append(out, a)
	}
	return out
}

// loadActivity fills the Activity section of a key, best effort: a failure leaves the section saying so.
func loadActivity(ctx *context.Context, lvl *Level, s *svc.Service, p *Page, id string) {
	resp, err := s.KeyActivity(ctx, lvl.Scope, id)
	if err == nil {
		var page *asctrust.AuditPage
		if page, err = asctrust.AuditPageOf(resp, nil); err == nil {
			p.Activity = activityRowsOf(page.Rows)
			p.ActivityLoaded = true
			return
		}
	}
	log.Debug("asc byo: activity of a key in %s: %v", lvl.Scope, err)
}
