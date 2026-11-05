// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"

	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

// MenuStatus is GET .../asc/menu-status: the statuses of the Storage and AI keys entries of the settings menu
// (board RepoSettingsMenu), as translated text. The menu draws without them and fills them in when this answers, so
// a slow or unreachable asc never holds the menu up. A part asc could not answer is left out.
//
//	{"storage": "Your bucket", "ai": "2 providers"}
func MenuStatus(ctx *context.Context, lvl *Level) {
	s, _, ok := begin(ctx, lvl, "PageIsSettingsAscMenuStatus")
	if !ok {
		return
	}
	out := map[string]string{}
	if s == nil {
		ctx.JSON(http.StatusOK, out)
		return
	}
	if ts, err := s.ListTargets(ctx, lvl.Scope); err == nil {
		out["storage"] = ctx.Locale.TrString("asc_shell.sn_status_hub_storage")
		for _, t := range svc.TargetViews(ts) {
			if t.IsEnabled {
				out["storage"] = ctx.Locale.TrString("asc_shell.sn_status_own_bucket")
				break
			}
		}
	}
	if ks, err := s.ListKeys(ctx, lvl.Scope); err == nil {
		if n := usableProviders(svc.KeyViews(ks)); n > 0 {
			out["ai"] = string(ctx.Locale.TrN(n, "asc_shell.sn_status_ai_one", "asc_shell.sn_status_ai_other", n))
		} else {
			out["ai"] = ctx.Locale.TrString("asc_shell.sn_status_ai_none")
		}
	}
	ctx.JSON(http.StatusOK, out)
}

// usableProviders counts the providers with a key that is in use or not tested yet.
func usableProviders(ks []svc.KeyView) int {
	seen := map[svc.Provider]bool{}
	for _, k := range ks {
		if k.StateName == "active" || k.StateName == "untested" {
			seen[k.Provider] = true
		}
	}
	return len(seen)
}
