// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"encoding/json"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func usernameCheck(t *testing.T, name string) ascUsernameAnswer {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "user/settings/username_check?name="+name)
	contexttest.LoadUser(t, ctx, 2) // user2
	ctx.IsSigned = true
	UsernameCheck(ctx)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var a ascUsernameAnswer
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &a))
	return a
}

func TestUsernameCheckAnswersFreeTakenOwnAndReserved(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	assert.True(t, usernameCheck(t, "a-brand-new-name").OK)
	assert.True(t, usernameCheck(t, "user2").OK, "the name the person has is theirs")

	a := usernameCheck(t, "user1")
	assert.False(t, a.OK)
	assert.NotEmpty(t, a.Message, "a name somebody has is said to be taken")
	assert.False(t, usernameCheck(t, "USER1").OK, "names are compared without case")

	a = usernameCheck(t, "admin")
	assert.False(t, a.OK, "a reserved name")
	assert.NotEmpty(t, a.Message)

	a = usernameCheck(t, "bad%20name")
	assert.False(t, a.OK)
	assert.NotEmpty(t, a.Message)

	a = usernameCheck(t, "")
	assert.False(t, a.OK)
	assert.Empty(t, a.Message, "nothing typed: nothing said")
}
