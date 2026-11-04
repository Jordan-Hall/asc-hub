// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefTagsOf(t *testing.T) {
	rel := func(name string, commits int64, tag bool) *repo_model.Release {
		return &repo_model.Release{TagName: name, Title: "Release " + name, NumCommits: commits, IsTag: tag}
	}
	tags := refTagsOf([]*repo_model.Release{rel("v3", 12, false), rel("v2-hotfix", 9, true), rel("v2", 10, false), rel("v1", 0, false), rel("v0", 4, false)})
	require.Len(t, tags, 5)
	assert.EqualValues(t, 3, tags[0].Changes, "v3 is three changes ahead of the tag before it")
	assert.Equal(t, "Release v3", tags[0].Title)
	assert.Zero(t, tags[1].Changes, "a tag on another branch is not ahead of the next one")
	assert.Empty(t, tags[1].Title, "a bare tag has no release title")
	assert.EqualValues(t, 10, tags[2].Changes, "the tag before has no stored count")
	assert.Zero(t, tags[3].Changes, "no stored count")
	assert.EqualValues(t, 4, tags[4].Changes, "the oldest tag: all of its changes")

	var many []*repo_model.Release
	for i := range refTagRows + 1 {
		many = append(many, rel("t", int64(100-i), false))
	}
	tags = refTagsOf(many)
	require.Len(t, tags, refTagRows, "the extra release only counts the last row")
	assert.EqualValues(t, 1, tags[refTagRows-1].Changes)
}

func TestRefTagsSection(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	out := renderPart(t, "repo/branch/ref_tags", func(d map[string]any) {
		at := timeutil.TimeStampNow()
		d["RefTags"] = []*refTag{
			{Name: "v1.1", Title: "Faster sync", Changes: 4, Release: &repo_model.Release{CreatedUnix: at}},
			{Name: "v1.0", Changes: 1, Release: &repo_model.Release{CreatedUnix: at}},
			{Name: "v0.9", Release: &repo_model.Release{CreatedUnix: at}},
		}
	})
	assert.Contains(t, out, `href="/user2/repo1/releases/tag/v1.1">v1.1</a>`)
	assert.Contains(t, out, "Faster sync")
	assert.Equal(t, 2, countOf(out, "asc_rp.ref_tag_changes_one"), "the test locale gives the first form for every count")
	assert.Equal(t, 3, countOf(out, "<time>"))
	assert.Equal(t, 2, countOf(out, `class="ds-num"`), "no count when it is not known")
	assert.NotContains(t, renderPart(t, "repo/branch/ref_tags", func(map[string]any) {}), "rp-ref-tags", "no tags: no section")
}
