// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockAs submits the block-by-username form of the Privacy page as user 2 and returns the answer a person sees.
func blockAs(t *testing.T, name string) (status int, location, flash string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "POST /user/settings/privacy/block")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Form = url.Values{"username": {name}}
	PrivacyBlock(ctx)
	return resp.Code, resp.Header().Get("Location"), ctx.Flash.ErrorMsg
}

func TestPrivacyBlockDoesNotRevealHiddenAccounts(t *testing.T) {
	unittest.PrepareTestEnv(t)

	var hidden []*user_model.User
	require.NoError(t, db.GetEngine(t.Context()).Where("visibility = ? AND type = ? AND id != 2", structs.VisibleTypePrivate, user_model.UserTypeIndividual).Find(&hidden))
	require.NotEmpty(t, hidden, "the fixtures have a private account")
	var org user_model.User
	has, err := db.GetEngine(t.Context()).Where("type = ?", user_model.UserTypeOrganization).Get(&org)
	require.NoError(t, err)
	require.True(t, has)

	missingStatus, missingLocation, missingFlash := blockAs(t, "no_such_person_zz")
	assert.NotEmpty(t, missingFlash)

	// a private account, an organization and a name that does not exist all get the same answer (with the typed name)
	for _, name := range []string{hidden[0].Name, org.Name} {
		status, location, flash := blockAs(t, name)
		assert.Equal(t, missingStatus, status, name)
		assert.Equal(t, missingLocation, location, name)
		assert.Equal(t, strings.ReplaceAll(missingFlash, "no_such_person_zz", name), flash, name)
	}
	require.NoError(t, err)
	assert.False(t, user_model.IsBlocked(t.Context(), 2, hidden[0].ID), "nothing was blocked")
}
