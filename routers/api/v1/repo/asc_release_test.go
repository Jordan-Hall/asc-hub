// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscReleasesInTheAPIList(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockAPIContext(t, "user2/repo1/releases")
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadUser(t, ctx, 2)

	records := map[string]*asctrust.ReleaseRecord{
		"v1.1":      {Tag: "v1.1", Title: "from asc", Published: 946684801},
		"asc-1.0":   {Tag: "asc-1.0", Title: "Made with asc", Notes: "notes", Published: 2_000_000_000, CreatedBy: "user2", AssetList: []asctrust.ReleaseAssetInfo{{Name: "a.tgz", Size: 12, SHA256: "ab", URL: "https://x.example/a.tgz"}, {Name: "b", Size: 1}}},
		"asc-rc":    {Tag: "asc-rc", Prerelease: true, Published: 1_500_000_000},
		"asc-draft": {Tag: "asc-draft", Draft: true, Created: 1_900_000_000},
	}
	opts := &repo_model.FindReleasesOptions{RepoID: 1, ListOptions: db.ListOptions{Page: 1, PageSize: 30}}
	hubRows, err := db.Find[repo_model.Release](ctx, opts)
	require.NoError(t, err)
	var hub []*api.Release
	for _, r := range hubRows {
		hub = append(hub, &api.Release{ID: r.ID, TagName: r.TagName, CreatedAt: r.CreatedUnix.AsTime()})
	}
	hubCount, err := db.Count[repo_model.Release](ctx, opts)
	require.NoError(t, err)

	got, total := mergeAscReleases(ctx, opts, hub, hubCount, 30, 1, records)
	assert.Equal(t, hubCount, total)
	require.Len(t, got, len(hub)+2, "the asc-only releases, no draft for a reader, the hub's v1.1 not repeated")
	assert.Equal(t, "asc-1.0", got[0].TagName, "newest first")
	assert.Equal(t, "Made with asc", got[0].Title)
	assert.Zero(t, got[0].ID)
	assert.Contains(t, got[0].HTMLURL, "/user2/repo1/src/tag/asc-1.0")
	assert.Contains(t, got[0].TarURL, "/archive/asc-1.0.tar.gz")
	assert.Equal(t, time.Unix(2_000_000_000, 0).UTC(), got[0].CreatedAt.UTC())
	require.NotNil(t, got[0].Publisher)
	assert.Equal(t, "user2", got[0].Publisher.UserName)
	require.Len(t, got[0].Attachments, 1, "an asset with no https address is left out")
	assert.Equal(t, "https://x.example/a.tgz", got[0].Attachments[0].DownloadURL)
	assert.Equal(t, "external", got[0].Attachments[0].Type)
	tags := map[string]int{}
	for _, r := range got {
		tags[r.TagName]++
	}
	assert.Equal(t, 1, tags["v1.1"], "the hub's row is the one listed")
	assert.Zero(t, tags["asc-draft"])

	// A writer sees the draft; the pre-release switch narrows asc's releases like the hub's.
	opts.IncludeDrafts = true
	got, _ = mergeAscReleases(ctx, opts, hub, hubCount, 30, 1, records)
	assert.Equal(t, 1, countTag(got, "asc-draft"))
	opts.IsPreRelease = optional.Some(true)
	got, _ = mergeAscReleases(ctx, opts, nil, 0, 30, 1, records)
	require.Len(t, got, 1)
	assert.Equal(t, "asc-rc", got[0].TagName)
	opts.IsPreRelease, opts.Keyword = optional.None[bool](), "made with"
	got, _ = mergeAscReleases(ctx, opts, nil, 0, 30, 1, records)
	require.Len(t, got, 1)
	assert.Equal(t, "asc-1.0", got[0].TagName)

	// No records: the list is the hub's.
	same, n := mergeAscReleases(ctx, &repo_model.FindReleasesOptions{RepoID: 1}, hub, hubCount, 30, 1, nil)
	assert.Equal(t, hub, same)
	assert.Equal(t, hubCount, n)
}

func countTag(rels []*api.Release, tag string) int {
	n := 0
	for _, r := range rels {
		if r.TagName == tag {
			n++
		}
	}
	return n
}
