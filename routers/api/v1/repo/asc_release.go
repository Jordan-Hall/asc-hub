// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"sort"
	"time"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascreleases"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/convert"
)

// mergeAscReleases adds the releases that exist only in asc (made with `asc release create`) to a page of the release list: the
// hub's rows win by tag (services/ascreleases). opts are the find options of the list, hubCount the number of the hub's rows.
// It returns the page, newest first, and the total for the headers. An entry of asc has the id 0 and links to the tag in the code
// view, because the hub has no release page for it.
func mergeAscReleases(ctx *context.APIContext, opts *repo_model.FindReleasesOptions, hub []*api.Release, hubCount int64, size, page int, records map[string]*asctrust.ReleaseRecord) ([]*api.Release, int64) {
	repo := ctx.Repo().Repository
	only := ascreleases.Only(ctx, repo.ID, records, opts.IncludeDrafts)
	filter := ascreleases.Filter{IsDraft: opts.IsDraft, IsPreRelease: opts.IsPreRelease, Keyword: opts.Keyword}
	onPage, total := ascreleases.ForPage(ctx, opts, filter.Apply(only), hubCount, size, page)
	if len(onPage) == 0 {
		return hub, total
	}
	out := append([]*api.Release(nil), hub...)
	cache := map[string]*user_model.User{}
	for _, rec := range onPage {
		out = append(out, ascReleaseToAPI(ctx, repo, rec, cache))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, total
}

// ascReleaseToAPI converts the record of a release that exists only in asc.
func ascReleaseToAPI(ctx *context.APIContext, repo *repo_model.Repository, rec *asctrust.ReleaseRecord, cache map[string]*user_model.User) *api.Release {
	created := time.Unix(rec.Time(), 0)
	published := created
	if rec.Created > 0 && rec.Published == 0 {
		published = time.Time{} // a draft is not published
	}
	title := rec.Title
	if title == "" {
		title = rec.Tag
	}
	u, ok := cache[rec.CreatedBy]
	if !ok {
		var err error
		if u, err = user_model.GetUserByName(ctx, rec.CreatedBy); err != nil {
			u = user_model.NewGhostUser()
		}
		cache[rec.CreatedBy] = u
	}
	var atts []*repo_model.Attachment
	for _, a := range rec.AssetList {
		if a.URL == "" {
			continue // an asset with no https address cannot be downloaded from the hub's list
		}
		atts = append(atts, &repo_model.Attachment{Name: a.Name, Size: int64(a.Size), ExternalURL: a.URL})
	}
	tag := util.PathEscapeSegments(rec.Tag)
	return &api.Release{
		TagName: rec.Tag, Target: repo.DefaultBranch, Title: title, Note: rec.Notes,
		HTMLURL: repo.HTMLURL() + "/src/tag/" + tag,
		TarURL:  repo.HTMLURL() + "/archive/" + tag + ".tar.gz", ZipURL: repo.HTMLURL() + "/archive/" + tag + ".zip",
		IsDraft: rec.Draft, IsPrerelease: rec.Prerelease, CreatedAt: created, PublishedAt: published,
		Publisher: convert.ToUser(ctx, u, nil), Attachments: convert.ToAPIAttachments(repo, atts),
		ArchiveDownloadCount: &api.TagArchiveDownloadCount{},
	}
}
