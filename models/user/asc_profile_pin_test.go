// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscProfilePins(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	ids, err := user_model.ListAscProfilePins(ctx, 2)
	require.NoError(t, err)
	assert.Empty(t, ids)

	// the order given is the order kept, and a new list replaces the old one
	require.NoError(t, user_model.SetAscProfilePins(ctx, 2, []int64{3, 1, 2}))
	ids, err = user_model.ListAscProfilePins(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 1, 2}, ids)
	require.NoError(t, user_model.SetAscProfilePins(ctx, 2, []int64{2}))
	ids, _ = user_model.ListAscProfilePins(ctx, 2)
	assert.Equal(t, []int64{2}, ids)

	// another person's pins are separate
	other, _ := user_model.ListAscProfilePins(ctx, 4)
	assert.Empty(t, other)

	// too many, repeated or invalid ids change nothing
	require.ErrorIs(t, user_model.SetAscProfilePins(ctx, 2, []int64{1, 2, 3, 4, 5, 6, 7}), user_model.ErrTooManyProfilePins)
	require.Error(t, user_model.SetAscProfilePins(ctx, 2, []int64{1, 1}))
	require.Error(t, user_model.SetAscProfilePins(ctx, 2, []int64{0}))
	ids, _ = user_model.ListAscProfilePins(ctx, 2)
	assert.Equal(t, []int64{2}, ids)
}
