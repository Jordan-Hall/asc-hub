// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/services/automerge"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardQueueShowsTheChangesOfThePersonThatWait(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "GET /")
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
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
	card := ascDashQueue(ctx, doer, entries, l)
	rows := card.Rows
	require.Len(t, rows, 1, "only a change of the person that waits: not another person's, not a merged one, not one outside the queue")
	assert.Equal(t, "first", rows[0].Title)
	assert.Equal(t, automerge.AscQueueQueued, rows[0].State)
	assert.Equal(t, 1, rows[0].Position)
	assert.Contains(t, rows[0].Where, "master")
	assert.Equal(t, "/user2/repo1/asc/queue?branch=master", rows[0].Open)
	assert.False(t, card.Running, "nothing holds the branch")
	assert.Empty(t, card.Median, "asc is off: no timing")
	assert.Empty(t, rows[0].ETA)

	t.Run("with the timing of asc and the checks of the head", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		calls := 0
		defer test.MockVariableValue(&ascDashQueueTiming, func(_ gocontext.Context, repo *repo_model.Repository, branch string) (string, map[string]string, error) {
			calls++
			assert.Equal(t, "user2/repo1", repo.FullName())
			assert.Equal(t, "master", branch)
			return "9 min", map[string]string{"kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk": "4 min"}, nil
		})()
		defer test.MockVariableValue(&ascInboxInfoLoad, func(gocontext.Context, *AscInboxEntry) *AscInboxInfo {
			return &AscInboxInfo{ChecksPass: 2, ChecksPending: 1}
		})()
		card := ascDashQueue(ctx, doer, entries, l)
		require.Len(t, card.Rows, 1)
		assert.Equal(t, "9 min", card.Median)
		assert.Equal(t, "2 of 3 checks", card.Rows[0].Checks)
		assert.Equal(t, "lands in about 4 min", card.Rows[0].ETA)
		assert.Equal(t, 1, calls, "one call to asc for each queue")
	})

	t.Run("asc not answering leaves the timing out", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, true)()
		defer test.MockVariableValue(&ascDashQueueTiming, func(gocontext.Context, *repo_model.Repository, string) (string, map[string]string, error) {
			return "", nil, errors.New("unavailable")
		})()
		card := ascDashQueue(ctx, doer, entries, l)
		require.Len(t, card.Rows, 1)
		assert.Empty(t, card.Median)
		assert.Empty(t, card.Rows[0].ETA)
	})
}

func TestAscDashChecksText(t *testing.T) {
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	assert.Empty(t, ascDashChecksText(l, nil))
	assert.Empty(t, ascDashChecksText(l, &AscInboxInfo{}), "no checks")
	assert.Empty(t, ascDashChecksText(l, &AscInboxInfo{ChecksPass: 3}), "all pass")
	assert.Equal(t, "2 of 3 checks", ascDashChecksText(l, &AscInboxInfo{ChecksPass: 2, ChecksFail: 1}))
	assert.Equal(t, "0 of 1 checks", ascDashChecksText(l, &AscInboxInfo{ChecksPending: 1}))
}
