// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	repo_service "forgejo.org/services/repository"
)

// The asc parts of the new-repository page: the name check as you type, the quota line, the line about the signing key and
// the data of the extra files (AGENTS.md, the policy file). Every part is advice around the form; the create handler decides
// again, and a part that cannot be read is left out.

// AscNewRepoSigning is the signing key line: the first live key of the person, or none.
type AscNewRepoSigning struct {
	HasKey bool
	ID     string
	Title  string
}

// AscNewRepoQuota is "N of M repositories used" for the owner chosen on the page.
type AscNewRepoQuota struct {
	Used, Limit int
	Reached     bool
}

// prepareAscNewRepo puts the quota line and the signing key line into ctx.Data. owner is the account the repository will
// be created for.
func prepareAscNewRepo(ctx *context.Context, owner *user_model.User) {
	if limit := owner.MaxCreationLimit(); limit > 0 {
		ctx.Data["AscNewRepoQuota"] = &AscNewRepoQuota{Used: owner.NumRepos, Limit: limit, Reached: owner.NumRepos >= limit}
	}
	if !newRepositoriesInAsc() || ctx.Doer == nil || !asctrust.HasFeature(ctx, asctrust.FeatureSigning) {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallAccount, nil)
	if err != nil {
		return
	}
	defer release()
	page, err := asctrust.LoadKeys(callCtx, conn, time.Now())
	if err != nil {
		log.Warn("asc new repository: signing keys: %v", err)
		return
	}
	line := &AscNewRepoSigning{}
	for _, k := range page.Keys {
		if k.State == "live" {
			line.HasKey, line.ID, line.Title = true, k.ID, k.Title
			break
		}
	}
	ctx.Data["AscNewRepoSigning"] = line
}

// ascNewRepoNameAnswer is the answer of CheckName.
type ascNewRepoNameAnswer struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// CheckName serves `GET /repo/check_name?uid=&name=`: whether the name is free for the chosen owner. It is advice for the
// page (the form says the same after submit), so it answers only about names, never about what a repository holds.
func CheckName(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	name := ctx.FormTrim("name")
	if name == "" || len(name) > 100 {
		ctx.JSON(http.StatusOK, ascNewRepoNameAnswer{})
		return
	}
	owner := checkContextUser(ctx, ctx.FormInt64("uid"))
	if ctx.Written() {
		return
	}
	if err := repo_model.IsUsableRepoName(name); err != nil {
		msg := ctx.Locale.TrString("asc_rp.newrepo_name_chars")
		switch {
		case db.IsErrNameReserved(err):
			msg = ctx.Locale.TrString("repo.form.name_reserved", name)
		case db.IsErrNamePatternNotAllowed(err):
			msg = ctx.Locale.TrString("repo.form.name_pattern_not_allowed", name)
		}
		ctx.JSON(http.StatusOK, ascNewRepoNameAnswer{Message: msg})
		return
	}
	exists, err := repo_model.IsRepositoryModelExist(gocontext.WithoutCancel(ctx), owner, name)
	if err != nil {
		ctx.ServerError("IsRepositoryModelExist", err)
		return
	}
	if exists {
		ctx.JSON(http.StatusOK, ascNewRepoNameAnswer{Message: ctx.Locale.TrString("asc_rp.newrepo_name_taken", owner.Name+"/"+name)})
		return
	}
	ctx.JSON(http.StatusOK, ascNewRepoNameAnswer{OK: true})
}

// ascWorkingMode keeps the working mode of the page to the two it offers, and only where asc keeps new repositories.
func ascWorkingMode(v string) string {
	if !newRepositoriesInAsc() || (v != repo_service.WorkingModeBranchless && v != repo_service.WorkingModeBranches) {
		return ""
	}
	return v
}

// ascPolicyApprovals keeps the approvals of the page within what the policy file offers.
func ascPolicyApprovals(n int) int {
	return max(0, min(n, repo_service.PolicyMaxApprovals))
}
