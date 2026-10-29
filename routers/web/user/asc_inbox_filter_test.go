// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInboxFilterRows(t *testing.T) {
	now := time.Now()
	rows := []AscInboxRow{
		{Entry: &AscInboxEntry{PullID: 1, RepoFullName: "b/x", Updated: now.Add(-time.Hour)}, Day: "Today"},
		{Agent: &AscDashAgent{Title: "run"}, Dash: AscDashRow{Where: "a/y"}},
		{Entry: &AscInboxEntry{PullID: 3, RepoFullName: "a/z", Updated: now.Add(-3 * time.Hour)}, Day: "Today"},
	}
	ids := func(rs []AscInboxRow) (out []int64) {
		for _, r := range rs {
			if r.Entry != nil {
				out = append(out, r.Entry.PullID)
			} else {
				out = append(out, 0)
			}
		}
		return out
	}
	assert.Equal(t, []int64{1, 0, 3}, ids(ascInboxFilterRows(rows, "", "")))
	assert.Equal(t, []int64{1, 3}, ids(ascInboxFilterRows(rows, "humans", "")))
	assert.Equal(t, []int64{0}, ids(ascInboxFilterRows(rows, "agents", "")))
	assert.Equal(t, []int64{0, 3, 1}, ids(ascInboxFilterRows(rows, "", "oldest")), "agents have no time and come first")
	assert.Equal(t, []int64{0, 3, 1}, ids(ascInboxFilterRows(rows, "", "repo")), "a/y, a/z, b/x")
	for _, r := range ascInboxFilterRows(rows, "", "repo") {
		assert.Empty(t, r.Day, "a sorted list has no day headings")
	}
	assert.Equal(t, "Today", rows[0].Day, "the input is not changed")

	assert.Equal(t, "humans", ascInboxFilterOf("humans", []string{"humans", "agents"}))
	assert.Empty(t, ascInboxFilterOf("<script>", []string{"humans", "agents"}))
	assert.Contains(t, ascInboxURLF("done", "a/b", 0, "humans", "repo"), "kind=humans")
}
