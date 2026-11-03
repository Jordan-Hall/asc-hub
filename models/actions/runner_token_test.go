// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetLatestRunnerToken(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token := unittest.AssertExistsAndLoadBean(t, &ActionRunnerToken{ID: 3})
	expectedToken, err := GetLatestRunnerToken(db.DefaultContext, optional.Some[int64](1), optional.None[int64]())
	require.NoError(t, err)
	assert.Equal(t, expectedToken, token)
}

func TestNewRunnerToken(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token, err := NewRunnerToken(db.DefaultContext, optional.Some[int64](1), optional.None[int64]())
	require.NoError(t, err)
	expectedToken, err := GetLatestRunnerToken(db.DefaultContext, optional.Some[int64](1), optional.None[int64]())
	require.NoError(t, err)
	assert.Equal(t, expectedToken, token)
}

func TestUpdateRunnerToken(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	token := unittest.AssertExistsAndLoadBean(t, &ActionRunnerToken{ID: 3})
	token.IsActive = true
	require.NoError(t, UpdateRunnerToken(db.DefaultContext, token, "is_active"))
	expectedToken, err := GetLatestRunnerToken(db.DefaultContext, optional.Some[int64](1), optional.None[int64]())
	require.NoError(t, err)
	assert.Equal(t, expectedToken, token)
}

func TestRunnerTokenExpiry(t *testing.T) {
	now := timeutil.TimeStampNow()
	assert.False(t, (&ActionRunnerToken{IsActive: true}).IsExpired(), "no expiry never expires")
	assert.True(t, (&ActionRunnerToken{IsActive: true}).Usable())
	assert.False(t, (&ActionRunnerToken{IsActive: true, ExpiresUnix: now + 60}).IsExpired())
	assert.True(t, (&ActionRunnerToken{IsActive: true, ExpiresUnix: now}).IsExpired())
	assert.False(t, (&ActionRunnerToken{IsActive: true, ExpiresUnix: now - 1}).Usable())
	assert.False(t, (&ActionRunnerToken{IsActive: false}).Usable())
}

func TestShownOnceRunnerToken(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	owner := optional.Some[int64](1)
	before := timeutil.TimeStampNow()
	token, err := NewShownOnceRunnerToken(db.DefaultContext, owner, optional.None[int64]())
	require.NoError(t, err)
	assert.True(t, token.Usable())
	assert.GreaterOrEqual(t, int64(token.ExpiresUnix), int64(before.AddDuration(ShownOnceRunnerTokenLifetime)))

	got, err := GetRunnerTokenByID(db.DefaultContext, token.ID)
	require.NoError(t, err)
	assert.Equal(t, token.Token, got.Token)
	assert.Equal(t, token.ExpiresUnix, got.ExpiresUnix)
	_, err = GetRunnerTokenByID(db.DefaultContext, 999999)
	require.ErrorIs(t, err, util.ErrNotExist)

	// the latest token still registers runners, so the API gives it
	usable, err := UsableRunnerToken(db.DefaultContext, owner, optional.None[int64]())
	require.NoError(t, err)
	assert.Equal(t, token.ID, usable.ID)

	// once it expired, the API gives a new one without expiry
	token.ExpiresUnix = before - 1
	require.NoError(t, UpdateRunnerToken(db.DefaultContext, token, "expires_unix"))
	usable, err = UsableRunnerToken(db.DefaultContext, owner, optional.None[int64]())
	require.NoError(t, err)
	assert.NotEqual(t, token.ID, usable.ID)
	assert.Zero(t, usable.ExpiresUnix)
	assert.True(t, usable.Usable())
	old, err := GetRunnerTokenByID(db.DefaultContext, token.ID)
	require.NoError(t, err)
	assert.False(t, old.IsActive, "a new token turns off the ones before it")
}
