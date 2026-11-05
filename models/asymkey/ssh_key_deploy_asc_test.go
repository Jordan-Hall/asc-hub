// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asymkey

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The access page says who added each deploy key.
func TestAddDeployKeyByRecordsWhoAddedIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	const content = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHnbqnHNKh/Td/1O6t9Q8qdJmitCAApnvHImHV8TkptX ci@example.org"

	key, err := AddDeployKeyBy(db.DefaultContext, 1, 2, "ci", content, true)
	require.NoError(t, err)
	assert.EqualValues(t, 2, key.AddedByID)
	assert.True(t, key.IsReadOnly())

	stored := unittest.AssertExistsAndLoadBean(t, &DeployKey{ID: key.ID})
	assert.EqualValues(t, 2, stored.AddedByID)
	assert.False(t, stored.HasUsed, "a key that was only added has not been used")

	// the same key on a second repository by someone else is that person's
	other, err := AddDeployKeyBy(db.DefaultContext, 2, 1, "ci", content, true)
	require.NoError(t, err)
	assert.EqualValues(t, 1, other.AddedByID)
	assert.Equal(t, key.KeyID, other.KeyID)

	old, err := AddDeployKey(db.DefaultContext, 3, "old", content, true)
	require.NoError(t, err)
	assert.EqualValues(t, 0, old.AddedByID, "a key added without a person records none")
}
