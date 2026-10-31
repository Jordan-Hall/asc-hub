// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIssueQuery(t *testing.T) {
	q := parseIssueQuery(`retry label:bug  author:maya "two words" label:"good first issue" is:open`)
	assert.Equal(t, []string{"bug", "good first issue"}, q.Labels)
	assert.Equal(t, []string{"maya"}, q.Authors)
	assert.Equal(t, `retry "two words" is:open`, q.Keyword, "the words and the other tokens stay, single spaced")

	q = parseIssueQuery("label: author: plain Label:Bug AUTHOR:Bob")
	assert.Equal(t, []string{"Bug"}, q.Labels, "the key is not case sensitive; an empty value is just a word")
	assert.Equal(t, []string{"Bob"}, q.Authors)
	assert.Equal(t, "label: author: plain", q.Keyword)

	assert.Equal(t, issueQuery{}, parseIssueQuery(""))
	var many string
	for i := 0; i < 30; i++ {
		many += " label:l"
	}
	assert.Len(t, parseIssueQuery(many).Labels, maxQueryFilters, "bounded")
}

func TestQueryFiltersResolveToIdsAndUnknownNamesMatchNothing(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "user2/repo1/issues")
	contexttest.LoadRepo(t, ctx, 1)
	ids := queryLabelIDs(ctx, ctx.Repo.Repository, []string{"label1", "no such label"})
	require.Len(t, ids, 2)
	assert.Equal(t, int64(1), ids[0], "label1 of repository 1")
	assert.Equal(t, nothingID, ids[1], "an unknown label is a filter nothing passes")

	assert.Equal(t, int64(0), queryPosterID(ctx, nil))
	assert.Equal(t, int64(2), queryPosterID(ctx, []string{"user2"}))
	assert.Equal(t, nothingID, queryPosterID(ctx, []string{"nobody-by-this-name"}))

	assert.Equal(t, []int64{1, 2, 3}, mergeIDs([]int64{1, 2}, []int64{2, 3}))
	assert.Equal(t, "1,2,3", joinIDs([]int64{1, 2, 3}))
}
