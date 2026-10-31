// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHideResolvedSwitchIsSavedPerPerson(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	save := func(userID int64, form url.Values) {
		ctx, resp := contexttest.MockContext(t, "POST user/settings/appearance/hide_resolved")
		contexttest.LoadUser(t, ctx, userID)
		ctx.IsSigned = true
		ctx.Req.Form, ctx.Req.PostForm = form, form
		UpdateUserHideResolved(ctx)
		require.Equal(t, http.StatusSeeOther, resp.Code)
	}
	get := func(userID int64) string {
		v, err := user_model.GetUserSetting(t.Context(), userID, user_model.SettingsKeyHideResolved)
		require.NoError(t, err)
		return v
	}
	assert.Equal(t, "", get(2), "no value until the person saves: resolved conversations are collapsed, as before")
	save(2, url.Values{})
	assert.Equal(t, "false", get(2), "an unchecked switch is off")
	save(2, url.Values{"hide_resolved": {"true"}})
	assert.Equal(t, "true", get(2))
	save(2, url.Values{})
	assert.Equal(t, "false", get(2))
	assert.Equal(t, "", get(1), "another person's choice is not touched")
}
