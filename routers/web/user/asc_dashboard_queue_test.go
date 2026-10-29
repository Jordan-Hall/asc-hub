// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/automerge"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardQueueShowsTheChangesOfThePersonThatWait(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "GET /")
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	for _, e := range []*automerge.AscStackQueueEntry{
		{RepoID: 1, Branch: "master", PullID: 2, AscChange: "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", DoerID: 2, State: automerge.AscQueueQueued},
		{RepoID: 1, Branch: "master", PullID: 3, AscChange: "llllllllllllllllllllllllllllllll", DoerID: 4, State: automerge.AscQueueQueued},
		{RepoID: 1, Branch: "master", PullID: 5, AscChange: "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", DoerID: 2, State: automerge.AscQueueMerged},
	} {
		_, err := db.GetEngine(ctx).Insert(e)
		require.NoError(t, err)
	}
	entries := []*AscInboxEntry{
		{PullID: 2, Title: "first", Link: "/user2/repo1/pulls/2", Author: "user2"},
		{PullID: 3, Title: "somebody else's", Link: "/user2/repo1/pulls/3", Author: "user4"},
		{PullID: 5, Title: "done", Link: "/user2/repo1/pulls/5", Author: "user2"},
		{PullID: 99, Title: "not queued", Link: "/x", Author: "user2"},
	}
	rows := ascDashQueue(ctx, doer, entries, ctx.Locale)
	require.Len(t, rows, 1, "only a change of the person that waits: not another person's, not a merged one, not one outside the queue")
	assert.Equal(t, "first", rows[0].Title)
	assert.Equal(t, automerge.AscQueueQueued, rows[0].State)
	assert.Equal(t, 1, rows[0].Position)
	assert.Contains(t, rows[0].Where, "master")
}
