// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package feed

import (
	"fmt"
	"html/template"
	"sort"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/markup"
	"forgejo.org/modules/markup/markdown"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascreleases"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/gorilla/feeds"
)

// ascReleasesToFeedItems makes the feed items of the releases that exist only in asc (made with `asc release create`): the ones
// with no release row of the hub, published, newest first. An item links to the tag in the code view, because the hub has no
// release page for it; the author is the person asc names.
func ascReleasesToFeedItems(ctx *context.Context, repo *repo_model.Repository, records map[string]*asctrust.ReleaseRecord) ([]*feeds.Item, error) {
	only := ascreleases.Only(ctx, repo.ID, records, false)
	if len(only) == 0 {
		return nil, nil
	}
	metas := repo.ComposeMetas(ctx)
	items := make([]*feeds.Item, 0, len(only))
	for _, rec := range only {
		var content template.HTML
		content, err := markdown.RenderString(&markup.RenderContext{
			Ctx: ctx, Links: markup.Links{Base: repo.Link()}, Metas: metas,
		}, rec.Notes)
		if err != nil {
			return nil, err
		}
		title := rec.Title
		if title == "" {
			title = rec.Tag
		}
		link := &feeds.Link{Href: repo.HTMLURL() + "/src/tag/" + util.PathEscapeSegments(rec.Tag)}
		item := &feeds.Item{
			Title:   title,
			Link:    link,
			Created: time.Unix(rec.Time(), 0),
			Id:      fmt.Sprintf("asc:%s: %s", rec.Tag, link.Href),
			Content: string(content),
		}
		if rec.CreatedBy != "" {
			item.Author = &feeds.Author{Name: rec.CreatedBy}
		}
		items = append(items, item)
	}
	return items, nil
}

// mergeFeedItems puts the items of asc among the hub's, newest first.
func mergeFeedItems(hub, extra []*feeds.Item) []*feeds.Item {
	if len(extra) == 0 {
		return hub
	}
	out := append(append([]*feeds.Item(nil), hub...), extra...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}
