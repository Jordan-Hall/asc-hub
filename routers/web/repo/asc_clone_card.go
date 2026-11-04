// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strconv"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
)

// The "Work on it locally" card of the repository home (board Repo): the commands from a clone to a first change,
// for a person who may write code here and has sent no change yet. "Hide" posts to AscCloneCardHide, which sets a
// cookie for the repository.

// ascCloneCardCookie is the cookie that hides the card of one repository.
func ascCloneCardCookie(repoID int64) string {
	return "asc_clone_hidden_" + strconv.FormatInt(repoID, 10)
}

func prepareHomeCloneCard(ctx *context.Context, repo *repo_model.Repository) {
	if !ctx.IsSigned || repo.IsEmpty || repo.IsArchived || repo.IsMirror || !ctx.Repo.CanWrite(unit.TypeCode) {
		return
	}
	if ctx.GetSiteCookie(ascCloneCardCookie(repo.ID)) != "" {
		return
	}
	n, err := issues_model.CountIssues(ctx, &issues_model.IssuesOptions{
		RepoIDs:  []int64{repo.ID},
		PosterID: ctx.Doer.ID,
		IsPull:   optional.Some(true),
	})
	if err != nil {
		log.Error("home: changes of user %d in %s: %v", ctx.Doer.ID, repo.FullName(), err)
		return
	}
	if n > 0 {
		return
	}
	ctx.Data["HomeCloneCard"] = true
}

// ascCloneCardHideAge is how long a hidden card stays hidden: a year.
const ascCloneCardHideAge = 365 * 24 * 60 * 60

// AscCloneCardHide hides the "Work on it locally" card of the repository in this browser and goes back to the home.
func AscCloneCardHide(ctx *context.Context) {
	ctx.SetSiteCookie(ascCloneCardCookie(ctx.Repo.Repository.ID), "1", ascCloneCardHideAge)
	ctx.Redirect(ctx.Repo.RepoLink)
}
