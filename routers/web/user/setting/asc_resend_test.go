// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"
	"time"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMailResendSaysWhenToAskAgain(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "user/settings/account")
	now := time.Unix(1_700_000_000, 0)

	_, limited := mailResendRemaining(ctx, "maya", now)
	assert.False(t, limited, "nothing was sent yet")

	markMailResend(ctx, "maya", now)
	wait, limited := mailResendRemaining(ctx, "maya", now.Add(45*time.Second))
	require.True(t, limited)
	assert.Equal(t, 135*time.Second, wait)
	wait, _ = mailResendRemaining(ctx, "maya", now.Add(179*time.Second))
	assert.Equal(t, time.Second, wait, "never less than a second while the limit stands")
	wait, _ = mailResendRemaining(ctx, "maya", now.Add(-time.Hour))
	assert.Equal(t, 180*time.Second, wait, "a clock that went back does not make the wait longer than the limit")

	// another account is not limited by this one
	_, limited = mailResendRemaining(ctx, "bob", now)
	assert.False(t, limited)

	// a limit set without its time (by an older hub) is the whole wait
	require.NoError(t, ctx.Cache.Put(mailResendLimKey+"old", "old", mailResendWait))
	wait, limited = mailResendRemaining(ctx, "old", now)
	assert.True(t, limited)
	assert.Equal(t, 180*time.Second, wait)

	assert.NotEmpty(t, mailResendText(ctx, 45*time.Second))
	assert.NotEqual(t, mailResendText(ctx, 45*time.Second), mailResendText(ctx, 150*time.Second))
}
