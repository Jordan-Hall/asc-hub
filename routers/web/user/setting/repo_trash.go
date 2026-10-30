// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"time"

	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// The trash of deleted repositories (models/repo/repo_trash.go): /user/settings/repo-trash lists what the signed-in
// person may restore and restores it. A person may restore a repository of their own, of an organisation they own,
// and, as a site administrator, any. Anyone else gets a 404 for a repository that is not theirs, whether or not it exists.
const tplRepoTrash base.TplName = "user/settings/repo_trash"

type trashRow struct {
	Trash *repo_model.RepoTrash
	Repo  *repo_model.Repository
	Days  int
}

// canRestoreRepo says whether the person may restore the repository.
func canRestoreRepo(ctx *context.Context, doer *user_model.User, repo *repo_model.Repository) bool {
	if doer.IsAdmin || repo.OwnerID == doer.ID {
		return true
	}
	if err := repo.LoadOwner(ctx); err != nil || !repo.Owner.IsOrganization() {
		return false
	}
	org := organization.OrgFromUser(repo.Owner)
	ok, err := org.IsOwnedBy(ctx, doer.ID)
	return err == nil && ok
}

// RepoTrash serves GET /user/settings/repo-trash.
func RepoTrash(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.repo_trash")
	ctx.Data["PageIsSettingsRepos"] = true
	ctx.Data["PageIsSettingsRepoTrash"] = true
	owners := []int64{ctx.Doer.ID}
	if ctx.Doer.IsAdmin {
		owners = nil
	} else if orgs, err := organization.GetOrgsCanCreateRepoByUserID(ctx, ctx.Doer.ID); err == nil {
		for _, o := range orgs {
			if ok, err := o.IsOwnedBy(ctx, ctx.Doer.ID); err == nil && ok {
				owners = append(owners, o.ID)
			}
		}
	}
	var records []*repo_model.RepoTrash
	var err error
	if ctx.Doer.IsAdmin {
		records, err = repo_model.FindTrashDue(ctx, time.Now().AddDate(0, 0, repo_model.TrashDays+1), 500)
	} else {
		records, err = repo_model.FindTrashOfOwners(ctx, owners)
	}
	if err != nil {
		ctx.ServerError("FindTrash", err)
		return
	}
	rows := make([]*trashRow, 0, len(records))
	for _, t := range records {
		repo, err := repo_model.GetRepositoryByID(ctx, t.RepoID)
		if err != nil {
			continue
		}
		left := int(time.Until(t.PurgeUnix.AsTime()).Hours()/24) + 1
		rows = append(rows, &trashRow{Trash: t, Repo: repo, Days: max(left, 0)})
	}
	ctx.Data["TrashRows"] = rows
	ctx.Data["TrashDays"] = repo_model.TrashDays
	ctx.HTML(http.StatusOK, tplRepoTrash)
}

// RepoTrashRestore serves POST /user/settings/repo-trash/{id}/restore.
func RepoTrashRestore(ctx *context.Context) {
	id := ctx.ParamsInt64(":id")
	repo, err := repo_model.GetRepositoryByID(ctx, id)
	if err != nil || !repo.IsTrashed() || !canRestoreRepo(ctx, ctx.Doer, repo) {
		ctx.NotFound("RepoTrashRestore", nil)
		return
	}
	if err := repo_model.RestoreRepository(ctx, repo.ID); err != nil {
		log.Error("restore of repository %d: %v", repo.ID, err)
		ctx.ServerError("RestoreRepository", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("settings.repo_trash_restored", repo.FullName()))
	ctx.Redirect(setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name)
}
