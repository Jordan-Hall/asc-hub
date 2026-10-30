// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascreleases merges the releases that exist only in asc (made with `asc release create`) into the lists of the hub's
// own releases: the Releases page, its Atom feed and the API list. The two are matched by tag and the hub row wins. A release
// published on the hub is mirrored to asc, so it is on both sides; one made with asc exists only there until it is matched.
//
// The hub pages its own rows in the database, newest first. An asc-only release is placed on the page of the last hub row that is
// as new or newer, so no release is lost or shown twice, and a page can hold a few more than its size. The pager counts the
// hub's rows alone, or the asc-only releases when the hub has none (they are then paged among themselves).
package ascreleases

import (
	"context"
	"sort"
	"strings"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/asctrust"
)

// Only returns the records of asc whose tag has no release row of the hub, newest first. A draft is left out unless
// includeDrafts. A bare tag of the hub (is_tag) is not a release, so an asc release of that tag is returned.
func Only(ctx context.Context, repoID int64, records map[string]*asctrust.ReleaseRecord, includeDrafts bool) []*asctrust.ReleaseRecord {
	if len(records) == 0 {
		return nil
	}
	var hubTags []string
	if err := db.GetEngine(ctx).Table("release").Where("repo_id = ? AND is_tag = ?", repoID, false).Cols("tag_name").Find(&hubTags); err != nil {
		log.Error("asc releases: the tags of the hub's releases: %v", err)
		return nil
	}
	have := make(map[string]bool, len(hubTags))
	for _, t := range hubTags {
		have[t] = true
	}
	var out []*asctrust.ReleaseRecord
	for tag, rec := range records {
		if have[tag] || (rec.Draft && !includeDrafts) {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Time() != out[j].Time() {
			return out[i].Time() > out[j].Time()
		}
		return out[i].Tag > out[j].Tag
	})
	return out
}

// Filter says which releases a list shows: the draft and pre-release switches (unset means either) and a keyword matched in
// the tag, the title and the notes, in any case.
type Filter struct {
	IsDraft      optional.Option[bool]
	IsPreRelease optional.Option[bool]
	Keyword      string
}

// Matches reports whether the record passes the filter.
func (f Filter) Matches(rec *asctrust.ReleaseRecord) bool {
	if f.IsDraft.Has() && f.IsDraft.ValueOrDefault(false) != rec.Draft {
		return false
	}
	if f.IsPreRelease.Has() && f.IsPreRelease.ValueOrDefault(false) != rec.Prerelease {
		return false
	}
	if f.Keyword == "" {
		return true
	}
	k := strings.ToLower(f.Keyword)
	return strings.Contains(strings.ToLower(rec.Tag), k) || strings.Contains(strings.ToLower(rec.Title), k) || strings.Contains(strings.ToLower(rec.Notes), k)
}

// Apply keeps the records that pass the filter.
func (f Filter) Apply(recs []*asctrust.ReleaseRecord) []*asctrust.ReleaseRecord {
	var out []*asctrust.ReleaseRecord
	for _, r := range recs {
		if f.Matches(r) {
			out = append(out, r)
		}
	}
	return out
}

// PageOf is the page, counting from 1, that holds a release of the given time when the hub's rows (newest first, times in
// hubTimes) are paged by size: the page of the last hub row that is as new or newer. A release older than every row of the
// hub is on the last page, and one newer than all of them on the first.
func PageOf(hubTimes []int64, t int64, size int) int {
	if size <= 0 || len(hubTimes) == 0 {
		return 1
	}
	newer := sort.Search(len(hubTimes), func(i int) bool { return hubTimes[i] < t }) // rows as new or newer
	pages := (len(hubTimes) + size - 1) / size
	page := (newer + size - 1) / size
	return min(max(page, 1), pages)
}

// ForPage returns the records of extra that ride on the given page of the hub's rows, in order, and the total the pager counts.
// opts are the find options of the hub's rows of the list (whatever filters it has); hubCount is how many there are.
func ForPage(ctx context.Context, opts *repo_model.FindReleasesOptions, extra []*asctrust.ReleaseRecord, hubCount int64, size, page int) ([]*asctrust.ReleaseRecord, int64) {
	if len(extra) == 0 {
		return nil, hubCount
	}
	var hubTimes []int64
	if hubCount > 0 {
		all := *opts
		all.ListOptions = db.ListOptions{ListAll: true}
		if err := db.GetEngine(ctx).Table("release").Where(all.ToConds()).Cols("created_unix").Desc("created_unix").Find(&hubTimes); err != nil {
			log.Error("asc releases: the times of the hub's releases: %v", err)
			return nil, hubCount
		}
	}
	if page < 1 {
		page = 1
	}
	var out []*asctrust.ReleaseRecord
	for i, rec := range extra {
		on := PageOf(hubTimes, rec.Time(), size)
		if hubCount == 0 && size > 0 {
			on = i/size + 1 // no row of the hub to ride on: the releases of asc are paged among themselves
		}
		if on == page {
			out = append(out, rec)
		}
	}
	total := hubCount
	if hubCount == 0 {
		total = int64(len(extra))
	}
	return out, total
}
