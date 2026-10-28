// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLicenseFileName(t *testing.T) {
	assert.Equal(t, "LICENSE", licenseFileName([]string{"README.md", "LICENSE"}))
	assert.Equal(t, "License.md", licenseFileName([]string{"Cargo.toml", "License.md"}))
	assert.Equal(t, "COPYING", licenseFileName([]string{"COPYING"}))
	assert.Empty(t, licenseFileName([]string{"README.md", "licenses-list.txt"}))
	assert.Empty(t, licenseFileName(nil))
}

func TestHomeStackOfNeedsPullAccess(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.NotEmpty(t, pr.MergedCommitID, "the fixture is a merged pull request")

	name, link := homeStackOf(t.Context(), pr.BaseRepoID, "/user2/repo1", pr.MergedCommitID, false)
	assert.Empty(t, name, "code-only access sees no pull request data")
	assert.Empty(t, link)

	name, link = homeStackOf(t.Context(), pr.BaseRepoID, "/user2/repo1", pr.MergedCommitID, true)
	assert.Equal(t, pr.HeadBranch, name)
	assert.Equal(t, fmt.Sprintf("/user2/repo1/pulls/%d", pr.Index), link)

	name, _ = homeStackOf(t.Context(), pr.BaseRepoID, "/user2/repo1", "0000000000000000000000000000000000000000", true)
	assert.Empty(t, name, "a commit that landed through no pull request has no stack")
}

func TestWeeklyCounts(t *testing.T) {
	now := time.Unix(10_000_000, 0)
	const week = int64(7 * 24 * 3600)
	times := []int64{now.Unix() - 10, now.Unix() - week - 5, now.Unix() - week - 6, now.Unix() - 11*week - 1, now.Unix() - 12*week - 1, now.Unix() + 100}
	counts := weeklyCounts(times, now, 12)
	assert.Equal(t, []int{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 1}, counts, "the oldest week first, a time out of the window is ignored")
	assert.Equal(t, make([]int, 4), weeklyCounts(nil, now, 4))
}

func TestSparkPoints(t *testing.T) {
	assert.Equal(t, "0.0,23.0 50.0,1.0 100.0,12.0", sparkPoints([]int{0, 4, 2}))
	assert.Equal(t, "0.0,23.0 100.0,23.0", sparkPoints([]int{0, 0}), "a flat series is a flat line")
	assert.Empty(t, sparkPoints([]int{3}))
}

func TestParseActivityLog(t *testing.T) {
	out := "100\x00Priya Raman\x00priya@example.org\n" +
		"90\x00Diego\x00diego@example.org\n" +
		"80\x00priya raman\x00PRIYA@example.org\n" +
		"garbage\n" +
		"x\x00Bad\x00bad@example.org\n" +
		"70\x00Hannah\x00hannah@example.org\n"
	times, people := parseActivityLog(out, 2)
	assert.Equal(t, []int64{100, 90, 80, 70}, times, "every well-formed line is a change")
	require.Len(t, people, 2, "at most limit people")
	assert.Equal(t, "Priya Raman", people[0].Name, "people are grouped by email without regard to case")
	assert.Equal(t, 2, people[0].Count)
	assert.Equal(t, 100, people[0].Pct)
	assert.Equal(t, "Diego", people[1].Name, "ties are ordered by name")
	assert.Equal(t, 50, people[1].Pct)

	times, people = parseActivityLog("", 5)
	assert.Empty(t, times)
	assert.Empty(t, people)
}
