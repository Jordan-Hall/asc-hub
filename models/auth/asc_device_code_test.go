// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth_test

import (
	"testing"
	"time"

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

	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "maya-laptop (Windows)", "")
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
	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "", "")
	require.NoError(t, err)
	require.NoError(t, row.Decide(ctx, 2, false))
	_, err = auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AccessTokenScopeAll)
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeDenied)
	unittest.AssertNotExistsBean(t, &auth_model.AccessToken{UID: 2, Name: "asc (" + auth_model.FormatAscUserCode(row.UserCode) + ")"})
}

func TestAscDeviceCodeExpiry(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "ci", "")
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
	_, _, err = auth_model.CreateAscDeviceCode(ctx, "line\nbreak", "")
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeBadDevice)
}

func TestAscDeviceCodesAreUnique(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	seen := map[string]bool{}
	for range 50 {
		row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "", "")
		require.NoError(t, err)
		assert.False(t, seen[row.UserCode] || seen[deviceCode])
		seen[row.UserCode], seen[deviceCode] = true, true
	}
}

func TestAscDeviceCodeCapsPending(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	oldTotal, oldIP := auth_model.AscDeviceCodeMaxPending, auth_model.AscDeviceCodeMaxPendingPerIP
	t.Cleanup(func() { auth_model.AscDeviceCodeMaxPending, auth_model.AscDeviceCodeMaxPendingPerIP = oldTotal, oldIP })
	auth_model.AscDeviceCodeMaxPending, auth_model.AscDeviceCodeMaxPendingPerIP = 5, 2

	// one address can have two codes waiting; the third is refused, another address is not affected
	for range 2 {
		_, _, err := auth_model.CreateAscDeviceCode(ctx, "", "203.0.113.7")
		require.NoError(t, err)
	}
	_, _, err := auth_model.CreateAscDeviceCode(ctx, "", "203.0.113.7")
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeTooMany)
	row, _, err := auth_model.CreateAscDeviceCode(ctx, "", "203.0.113.8")
	require.NoError(t, err)

	// an answered code no longer counts as waiting
	require.NoError(t, row.Decide(ctx, 2, false))
	_, _, err = auth_model.CreateAscDeviceCode(ctx, "", "203.0.113.8")
	require.NoError(t, err)

}

func TestDeleteExpiredAscDeviceCodes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	old, _, err := auth_model.CreateAscDeviceCode(ctx, "", "")
	require.NoError(t, err)
	recent, _, err := auth_model.CreateAscDeviceCode(ctx, "", "")
	require.NoError(t, err)
	// ended two days ago, and ended an hour ago
	_, err = db.GetEngine(ctx).ID(old.ID).Cols("expires_unix").Update(&auth_model.AscDeviceCode{ExpiresUnix: timeutil.TimeStampNow().Add(-2 * 86400)})
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(recent.ID).Cols("expires_unix").Update(&auth_model.AscDeviceCode{ExpiresUnix: timeutil.TimeStampNow().Add(-3600)})
	require.NoError(t, err)

	n, err := auth_model.DeleteExpiredAscDeviceCodes(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	unittest.AssertNotExistsBean(t, &auth_model.AscDeviceCode{ID: old.ID})
	unittest.AssertExistsAndLoadBean(t, &auth_model.AscDeviceCode{ID: recent.ID})
}

func TestAscDeviceCodeFullHubEvictsTheOldestInsteadOfLockingOthersOut(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	oldTotal, oldIP := auth_model.AscDeviceCodeMaxPending, auth_model.AscDeviceCodeMaxPendingPerIP
	t.Cleanup(func() { auth_model.AscDeviceCodeMaxPending, auth_model.AscDeviceCodeMaxPendingPerIP = oldTotal, oldIP })
	auth_model.AscDeviceCodeMaxPending, auth_model.AscDeviceCodeMaxPendingPerIP = 3, 3

	// an approved code that is in use, then waiting codes from attackers that fill the hub
	used, _, err := auth_model.CreateAscDeviceCode(ctx, "in use", "192.0.2.1")
	require.NoError(t, err)
	require.NoError(t, used.Decide(ctx, 2, true))
	var first *auth_model.AscDeviceCode
	for i, ip := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		row, _, err := auth_model.CreateAscDeviceCode(ctx, "", ip)
		require.NoError(t, err)
		if i == 0 {
			first = row
		}
	}

	// a different address still gets a code; the oldest waiting code goes, the approved one stays
	_, _, err = auth_model.CreateAscDeviceCode(ctx, "", "203.0.113.50")
	require.NoError(t, err)
	unittest.AssertNotExistsBean(t, &auth_model.AscDeviceCode{ID: first.ID})
	unittest.AssertExistsAndLoadBean(t, &auth_model.AscDeviceCode{ID: used.ID})

	// an address over its own cap is refused without pushing anyone out
	for range 3 {
		_, _, err = auth_model.CreateAscDeviceCode(ctx, "", "198.51.100.77")
		require.NoError(t, err)
	}
	before, err := db.GetEngine(ctx).Count(new(auth_model.AscDeviceCode))
	require.NoError(t, err)
	_, _, err = auth_model.CreateAscDeviceCode(ctx, "", "198.51.100.77")
	require.ErrorIs(t, err, auth_model.ErrAscDeviceCodeTooMany)
	after, err := db.GetEngine(ctx).Count(new(auth_model.AscDeviceCode))
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestParseAscDeviceScope(t *testing.T) {
	def, err := auth_model.ParseAscDeviceScope("")
	require.NoError(t, err)
	want, _ := auth_model.AscDeviceDefaultScope.Normalize()
	assert.Equal(t, want, def)

	got, err := auth_model.ParseAscDeviceScope("read:repository read:changes")
	require.NoError(t, err)
	assert.Equal(t, auth_model.AccessTokenScope("read:repository,read:changes"), got)

	// a terminal can never ask for more than the work of a person: not all, not admin, not packages
	for _, bad := range []string{"all", "write:admin", "write:package", "write:user", "write:organization", "public-only", "nonsense"} {
		_, err := auth_model.ParseAscDeviceScope(bad)
		require.ErrorIs(t, err, auth_model.ErrAscDeviceBadScope, bad)
	}
}

func TestExchangeAscDeviceCodeScopeAndExpiry(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, "laptop", "")
	require.NoError(t, err)
	scope, err := auth_model.ParseAscDeviceScope("read:repository")
	require.NoError(t, err)
	require.NoError(t, row.SetScope(ctx, scope))
	require.NoError(t, row.Decide(ctx, 2, true))

	token, err := auth_model.ExchangeAscDeviceCode(ctx, deviceCode, auth_model.AscDeviceDefaultScope)
	require.NoError(t, err)
	assert.Equal(t, scope, token.Scope, "the scope the terminal asked for wins over the default")
	assert.NotEqual(t, auth_model.AccessTokenScopeAll, token.Scope)
	require.True(t, token.HasExpiry())
	left := time.Until(token.ExpiresUnix.AsTime())
	assert.Greater(t, left, auth_model.AscDeviceTokenLifetime-time.Minute)
	assert.LessOrEqual(t, left, auth_model.AscDeviceTokenLifetime)
}
