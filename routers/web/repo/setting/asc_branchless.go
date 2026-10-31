// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"errors"
	"net/http"

	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/context"
)

// The "Branchless" page of a repository (/{owner}/{repo}/settings/asc/branchless, SPEC section 21.5.2). asc
// owns the settings (branchless, cloud, shared_refs) and is the only place that changes them, so the page
// shows what asc reports and says how an administrator of the asc server changes it. It has no form: there
// is nothing to post, and so nothing that could write a setting that asc did not accept.
const tplSettingsAscBranchless base.TplName = "repo/settings/asc_branchless"

// ascBranchlessAllowed reports whether the page applies: [asc] is enabled, the repository is stored in asc,
// and the user administers it. It writes the refusal itself and fails closed.
func ascBranchlessAllowed(ctx *context.Context) bool {
	if !setting.Asc.Enabled || !ctx.IsSigned || ctx.Repo.Repository == nil {
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

// AscBranchlessPost serves `POST /{owner}/{repo}/settings/asc/branchless` with the field mode (branchless or branches): a
// repository administrator switches the working mode. asc decides whether it can be done without stranding a ref and says
// why when it cannot; its text is shown as it is.
func AscBranchlessPost(ctx *context.Context) {
	if !ascBranchlessAllowed(ctx) {
		return
	}
	back := ctx.Repo.RepoLink + "/settings/asc/branchless"
	var on bool
	switch ctx.FormString("mode") {
	case "branchless":
		on = true
	case "branches":
	default:
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_branchless.bad_mode"))
		ctx.Redirect(back)
		return
	}
	switch err := ascsync.SetBranchless(ctx, ctx.Repo.Repository, on); {
	case err == nil:
		ctx.Flash.Success(ctx.Tr("repo.settings.asc_branchless.changed"))
	case errors.Is(err, ascsync.ErrBranchlessUnsupported):
		ctx.Flash.Error(ctx.Tr("repo.settings.asc_branchless.unsupported"))
	default:
		var rej *ascsync.RejectError
		if errors.As(err, &rej) {
			ctx.Flash.Error(rej.Message) // asc wrote it for the person: which branch or user ref is in the way
		} else {
			log.Error("asc branchless: %s: %v", ctx.Repo.Repository.FullName(), err)
			ctx.Flash.Error(ctx.Tr("repo.settings.asc_branchless.failed"))
		}
	}
	ctx.Redirect(back)
}

// AscBranchless serves `GET /{owner}/{repo}/settings/asc/branchless`.
func AscBranchless(ctx *context.Context) {
	if !ascBranchlessAllowed(ctx) {
		return
	}
	ctx.Data["Title"] = ctx.Tr("repo.settings.asc_branchless")
	ctx.Data["PageIsSettingsAscBranchless"] = true
	ctx.Data["AscRepoName"] = ctx.Repo.Repository.FullName()

	s, err := ascsync.LoadBranchlessSettings(ctx, ctx.Repo.Repository)
	if err != nil {
		// Only the fact that it failed is shown; the cause goes to the log.
		log.Error("asc branchless settings of %s: %v", ctx.Repo.Repository.FullName(), err)
		ctx.Data["AscBranchlessError"] = true
		ctx.HTML(http.StatusBadGateway, tplSettingsAscBranchless)
		return
	}
	ctx.Data["AscBranchless"] = s.Branchless
	ctx.Data["AscCloud"] = s.Cloud
	ctx.Data["AscSharedRefs"] = s.SharedRefs
	ctx.HTML(http.StatusOK, tplSettingsAscBranchless)
}
