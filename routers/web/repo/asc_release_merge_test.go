// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscReleaseFilters(t *testing.T) {
	stable := &asctrust.ReleaseRecord{Tag: "v2", Title: "Two", Notes: "Fixes the bridge"}
	pre := &asctrust.ReleaseRecord{Tag: "v3rc", Prerelease: true}
	draft := &asctrust.ReleaseRecord{Tag: "v4", Draft: true}
	all := []*asctrust.ReleaseRecord{stable, pre, draft}
	assert.Equal(t, all, ascFilterReleases(all, releaseChannelAll, ""))
	assert.Equal(t, []*asctrust.ReleaseRecord{stable}, ascFilterReleases(all, releaseChannelStable, ""))
	assert.Equal(t, []*asctrust.ReleaseRecord{pre}, ascFilterReleases(all, releaseChannelPre, ""))
	assert.Equal(t, []*asctrust.ReleaseRecord{draft}, ascFilterReleases(all, releaseChannelDraft, ""))
	assert.Equal(t, []*asctrust.ReleaseRecord{stable}, ascFilterReleases(all, releaseChannelAll, "BRIDGE"), "the notes are searched, any case")
	assert.Equal(t, []*asctrust.ReleaseRecord{pre}, ascFilterReleases(all, releaseChannelAll, "v3"))
	assert.Empty(t, ascFilterReleases(all, releaseChannelAll, "nothing"))

	counts := ascAddChannelCounts([]ReleaseChannelCount{{Key: releaseChannelAll, Count: 4}, {Key: releaseChannelStable, Count: 2}, {Key: releaseChannelPre, Count: 1}, {Key: releaseChannelDraft, Count: 1}}, all, "")
	assert.Equal(t, []ReleaseChannelCount{{Key: "all", Count: 7}, {Key: "stable", Count: 3}, {Key: "pre", Count: 2}, {Key: "draft", Count: 2}}, counts)
}

func TestAscOnlyReleasesAreMergedAndTheHubRowWins(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "/user2/repo1/releases")
	contexttest.LoadRepo(t, ctx, 1)

	records := map[string]*asctrust.ReleaseRecord{
		"v1.1":         {Tag: "v1.1", Title: "from asc", Notes: "asc notes", Published: 946684801}, // the hub has a row for it
		"delete-tag":   {Tag: "delete-tag", Title: "was a bare tag", Notes: "notes", Published: 946684900},
		"asc-1.0":      {Tag: "asc-1.0", Title: "Made with asc", Notes: "**bold**", Published: 2_000_000_000, CreatedBy: "user2", AssetList: []asctrust.ReleaseAssetInfo{{Name: "a.tgz", Size: 12, SHA256: "ab", URL: "https://x.example/a.tgz"}, {Name: "b.tgz", Size: 1}}},
		"asc-draft":    {Tag: "asc-draft", Draft: true, Created: 1_900_000_000},
		"asc-1.0-rc.1": {Tag: "asc-1.0-rc.1", Prerelease: true, Published: 1_000_000_000},
	}

	readers := ascOnlyReleases(ctx, records, false)
	var tags []string
	for _, r := range readers {
		tags = append(tags, r.Tag)
	}
	assert.Equal(t, []string{"asc-1.0", "asc-1.0-rc.1", "delete-tag"}, tags, "newest first; no draft for a reader; the hub's release v1.1 is left to the hub; a bare tag is not a release")
	writers := ascOnlyReleases(ctx, records, true)
	require.Len(t, writers, 4)
	assert.Equal(t, "asc-draft", writers[1].Tag)

	// The page of the hub's own rows, with the asc ones merged in.
	opts := &repo_model.FindReleasesOptions{RepoID: 1, ListOptions: db.ListOptions{Page: 1, PageSize: 10}}
	hubRows, err := db.Find[repo_model.Release](t.Context(), opts)
	require.NoError(t, err)
	require.NotEmpty(t, hubRows)
	var hub []*ReleaseInfo
	for _, r := range hubRows {
		hub = append(hub, &ReleaseInfo{Release: r})
	}
	merged, total := mergeAscReleases(ctx, opts, hub, int64(len(hub)), readers, 10, 1)
	assert.Equal(t, int64(len(hub)), total, "the pager counts the hub's rows")
	require.Len(t, merged, len(hub)+3)
	assert.Equal(t, "asc-1.0", merged[0].Release.TagName, "the newest release is first")
	first := merged[0].Release
	assert.Negative(t, first.ID, "an entry of asc has an id no row has")
	assert.Equal(t, "Made with asc", first.Title)
	assert.Contains(t, string(first.RenderedNote), "<strong>bold</strong>")
	require.Len(t, first.Attachments, 1, "an asset with no https address is left out of the list")
	assert.Equal(t, "https://x.example/a.tgz", first.Attachments[0].DownloadURL())
	assert.NotNil(t, first.ArchiveDownloadCount)
	assert.NotNil(t, merged[0].Asc)
	ids := map[int64]bool{}
	for _, m := range merged {
		assert.False(t, ids[m.Release.ID], "ids differ")
		ids[m.Release.ID] = true
	}
	for _, m := range merged {
		if m.Release.TagName == "v1.1" {
			assert.Positive(t, m.Release.ID, "the hub row of v1.1 is the one listed")
			assert.NotEqual(t, "from asc", m.Release.Title)
		}
	}

	// No extras: nothing changes. A page past the hub's rows takes none of asc's.
	same, n := mergeAscReleases(ctx, opts, hub, int64(len(hub)), nil, 10, 1)
	assert.Equal(t, hub, same)
	assert.Equal(t, int64(len(hub)), n)
	none, _ := mergeAscReleases(ctx, opts, nil, int64(len(hub)), readers, 10, 99)
	assert.Empty(t, none, "an asc release rides on one page only")
}

