// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user_test

import (
	"testing"

	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
)

func TestIsAscAgent(t *testing.T) {
	assert.True(t, (&user_model.User{ID: 7, Type: user_model.UserTypeBot}).IsAscAgent())
	assert.False(t, (&user_model.User{ID: 7, Type: user_model.UserTypeIndividual}).IsAscAgent())
	assert.False(t, (&user_model.User{ID: 7, Type: user_model.UserTypeOrganization}).IsAscAgent())
	assert.False(t, user_model.NewGhostUser().IsAscAgent())
	assert.False(t, user_model.NewActionsUser().IsAscAgent())
	var none *user_model.User
	assert.False(t, none.IsAscAgent())
}
