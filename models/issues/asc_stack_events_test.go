// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRepoIDsUpdatedBetween(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	const at = timeutil.TimeStamp(2000000000)
	touch := func(id int64, when timeutil.TimeStamp) {
		_, err := db.GetEngine(db.DefaultContext).ID(id).NoAutoTime().Cols("updated_unix").Update(&issues_model.Issue{UpdatedUnix: when})
		require.NoError(t, err)
	}
	touch(2, at)   // a pull request of repo 1
	touch(1, at)   // an issue of repo 1: not a pull request
	touch(3, at+1) // a pull request after the window

	ids, err := issues_model.PullRepoIDsUpdatedBetween(db.DefaultContext, at-1, at, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, ids)

	ids, err = issues_model.PullRepoIDsUpdatedBetween(db.DefaultContext, at, at, 10)
	require.NoError(t, err)
	assert.Empty(t, ids, "the window is open at its start")
}
