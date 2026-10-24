// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"errors"
	"net/http"
	"strings"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/context"
)

// maxAscSharedRefsFormBytes bounds the shared_refs textarea before it is split.
const maxAscSharedRefsFormBytes = ascsync.MaxSharedRefs * (ascsync.MaxSharedRefBytes + 1)

// parseAscSharedRefs splits the textarea (one pattern per line, blank lines ignored).
func parseAscSharedRefs(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// ascBranchlessAllowed reports whether the page applies: [asc] is enabled, the repository is stored in asc,
// and the user administers it. It writes the refusal itself. The route is also behind the repository
// admin middleware; this is the second check, and it fails closed.
func ascBranchlessAllowed(ctx *context.Context) bool {
	if !setting.Asc.Enabled || ctx.Repo.Repository == nil {
		ctx.NotFound("AscBranchless", nil)
		return false
	}
	if ok, err := ascsync.BackedBy(ctx.Repo.Repository); err != nil || !ok {
		ctx.NotFound("AscBranchless", nil)
		return false
	}
	if !ctx.Repo.IsAdmin() {
		ctx.Error(http.StatusForbidden)
		return false
	}
	return true
}

// PrepareAscBranchless loads the branchless settings from asc for the repository settings template. It
// sets AscBranchlessShown only when the section applies; an asc failure shows an error in the section and
// leaves the rest of the page alone.
func PrepareAscBranchless(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Repo.Repository == nil {
		return
	}
	if ok, err := ascsync.BackedBy(ctx.Repo.Repository); err != nil || !ok {
		return
	}
	ctx.Data["AscBranchlessShown"] = true
	s, err := ascsync.LoadBranchlessSettings(ctx, ctx.Repo.Repository)
	if err != nil {
		log.Error("asc branchless settings of %s: %v", ctx.Repo.Repository.FullName(), err)
		ctx.Data["AscBranchlessError"] = true
		return
	}
	ctx.Data["AscBranchless"] = s.Branchless
	ctx.Data["AscCloud"] = s.Cloud
	ctx.Data["AscSharedRefs"] = strings.Join(s.SharedRefs, "\n")
}

// AscBranchlessPost stores the branchless, cloud and shared_refs settings in asc (SPEC section 21.5.2).
func AscBranchlessPost(ctx *context.Context) {
	if !ascBranchlessAllowed(ctx) {
		return
	}
	redirect := ctx.Repo.RepoLink + "/settings"
	raw := ctx.Req.PostFormValue("asc_shared_refs")
	if len(raw) > maxAscSharedRefsFormBytes {
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_branchless_invalid"))
		ctx.Redirect(redirect)
		return
	}
	s := ascsync.BranchlessSettings{
		Branchless: ctx.Req.PostFormValue("asc_branchless") == "on",
		Cloud:      ctx.Req.PostFormValue("asc_cloud") == "on",
		SharedRefs: parseAscSharedRefs(raw),
	}
	if err := ascsync.SaveBranchlessSettings(ctx, ctx.Repo.Repository, s); err != nil {
		switch {
		case errors.Is(err, ascsync.ErrBadRequest):
			ctx.Flash.Error(ctx.Tr("repo.settings.asc_branchless_invalid"))
		default:
			log.Error("asc branchless settings of %s: %v", ctx.Repo.Repository.FullName(), err)
			ctx.Flash.Error(ctx.Tr("repo.settings.asc_branchless_failed"))
		}
		ctx.Redirect(redirect)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.settings.update_settings_success"))
	ctx.Redirect(redirect)
}
