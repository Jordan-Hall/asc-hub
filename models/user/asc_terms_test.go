// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscTermsAcceptance(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	row, err := user_model.GetAscTermsAcceptance(ctx, 2)
	require.NoError(t, err)
	assert.Nil(t, row, "no record before the box was ticked")

	require.NoError(t, user_model.RecordAscTermsAcceptance(ctx, 2))
	row, err = user_model.GetAscTermsAcceptance(ctx, 2)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.InDelta(t, int64(timeutil.TimeStampNow()), int64(row.AcceptedUnix), 5)

	// a second agreement keeps the first time and adds no row
	_, err = db.GetEngine(ctx).Exec("UPDATE asc_terms_acceptance SET accepted_unix = 100 WHERE user_id = 2")
	require.NoError(t, err)
	require.NoError(t, user_model.RecordAscTermsAcceptance(ctx, 2))
	row, _ = user_model.GetAscTermsAcceptance(ctx, 2)
	assert.EqualValues(t, 100, row.AcceptedUnix)
	unittest.AssertCount(t, &user_model.AscTermsAcceptance{UserID: 2}, 1)

	other, _ := user_model.GetAscTermsAcceptance(ctx, 4)
	assert.Nil(t, other, "the record is per person")
}
