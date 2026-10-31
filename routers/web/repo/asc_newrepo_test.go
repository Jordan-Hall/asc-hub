// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func checkName(t *testing.T, uid, name string) ascNewRepoNameAnswer {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "repo/check_name?uid="+uid+"&name="+name)
	contexttest.LoadUser(t, ctx, 2)
	ctx.IsSigned = true
	CheckName(ctx)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var a ascNewRepoNameAnswer
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &a))
	return a
}

func TestCheckNameAnswersFreeTakenAndReserved(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	assert.True(t, checkName(t, "2", "brand-new").OK)

	a := checkName(t, "2", "repo1")
	assert.False(t, a.OK)
	assert.NotEmpty(t, a.Message, "the taken name is said")

	a = checkName(t, "2", "..")
	assert.False(t, a.OK, "a reserved name")
	assert.NotEmpty(t, a.Message)

	a = checkName(t, "2", "bad%20name")
	assert.False(t, a.OK)

	a = checkName(t, "2", "")
	assert.False(t, a.OK)
	assert.Empty(t, a.Message, "nothing typed: nothing said")
}

func TestPrepareAscNewRepoQuotaLine(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "repo/create")
	contexttest.LoadUser(t, ctx, 2)
	u := &user_model.User{NumRepos: 3, MaxRepoCreation: 5}
	prepareAscNewRepo(ctx, u)
	q, ok := ctx.Data["AscNewRepoQuota"].(*AscNewRepoQuota)
	require.True(t, ok)
	assert.Equal(t, AscNewRepoQuota{Used: 3, Limit: 5}, *q)

	u.NumRepos = 5
	prepareAscNewRepo(ctx, u)
	assert.True(t, ctx.Data["AscNewRepoQuota"].(*AscNewRepoQuota).Reached)
}
