// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscExportWatch(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	require.NoError(t, user_model.WatchAscExport(ctx, 2, "ex_aaaaaaaaaaaaaaaaaaaa"))
	require.NoError(t, user_model.WatchAscExport(ctx, 2, "ex_aaaaaaaaaaaaaaaaaaaa"), "the same export twice is one watch")
	require.NoError(t, user_model.WatchAscExport(ctx, 4, "ex_aaaaaaaaaaaaaaaaaaaa"))

	rows, err := user_model.AscExportWatches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, int64(2), rows[0].UserID)
	assert.Equal(t, "ex_aaaaaaaaaaaaaaaaaaaa", rows[0].ExportID)
	assert.NotZero(t, rows[0].CreatedUnix)

	require.NoError(t, user_model.UnwatchAscExport(ctx, rows[0].ID))
	rows, err = user_model.AscExportWatches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(4), rows[0].UserID)
}
