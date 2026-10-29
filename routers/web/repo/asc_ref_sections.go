// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The sections the Refs page adds to the branch list (board Refs): the stacks in progress, each with its progress rail,
// and the latest tags with the release each one belongs to. The stacks come from the pull request unit, so they are read
// only for a viewer who may read pull requests; the tags need the code unit, which the route has already checked.

// refStackRows is how many stacks the Refs page lists; "All stacks" opens the full list.
const refStackRows = 12

// refTagRows is how many tags the Refs page lists; the Tags page has the rest.
const refTagRows = 10

// refTag is one tag of the Tags section with its release when it has one.
type refTag struct {
	Name    string
	Title   string
	Release *repo_model.Release
}

// prepareRefSections adds the stacks and the tags to the data of the branch list of an asc repository.
func prepareRefSections(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if repo.GetStorageBackend() != "asc" {
		return
	}
	if repo.CanEnablePulls() && ctx.Repo.CanRead(unit.TypePullRequests) {
		changes, landed, err := loadStackChanges(ctx, repo, ctx.Repo.GitRepo, ctx.Doer)
		if err != nil {
			log.Error("refs: stacks of %s: %v", repo.FullName(), err)
			ctx.Data["RefStacksFailed"] = true
		} else {
			rows := buildStackRows(changes, landed, repo.Link(), ctx.Doer)
			ctx.Data["RefStackTotal"] = len(rows)
			if len(rows) > refStackRows {
				rows = rows[:refStackRows]
			}
			ctx.Data["RefStacks"] = rows
		}
	}
	if !ctx.Repo.CanRead(unit.TypeCode) {
		return
	}
	rels, err := db.Find[repo_model.Release](ctx, repo_model.FindReleasesOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: refTagRows},
		RepoID:      repo.ID, IncludeTags: true,
	})
	if err != nil {
		log.Error("refs: tags of %s: %v", repo.FullName(), err)
		return
	}
	if len(rels) == 0 {
		return
	}
	var tags []*refTag
	for _, r := range rels {
		r.Repo = repo
		if err := r.LoadAttributes(ctx); err != nil {
			log.Error("refs: publisher of %s: %v", r.TagName, err)
		}
		t := &refTag{Name: r.TagName, Release: r}
		if !r.IsTag {
			t.Title = r.Title
		}
		tags = append(tags, t)
	}
	ctx.Data["RefTags"] = tags
}
