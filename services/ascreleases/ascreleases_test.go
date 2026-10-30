// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascreleases

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func TestPageOfPlacesAReleaseAfterTheLastRowAsNewAsIt(t *testing.T) {
	times := []int64{100, 90, 80, 70, 60} // five hub rows, newest first; pages of two
	for at, want := range map[int64]int{
		200: 1, // newer than every row: the first page
		100: 1, // one row is as new: after it
		95:  1,
		90:  1, // two rows: the end of the first page
		85:  1,
		80:  2, // three rows
		75:  2,
		70:  2, // four rows
		65:  2,
		60:  3, // five rows
		10:  3, // older than every row: the last page
	} {
		assert.Equal(t, want, PageOf(times, at, 2), "time %d", at)
	}
	assert.Equal(t, 1, PageOf(nil, 10, 2), "no rows: page 1")
	assert.Equal(t, 1, PageOf(times, 10, 0))
}

func TestFilter(t *testing.T) {
	stable := &asctrust.ReleaseRecord{Tag: "v2", Title: "Two", Notes: "Fixes the bridge"}
	pre := &asctrust.ReleaseRecord{Tag: "v3rc", Prerelease: true}
	draft := &asctrust.ReleaseRecord{Tag: "v4", Draft: true}
	all := []*asctrust.ReleaseRecord{stable, pre, draft}
	assert.Equal(t, all, Filter{}.Apply(all))
	assert.Equal(t, []*asctrust.ReleaseRecord{pre}, Filter{IsPreRelease: optional.Some(true), IsDraft: optional.Some(false)}.Apply(all))
	assert.Equal(t, []*asctrust.ReleaseRecord{draft}, Filter{IsDraft: optional.Some(true)}.Apply(all))
	assert.Equal(t, []*asctrust.ReleaseRecord{stable, draft}, Filter{IsPreRelease: optional.Some(false)}.Apply(all))
	assert.Equal(t, []*asctrust.ReleaseRecord{stable}, Filter{Keyword: "BRIDGE"}.Apply(all), "the notes are searched, any case")
	assert.Empty(t, Filter{Keyword: "nothing"}.Apply(all))
}

func TestOnlyAndForPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	records := map[string]*asctrust.ReleaseRecord{
		"v1.1":       {Tag: "v1.1", Published: 946684801},       // the hub has a row for it
		"delete-tag": {Tag: "delete-tag", Published: 946684900}, // a bare tag of the hub is not a release
		"asc-1.0":    {Tag: "asc-1.0", Published: 2_000_000_000},
		"asc-draft":  {Tag: "asc-draft", Draft: true, Created: 1_900_000_000},
	}
	var tags []string
	for _, r := range Only(t.Context(), 1, records, false) {
		tags = append(tags, r.Tag)
	}
	assert.Equal(t, []string{"asc-1.0", "delete-tag"}, tags)
	assert.Len(t, Only(t.Context(), 1, records, true), 3)
	assert.Nil(t, Only(t.Context(), 1, nil, true))

	extra := Only(t.Context(), 1, records, false)
	opts := &repo_model.FindReleasesOptions{RepoID: 1, ListOptions: db.ListOptions{Page: 1, PageSize: 10}}
	hubCount, err := db.Count[repo_model.Release](t.Context(), opts)
	require.NoError(t, err)
	onPage, total := ForPage(t.Context(), opts, extra, hubCount, 10, 1)
	assert.Equal(t, hubCount, total, "the pager counts the hub's rows")
	require.NotEmpty(t, onPage)
	assert.Equal(t, "asc-1.0", onPage[0].Tag)
	none, _ := ForPage(t.Context(), opts, extra, hubCount, 10, 99)
	assert.Empty(t, none, "a release rides on one page only")
	same, n := ForPage(t.Context(), opts, nil, hubCount, 10, 1)
	assert.Empty(t, same)
	assert.Equal(t, hubCount, n)

	// With no row of the hub the releases of asc are paged among themselves.
	empty := &repo_model.FindReleasesOptions{RepoID: 1, Keyword: "no-such-release-anywhere"}
	var five []*asctrust.ReleaseRecord
	for i := range 5 {
		five = append(five, &asctrust.ReleaseRecord{Tag: "r" + string(rune('a'+i)), Published: int64(1000 - i)})
	}
	p3, total := ForPage(t.Context(), empty, five, 0, 2, 3)
	assert.EqualValues(t, 5, total)
	require.Len(t, p3, 1)
	assert.Equal(t, "re", p3[0].Tag)
}
