// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package feed

import (
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/gorilla/feeds"
)

// shows tags and/or releases on the repo as RSS / Atom feed
func ShowReleaseFeed(ctx *context.Context, repo *repo_model.Repository, isReleasesOnly bool, formatType string) {
	releases, err := db.Find[repo_model.Release](ctx, repo_model.FindReleasesOptions{
		IncludeTags: !isReleasesOnly,
		RepoID:      ctx.Repo.Repository.ID,
	})
	if err != nil {
		ctx.ServerError("GetReleasesByRepoID", err)
		return
	}

	var title string
	var link *feeds.Link

	if isReleasesOnly {
		title = ctx.Locale.TrString("repo.release.releases_for", repo.FullName())
		link = &feeds.Link{Href: repo.HTMLURL() + "/releases"}
	} else {
		title = ctx.Locale.TrString("repo.release.tags_for", repo.FullName())
		link = &feeds.Link{Href: repo.HTMLURL() + "/tags"}
	}

	feed := &feeds.Feed{
		Title:       title,
		Link:        link,
		Description: repo.Description,
		Created:     time.Now(),
	}

	feed.Items, err = releasesToFeedItems(ctx, releases)
	if err != nil {
		ctx.ServerError("releasesToFeedItems", err)
		return
	}
	if isReleasesOnly {
		// The releases made with `asc release create` are in the feed with the hub's own (never drafts: a feed is public).
		extra, err := ascReleasesToFeedItems(ctx, repo, asctrust.ListReleaseRecords(ctx, repo))
		if err != nil {
			ctx.ServerError("ascReleasesToFeedItems", err)
			return
		}
		feed.Items = mergeFeedItems(feed.Items, extra)
	}

	writeFeed(ctx, feed, formatType)
}
