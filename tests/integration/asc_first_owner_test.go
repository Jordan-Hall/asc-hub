// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscFirstUserOwnsTheHub checks the skipped administrator of the install (board Install): on a hub with no
// account the first person to register becomes its administrator, and the next one does not.
func TestAscFirstUserOwnsTheHub(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.EnableCaptcha, false)()
	defer test.MockVariableValue(&setting.Service.RegisterEmailConfirm, false)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	require.NoError(t, db.TruncateBeansCascade(db.DefaultContext, user_model.User{}))
	_, err := db.GetEngine(db.DefaultContext).Exec("DELETE FROM `email_address`")
	require.NoError(t, err)
	require.EqualValues(t, 0, user_model.CountUsers(db.DefaultContext, nil))

	signUp := func(name string) *user_model.User {
		MakeRequest(t, NewRequestWithValues(t, "POST", "/user/sign_up", map[string]string{
			"user_name": name, "email": name + "@example.org", "password": "examplePassword!1", "agree": "on",
		}), http.StatusSeeOther)
		return unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: name})
	}

	owner := signUp("maya")
	assert.True(t, owner.IsAdmin, "the first account owns the hub")
	assert.True(t, owner.IsActive)

	next := signUp("sam")
	assert.False(t, next.IsAdmin)
}
