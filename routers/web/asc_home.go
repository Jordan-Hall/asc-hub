// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// ascHomeRepoCount is how many public repositories the landing page lists.
const ascHomeRepoCount = 6

// prepareHomeRepos puts the most recently updated public repositories on the landing page of a visitor who
// is not signed in. It lists only repositories anyone may see, and nothing when the site has none or the
// operator closed the repository list: the landing page never shows made-up content.
func prepareHomeRepos(ctx *context.Context) {
	if setting.Service.RequireSignInView || setting.Service.Explore.RequireSigninView {
		return
	}
	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: ascHomeRepoCount},
		OrderBy:     db.SearchOrderByRecentUpdated,
		AllPublic:   true,
		Private:     false,
	})
	if err != nil {
		// The list is an extra: the page still renders without it.
		log.Error("home: listing public repositories: %v", err)
		return
	}
	if err := repo_model.RepositoryList(repos).LoadAttributes(ctx); err != nil {
		log.Error("home: loading the attributes of public repositories: %v", err)
		return
	}
	ctx.Data["AscHomeRepos"] = repos
}
