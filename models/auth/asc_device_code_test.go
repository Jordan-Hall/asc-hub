// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth_test

import (
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscUserCodeForms(t *testing.T) {
	assert.Equal(t, "WDJBMJHT", auth_model.NormalizeAscUserCode("wdjb-mjht"))
	assert.Equal(t, "WDJBMJHT", auth_model.NormalizeAscUserCode(" WDJB MJHT "))
	// vowels and digits are not in the alphabet, so they drop out
	assert.Equal(t, "BCD", auth_model.NormalizeAscUserCode("a1b-c.d"))
	assert.Equal(t, "WDJB-MJHT", auth_model.FormatAscUserCode("WDJBMJHT"))
	assert.Equal(t, "ABC", auth_model.FormatAscUserCode("ABC"))
}

func TestAscDeviceCodeApproveExchangeOnce(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "maya-laptop (Windows)")
	require.NoError(t, err)
	assert.Len(t, row.UserCode, 8)
	assert.NotEqual(t, deviceCode, row.DeviceCodeHash, "only the hash of the device code is stored")

	// not answered yet; a second poll within the interval is told to slow down
	_, err = auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodePending)
	_, err = auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeSlowDown)

	// the person finds the code by what they typed, in any case and with the dash
	found, err := auth_model.GetAscDeviceCodeByUserCode(ctx, auth_model.FormatAscUserCode(row.UserCode))
	require.NoError(t, err)
	require.NoError(t, found.Decide(ctx, 2, true))
	// the answer is final
	require.ErrorIs(t, found.Decide(ctx, 3, false), auth_model.ErrAscDeviceCodeDecided)

	token, err := auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.NoError(t, err)
	assert.Equal(t, int64(2), token.UID)
	assert.NotEmpty(t, token.Token)
	assert.Contains(t, token.Name, "maya-laptop (Windows)")
	unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{UID: 2, TokenHash: token.TokenHash})

	// the token is handed out once
	_, err = auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeNotFound)
}

func TestAscDeviceCodeDeny(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "")
	require.NoError(t, err)
	require.NoError(t, row.Decide(ctx, 2, false))
	_, err = auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeDenied)
	unittest.AssertNotExistsBean(t, &auth_model.AccessToken{UID: 2, Name: "asc (" + auth_model.FormatAscUserCode(row.UserCode) + ")"})
}

func TestAscDeviceCodeExpiry(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "ci")
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(row.ID).Cols("expires_unix").Update(&auth_model.AscDeviceCode{ExpiresUnix: timeutil.TimeStampNow().Add(-1)})
	require.NoError(t, err)

	_, err = auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeExpired)
	_, err = auth_model.GetAscDeviceCodeByUserCode(ctx, row.UserCode)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeExpired)
	// an expired code cannot be approved, even by a stale page
	require.ErrorIs(t, row.Decide(ctx, 2, true), auth_model.ErrAscDeviceCodeExpired)
}

func TestAscDeviceCodeUnknownAndBadInput(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	_, err := auth_model.ExchangeAscDeviceCode(ctx, "nope", auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeNotFound)
	_, err = auth_model.GetAscDeviceCodeByUserCode(ctx, "BCDFGHJK")
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeNotFound)
	_, err = auth_model.GetAscDeviceCodeByUserCode(ctx, "BCD")
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeNotFound)
	_, _, err = auth_model.CreateAscDeviceCode(ctx, "line\nbreak")
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeBadDevice)
}

func TestAscDeviceCodesAreUnique(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	seen := map[string]bool{}
	for range 50 {
		row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "")
		require.NoError(t, err)
		assert.False(t, seen[row.UserCode] || seen[deviceCode])
		seen[row.UserCode], seen[deviceCode] = true, true
	}
}
