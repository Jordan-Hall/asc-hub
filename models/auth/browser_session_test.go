// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth_test

import (
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserSessionsAreListedAndSignedOutFromElsewhere(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext

	// two browsers sign in; the session IDs are never kept
	for _, sid := range []string{"sid-laptop", "sid-phone"} {
		revoked, err := auth_model.TouchBrowserSession(ctx, 2, sid, "", "Mozilla/5.0 Firefox/140.0", "203.0.113.7")
		require.NoError(t, err)
		assert.False(t, revoked)
	}
	unittest.AssertNotExistsBean(t, &auth_model.BrowserSession{SIDHash: "sid-laptop"})
	rows, err := auth_model.ListBrowserSessions(ctx, 2)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "203.0.113.7", rows[0].IP)

	// the phone remembers the person: signing it out ends its remember-me token too
	lookup, _, err := auth_model.GenerateAuthToken(ctx, 2, optional.None[int64](), timeutil.TimeStampNow().Add(3600), auth_model.LongTermAuthorization)
	require.NoError(t, err)
	_, err = auth_model.TouchBrowserSession(ctx, 2, "sid-phone", lookup, "Mozilla/5.0 Firefox/140.0", "203.0.113.8")
	require.NoError(t, err)
	phone := unittest.AssertExistsAndLoadBean(t, &auth_model.BrowserSession{SIDHash: auth_model.HashSessionID("sid-phone")})
	assert.Equal(t, lookup, phone.RememberKey)

	// another person cannot sign it out
	found, err := auth_model.RevokeBrowserSession(ctx, 1, phone.ID)
	require.NoError(t, err)
	assert.False(t, found)

	found, err = auth_model.RevokeBrowserSession(ctx, 2, phone.ID)
	require.NoError(t, err)
	assert.True(t, found)
	unittest.AssertNotExistsBean(t, &auth_model.AuthorizationToken{LookupKey: lookup})
	revoked, err := auth_model.TouchBrowserSession(ctx, 2, "sid-phone", "", "", "")
	require.NoError(t, err)
	assert.True(t, revoked, "the phone's next request ends its session")

	rows, err = auth_model.ListBrowserSessions(ctx, 2)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// sign out everywhere else keeps this browser
	_, err = auth_model.TouchBrowserSession(ctx, 2, "sid-desk", "", "", "")
	require.NoError(t, err)
	n, err := auth_model.RevokeOtherBrowserSessions(ctx, 2, "sid-desk")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	revoked, err = auth_model.TouchBrowserSession(ctx, 2, "sid-desk", "", "", "")
	require.NoError(t, err)
	assert.False(t, revoked)
	revoked, err = auth_model.TouchBrowserSession(ctx, 2, "sid-laptop", "", "", "")
	require.NoError(t, err)
	assert.True(t, revoked)

	// a session of one person is never taken over by another
	revoked, err = auth_model.TouchBrowserSession(ctx, 1, "sid-desk", "", "", "")
	require.NoError(t, err)
	assert.True(t, revoked)

	// signing out drops the row
	require.NoError(t, auth_model.EndBrowserSession(ctx, "sid-desk"))
	unittest.AssertNotExistsBean(t, &auth_model.BrowserSession{SIDHash: auth_model.HashSessionID("sid-desk")})
}
