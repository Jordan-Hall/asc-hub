// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"sort"
	"strings"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/markup"
	"forgejo.org/modules/markup/markdown"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascreleases"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The Releases page lists the releases of the hub and, merged in, the releases that exist only in asc (made with `asc release
// create`; a release published on the hub is mirrored to asc, so it is on both sides). The two are matched by tag and the hub
// row wins: its notes, its assets and its edit links are the ones shown, and the record of asc only adds the provenance and the
// checksums (asc_release_draft.go). A release that exists only in asc is shown read-only: its notes and assets as asc keeps
// them (an asset is a link to the https address asc was given), no edit or delete link, and its page is the tag in the code view.
//
// Order and pages. The hub's rows are paged by the database, newest first. An asc-only release is placed on the page that
// holds the hub row just before it in time, so no release is lost or shown twice, and a page can hold a few more than its
// size. The pager counts the hub's rows alone (the asc-only releases ride on those pages), or the asc-only ones when the hub
// has none.

// ascOnlyReleases returns the records of asc whose tag has no release row of the hub, that the person may see: a draft only for
// a person who can write. Newest first.
func ascOnlyReleases(ctx *context.Context, records map[string]*asctrust.ReleaseRecord, writeAccess bool) []*asctrust.ReleaseRecord {
	return ascreleases.Only(ctx, ctx.Repo.Repository.ID, records, writeAccess)
}

// ascChannelFilter is the filter of a channel of the page and its keyword.
func ascChannelFilter(channel, keyword string) ascreleases.Filter {
	f := ascreleases.Filter{Keyword: keyword}
	switch channel {
	case releaseChannelStable:
		f.IsPreRelease, f.IsDraft = optional.Some(false), optional.Some(false)
	case releaseChannelPre:
		f.IsPreRelease, f.IsDraft = optional.Some(true), optional.Some(false)
	case releaseChannelDraft:
		f.IsDraft = optional.Some(true)
	}
	return f
}

// ascFilterReleases keeps the releases of the channel that match the keyword.
func ascFilterReleases(recs []*asctrust.ReleaseRecord, channel, keyword string) []*asctrust.ReleaseRecord {
	return ascChannelFilter(channel, keyword).Apply(recs)
}

// ascAddChannelCounts adds the releases that exist only in asc to the pills of the channel filter.
func ascAddChannelCounts(counts []ReleaseChannelCount, extra []*asctrust.ReleaseRecord, keyword string) []ReleaseChannelCount {
	for i := range counts {
		counts[i].Count += int64(len(ascFilterReleases(extra, counts[i].Key, keyword)))
	}
	return counts
}

// ascReleaseInfo makes the entry of the list for a release that exists only in asc.
func ascReleaseInfo(ctx *context.Context, rec *asctrust.ReleaseRecord, n int, cache map[string]*user_model.User) *ReleaseInfo {
	repo := ctx.Repo.Repository
	rel := &repo_model.Release{
		// Negative, so that it is never the id of a row and never equal to another entry's.
		ID: -int64(n + 1), RepoID: repo.ID, Repo: repo, TagName: rec.Tag, LowerTagName: strings.ToLower(rec.Tag),
		Title: rec.Title, Note: rec.Notes, IsDraft: rec.Draft, IsPrerelease: rec.Prerelease,
		CreatedUnix: timeutil.TimeStamp(rec.Time()), ArchiveDownloadCount: &structs.TagArchiveDownloadCount{},
	}
	if rel.Title == "" {
		rel.Title = rec.Tag
	}
	if ctx.Repo.GitRepo != nil {
		if sha, err := ctx.Repo.GitRepo.GetTagCommitID(rec.Tag); err == nil {
			rel.Sha1 = sha
		}
	}
	rel.TargetBehind = repo.DefaultBranch
	u, ok := cache[rec.CreatedBy]
	if !ok {
		var err error
		if u, err = user_model.GetUserByName(ctx, rec.CreatedBy); err != nil {
			u = user_model.NewGhostUser()
		}
		cache[rec.CreatedBy] = u
	}
	rel.Publisher, rel.PublisherID = u, u.ID
	if note, err := markdown.RenderString(&markup.RenderContext{
		Links: markup.Links{Base: ctx.Repo.RepoLink}, Metas: repo.ComposeMetas(ctx), GitRepo: ctx.Repo.GitRepo, Ctx: ctx,
	}, rel.Note); err == nil {
		rel.RenderedNote = note
	}
	for i, a := range rec.AssetList {
		rel.Attachments = append(rel.Attachments, &repo_model.Attachment{
			ID: -int64(i + 1), Name: a.Name, Size: int64(a.Size), ExternalURL: a.URL, CreatedUnix: rel.CreatedUnix,
		})
	}
	// An asset with no https address is only listed with its checksum (the record), so the list keeps what can be downloaded.
	kept := rel.Attachments[:0]
	for _, a := range rel.Attachments {
		if a.ExternalURL != "" {
			kept = append(kept, a)
		}
	}
	rel.Attachments = kept
	return &ReleaseInfo{Release: rel, Asc: rec}
}

// mergeAscReleases puts the releases that exist only in asc into the page of the hub's rows (see the top of this file). opts
// are the find options of the page, size its size and page its number. It returns the entries of the page, newest first, and
// the total the pager counts.
func mergeAscReleases(ctx *context.Context, opts *repo_model.FindReleasesOptions, hub []*ReleaseInfo, hubCount int64, extra []*asctrust.ReleaseRecord, size, page int) ([]*ReleaseInfo, int64) {
	onPage, total := ascreleases.ForPage(ctx, opts, extra, hubCount, size, page)
	if len(onPage) == 0 {
		return hub, total
	}
	cache := map[string]*user_model.User{}
	out := append([]*ReleaseInfo(nil), hub...)
	for i, rec := range onPage {
		out = append(out, ascReleaseInfo(ctx, rec, i, cache))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Release.CreatedUnix > out[j].Release.CreatedUnix })
	return out, total
}
