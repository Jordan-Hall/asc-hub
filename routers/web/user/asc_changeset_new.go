// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"

	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

// "New changeset" of the Create menu (board Shell): the asc repositories the person can write, most recently
// updated first; each opens the editor on its default branch, where saving makes the changeset.

const tplAscChangesetNew base.TplName = "user/asc_changeset_new"

// AscChangesetRepo is one repository a changeset can be started in.
type AscChangesetRepo struct {
	Repo   *repo_model.Repository
	NewURL string
}

// AscChangesetNew serves `GET /-/asc/changeset/new`.
func AscChangesetNew(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscChangesetNew", nil)
		return
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_shell.create_changeset")
	repos, err := ascAgentsHomeRepos(ctx)
	if err != nil {
		ctx.ServerError("SearchRepository", err)
		return
	}
	rows := make([]AscChangesetRepo, 0, len(repos))
	for _, repo := range repos {
		if !repo.CanEnableEditor() {
			continue
		}
		perm, err := access_model.GetUserRepoPermission(ctx, repo, ctx.Doer)
		if err != nil {
			ctx.ServerError("GetUserRepoPermission", err)
			return
		}
		if !perm.CanWrite(unit_model.TypeCode) {
			continue
		}
		rows = append(rows, AscChangesetRepo{
			Repo:   repo,
			NewURL: repo.Link() + "/_new/" + util.PathEscapeSegments(repo.DefaultBranch) + "/",
		})
	}
	ctx.Data["AscChangesetRepos"] = rows
	ctx.HTML(http.StatusOK, tplAscChangesetNew)
}
