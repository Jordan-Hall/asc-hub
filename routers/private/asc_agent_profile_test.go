// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentProfileCall(t *testing.T, login string) (int, ascAgentProfileResponse, string) {
	t.Helper()
	body, err := json.Marshal(ascAgentProfileRequest{Login: login})
	require.NoError(t, err)
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/agent-profile")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(string(body)))
	AscAgentProfile(ctx)
	var out ascAgentProfileResponse
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return resp.Code, out, resp.Body.String()
}

func TestAscAgentProfileFollowsTheSwitchAndHidesWhoExists(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ctx := t.Context()
	_, err := db.GetEngine(ctx).Exec("UPDATE `user` SET full_name = 'User Two', description = 'bio', location = 'Lisbon' WHERE id = 2")
	require.NoError(t, err)

	status, out, _ := agentProfileCall(t, "user2")
	require.Equal(t, http.StatusOK, status)
	assert.True(t, out.Allowed)
	assert.Equal(t, "User Two", out.FullName)
	assert.Equal(t, "bio", out.Description)

	require.NoError(t, user_model.SetAscAgentsMayReadProfile(ctx, 2, false))
	_, off, offBody := agentProfileCall(t, "user2")
	assert.False(t, off.Allowed)

	// switched off, a private account and a name that does not exist answer with the same bytes
	_, err = db.GetEngine(ctx).Exec("UPDATE `user` SET visibility = ? WHERE id = 4", structs.VisibleTypePrivate)
	require.NoError(t, err)
	_, _, privBody := agentProfileCall(t, "user4")
	_, _, missingBody := agentProfileCall(t, "no_such_person_zz")
	assert.Equal(t, offBody, privBody)
	assert.Equal(t, offBody, missingBody)

	// the organization is not a person
	status, _, orgBody := agentProfileCall(t, "org3")
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, offBody, orgBody)

	status, _, _ = agentProfileCall(t, "")
	assert.Equal(t, http.StatusBadRequest, status)
	status, _, _ = agentProfileCall(t, "a/b")
	assert.Equal(t, http.StatusBadRequest, status)
}
