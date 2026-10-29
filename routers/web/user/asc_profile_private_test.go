// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Work and Reviews tabs, their numbers and the online dot are activity of the person: they follow "Show my activity".
func TestProfileTabsFollowTheActivitySwitch(t *testing.T) {
	unittest.PrepareTestEnv(t)
	_, err := db.GetEngine(t.Context()).Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 2", true)
	require.NoError(t, err)
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.True(t, person.KeepActivityPrivate)

	probe := func(doerID int64) (work, reviews, counts bool) {
		for _, tab := range []string{"work", "reviews"} {
			ctx, _ := contexttest.MockContext(t, "GET /user2?tab="+tab)
			if doerID != 0 {
				contexttest.LoadUser(t, ctx, doerID)
			}
			ctx.ContextUser = person
			ascProfileTabData(ctx, tab)
			if tab == "work" {
				_, work = ctx.Data["AscProfileWork"]
			} else {
				_, reviews = ctx.Data["AscProfileReviews"]
			}
		}
		ctx, _ := contexttest.MockContext(t, "GET /user2")
		if doerID != 0 {
			contexttest.LoadUser(t, ctx, doerID)
		}
		ctx.ContextUser = person
		ascProfileCounts(ctx)
		_, counts = ctx.Data["AscWorkCount"]
		return work, reviews, counts
	}

	// another person, and a visitor who is not signed in: nothing
	for _, doer := range []int64{4, 0} {
		w, r, c := probe(doer)
		assert.False(t, w || r || c, "doer %d sees no tab data and no numbers", doer)
	}
	// the person and an administrator (user 1) see it
	for _, doer := range []int64{2, 1} {
		w, r, c := probe(doer)
		assert.True(t, w && r && c, "doer %d sees the tabs", doer)
	}

	// and with the switch off for everybody, anybody does
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `user` SET keep_activity_private = ? WHERE id = 2", false)
	require.NoError(t, err)
	person = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	w, r, c := probe(4)
	assert.True(t, w && r && c)
}
