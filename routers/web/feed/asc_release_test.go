// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package feed

import (
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/gorilla/feeds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscReleasesInTheFeed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "/user2/repo1.atom")
	contexttest.LoadRepo(t, ctx, 1)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	records := map[string]*asctrust.ReleaseRecord{
		"v1.1":      {Tag: "v1.1", Title: "from asc", Published: 946684801}, // the hub's release: left to the hub
		"asc-1.0":   {Tag: "asc-1.0", Title: "Made with asc", Notes: "**bold** <script>x</script>", Published: 2_000_000_000, CreatedBy: "user2"},
		"asc-draft": {Tag: "asc-draft", Draft: true, Created: 1_900_000_000},
		"asc-0.9":   {Tag: "asc-0.9", Published: 1_000_000_000},
	}
	items, err := ascReleasesToFeedItems(ctx, repo, records)
	require.NoError(t, err)
	require.Len(t, items, 2, "no draft in a public feed, and the hub's release is not repeated")
	assert.Equal(t, "Made with asc", items[0].Title)
	assert.Equal(t, "asc-0.9", items[1].Title, "a release with no title is named by its tag")
	assert.Contains(t, items[0].Content, "<strong>bold</strong>")
	assert.NotContains(t, items[0].Content, "<script>")
	assert.Contains(t, items[0].Link.Href, "/user2/repo1/src/tag/asc-1.0")
	assert.Equal(t, time.Unix(2_000_000_000, 0), items[0].Created)
	require.NotNil(t, items[0].Author)
	assert.Equal(t, "user2", items[0].Author.Name)
	assert.Nil(t, items[1].Author)
	assert.NotEqual(t, items[0].Id, items[1].Id)

	none, err := ascReleasesToFeedItems(ctx, repo, nil)
	require.NoError(t, err)
	assert.Empty(t, none)

	hub := []*feeds.Item{{Title: "hub new", Created: time.Unix(2_100_000_000, 0)}, {Title: "hub old", Created: time.Unix(500, 0)}}
	merged := mergeFeedItems(hub, items)
	var titles []string
	for _, m := range merged {
		titles = append(titles, m.Title)
	}
	assert.Equal(t, []string{"hub new", "Made with asc", "asc-0.9", "hub old"}, titles, "newest first")
	assert.Equal(t, hub, mergeFeedItems(hub, nil))
}