func TestAscOnlyReleasesPageAmongThemselvesWhenTheHubHasNone(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "/user2/repo1/releases")
	contexttest.LoadRepo(t, ctx, 1)
	var extra []*asctrust.ReleaseRecord
	for i := range 5 {
		extra = append(extra, &asctrust.ReleaseRecord{Tag: "r" + string(rune('a'+i)), Published: int64(1000 - i)})
	}
	opts := &repo_model.FindReleasesOptions{RepoID: 1, Keyword: "no-such-release-anywhere"}
	p1, total := mergeAscReleases(ctx, opts, nil, 0, extra, 2, 1)
	assert.EqualValues(t, 5, total)
	assert.Len(t, p1, 2)
	p3, _ := mergeAscReleases(ctx, opts, nil, 0, extra, 2, 3)
	require.Len(t, p3, 1)
	assert.Equal(t, "re", p3[0].Release.TagName)
}

func TestAscOnlyReleaseDrawsReadOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "/user2/repo1/releases")
	contexttest.LoadRepo(t, ctx, 1)
	rec := &asctrust.ReleaseRecord{Tag: "asc-1.0", Title: "Made with asc", Notes: "notes", Published: 2_000_000_000, CreatedBy: "user2",
		AssetList: []asctrust.ReleaseAssetInfo{{Name: "a.tgz", Size: 12, SHA256: "ab", URL: "https://x.example/a.tgz"}}}
	info := ascReleaseInfo(ctx, rec, 0, map[string]*user_model.User{})
	html := renderPart(t, "repo/release/list", func(d map[string]any) {
		d["Title"] = "Releases"
		d["Releases"] = []*ReleaseInfo{info}
		d["CanCreateRelease"] = true
		d["PageIsReleaseList"] = true
		d["HdrData"] = d
		d["VerifyTag"] = func(*repo_model.Release) *asymkey.ObjectVerification { return nil }
		d["HasSignature"] = func(*asymkey.ObjectVerification) bool { return false }
	})
	assert.Contains(t, html, `data-testid="release-asc-only"`)
	assert.Contains(t, html, "Made with asc")
	assert.Contains(t, html, `href="https://x.example/a.tgz"`)
	assert.NotContains(t, html, "/releases/edit/asc-1.0", "an entry of asc has no edit link")
	assert.NotContains(t, html, "/releases/tag/asc-1.0")
}
