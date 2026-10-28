// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscPushEndpointIsPublicHttps(t *testing.T) {
	for _, ok := range []string{"https://fcm.googleapis.com/fcm/send/abc", "https://updates.push.services.mozilla.com:443/wpush/v2/x"} {
		assert.NoError(t, user_model.ValidateAscPushEndpoint(ok), ok)
	}
	for _, bad := range []string{
		"", "http://push.example.net/x", "ftp://push.example.net", "https://user:pw@push.example.net/x", "https://push.example.net/x#f",
		"https://localhost/x", "https://a.localhost/x", "https://127.0.0.1/x", "https://[::1]/x", "https://10.0.0.5/x", "https://192.168.1.1/x",
		"https://169.254.169.254/latest", "https://0.0.0.0/x", "https://printer.local/x", "https://db.internal/x", "/relative", "https://" + strings.Repeat("a", 1000) + ".net/x",
	} {
		assert.Error(t, user_model.ValidateAscPushEndpoint(bad), bad)
	}
}

func TestAscPushSubscriptions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	require.NoError(t, user_model.SaveAscPushSubscription(ctx, 2, "https://push.example.net/a", "k", "a"))
	require.NoError(t, user_model.SaveAscPushSubscription(ctx, 2, "https://push.example.net/a", "k2", "a2"))
	subs, err := user_model.ListAscPushSubscriptions(ctx, 2)
	require.NoError(t, err)
	require.Len(t, subs, 1, "subscribing again replaces")
	assert.Equal(t, "k2", subs[0].P256dh)

	// the same browser used by another person moves to that person
	require.NoError(t, user_model.SaveAscPushSubscription(ctx, 4, "https://push.example.net/a", "k3", "a3"))
	subs, _ = user_model.ListAscPushSubscriptions(ctx, 2)
	assert.Empty(t, subs)
	subs, _ = user_model.ListAscPushSubscriptions(ctx, 4)
	assert.Len(t, subs, 1)

	// a person cannot remove the subscription of another
	gone, err := user_model.DeleteAscPushSubscription(ctx, 2, "https://push.example.net/a")
	require.NoError(t, err)
	assert.False(t, gone)
	gone, err = user_model.DeleteAscPushSubscription(ctx, 4, "https://push.example.net/a")
	require.NoError(t, err)
	assert.True(t, gone)

	for i := range user_model.AscMaxPushSubscriptions {
		require.NoError(t, user_model.SaveAscPushSubscription(ctx, 2, "https://push.example.net/n"+string(rune('a'+i)), "k", "a"))
	}
	require.ErrorIs(t, user_model.SaveAscPushSubscription(ctx, 2, "https://push.example.net/one-more", "k", "a"), user_model.ErrTooManyPushSubscriptions)
	require.Error(t, user_model.SaveAscPushSubscription(ctx, 2, "http://push.example.net/x", "k", "a"))
	require.NoError(t, user_model.DeleteAscPushSubscriptionsOfUser(ctx, 2))
	subs, _ = user_model.ListAscPushSubscriptions(ctx, 2)
	assert.Empty(t, subs)
}
