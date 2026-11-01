// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth_test

import (
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryCodesAreTenOneTimeCodesThatAreNeverKept(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	unused, total, err := auth_model.CountRecoveryCodes(ctx, 2)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, unused)

	codes, err := auth_model.GenerateRecoveryCodes(ctx, 2)
	require.NoError(t, err)
	require.Len(t, codes, auth_model.RecoveryCodeCount)
	seen := map[string]bool{}
	for _, c := range codes {
		assert.Regexp(t, `^[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}$`, c)
		assert.False(t, seen[c], "no two codes alike")
		seen[c] = true
	}
	unused, total, err = auth_model.CountRecoveryCodes(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 10, unused)
	assert.Equal(t, 10, total)

	// the codes are not in the table: only a salt and a hash
	var rows []*auth_model.TwoFactorRecoveryCode
	require.NoError(t, db.GetEngine(ctx).Where("uid = 2").Find(&rows))
	for _, r := range rows {
		for _, c := range codes {
			assert.NotContains(t, r.Hash, strings.ReplaceAll(c, "-", ""))
		}
	}

	// a code works once, typed in any case with or without dashes and spaces; another person's code does not work for me
	ok, err := auth_model.UseRecoveryCode(ctx, 2, strings.ToLower(strings.ReplaceAll(codes[0], "-", " ")))
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = auth_model.UseRecoveryCode(ctx, 2, codes[0])
	require.NoError(t, err)
	assert.False(t, ok, "spent")
	ok, err = auth_model.UseRecoveryCode(ctx, 4, codes[1])
	require.NoError(t, err)
	assert.False(t, ok, "not this person's")
	for _, bad := range []string{"", "ABCD", strings.Repeat("A", 40), "!!!!-!!!!-!!!!"} {
		ok, err = auth_model.UseRecoveryCode(ctx, 2, bad)
		require.NoError(t, err)
		assert.False(t, ok, bad)
	}
	unused, total, err = auth_model.CountRecoveryCodes(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, 9, unused)
	assert.Equal(t, 10, total)

	// new codes replace the old ones
	fresh, err := auth_model.GenerateRecoveryCodes(ctx, 2)
	require.NoError(t, err)
	ok, err = auth_model.UseRecoveryCode(ctx, 2, codes[1])
	require.NoError(t, err)
	assert.False(t, ok, "the old codes stopped working")
	ok, err = auth_model.UseRecoveryCode(ctx, 2, fresh[0])
	require.NoError(t, err)
	assert.True(t, ok)

	// they go with the second factor
	require.NoError(t, auth_model.DeleteRecoveryCodes(ctx, 2))
	_, total, err = auth_model.CountRecoveryCodes(ctx, 2)
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestDeletingTheSecondFactorDeletesTheRecoveryCodes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	tf := unittest.AssertExistsAndLoadBean(t, &auth_model.TwoFactor{})
	_, err := auth_model.GenerateRecoveryCodes(ctx, tf.UID)
	require.NoError(t, err)
	require.NoError(t, auth_model.DeleteTwoFactorByID(ctx, tf.ID, tf.UID))
	_, total, err := auth_model.CountRecoveryCodes(ctx, tf.UID)
	require.NoError(t, err)
	assert.Zero(t, total)
}
