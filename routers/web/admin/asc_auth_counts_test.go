// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"code.forgejo.org/go-chi/session"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthSourceCountsPeoplePerSource(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "admin/auths")
	contexttest.LoadUser(t, ctx, 1)
	before := authSourceCounts(ctx)
	require.NotNil(t, before)
	require.Positive(t, before[0], "the fixtures have people with local passwords")

	// two people move to a source, one of them an organization (which is no person)
	_, err := db.GetEngine(ctx).Exec("UPDATE `user` SET login_source = 77 WHERE id IN (2, 3)")
	require.NoError(t, err)
	org := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	require.True(t, org.IsOrganization())
	after := authSourceCounts(ctx)
	assert.EqualValues(t, 1, after[77], "only the person counts")
	assert.EqualValues(t, before[0]-1, after[0])
}

func TestAuthenticationsPageShowsTheLocalRowAndThePeopleOfEachSource(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, rec := contexttest.MockContext(t, "admin/auths")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	Authentications(ctx)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="auth-local"`, "the built-in password source is listed even when it is the only one")
	assert.NotContains(t, rec.Body.String(), `data-testid="auth-people"`)

	// with an external source the table has the local row and the people of the source
	_, err := db.GetEngine(ctx).Exec("INSERT INTO login_source (id, type, name, is_active, is_sync_enabled, cfg, created_unix, updated_unix) VALUES (88, 6, 'ext', 1, 0, '{}', 1, 1)")
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).Exec("UPDATE `user` SET login_source = 88 WHERE id = 2")
	require.NoError(t, err)
	ctx, rec = contexttest.MockContext(t, "admin/auths")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	Authentications(ctx)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="auth-local"`)
	assert.Contains(t, body, `data-testid="auth-people"`)
}

// The board states of AdminAuth: no extra sources is the setup state, and a source that people sign in with cannot be
// removed until they are moved; the page says how many and links to the accounts.
func TestAuthSourceRemoveIsBlockedWhilePeopleUseIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, rec := contexttest.MockContext(t, "admin/auths")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	Authentications(ctx)
	assert.Contains(t, rec.Body.String(), "asc_admin.auth_none_title", "no extra sources is the setup state")

	_, err := db.GetEngine(ctx).Exec("INSERT INTO login_source (id, type, name, is_active, is_sync_enabled, cfg, created_unix, updated_unix) VALUES (89, 3, 'mail', 1, 0, '{\"Host\":\"smtp.example.org\",\"Port\":587}', 1, 1)")
	require.NoError(t, err)
	edit := func() string {
		ctx, rec := contexttest.MockContext(t, "admin/auths/89")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, 1)
		ctx.IsSigned = true
		ctx.SetParams(":authid", "89")
		ctx.Session = &authEditSession{values: map[any]any{}}
		EditAuthSource(ctx)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec.Body.String()
	}

	body := edit()
	assert.Contains(t, body, `data-testid="auth-delete"`, "nobody uses the source, so it can be removed")
	assert.NotContains(t, body, `data-testid="auth-delete-blocked"`)

	_, err = db.GetEngine(ctx).Exec("UPDATE `user` SET login_source = 89 WHERE id IN (2, 4)")
	require.NoError(t, err)
	body = edit()
	assert.Contains(t, body, `data-testid="auth-delete-blocked"`)
	assert.NotContains(t, body, `data-testid="auth-delete"`)
	assert.Contains(t, body, `aria-describedby="auth-delete-blocked"`)
	assert.Contains(t, body, "asc_admin.auth_delete_blocked_", "the note says how many people use the source")
	assert.Contains(t, body, `href="/admin/users"`)
}

// authEditSession is a map-backed session store; the embedded interface is nil and only Get, Set and Delete are used.
type authEditSession struct {
	session.Store
	values map[any]any
}

func (s *authEditSession) Get(k any) any      { return s.values[k] }
func (s *authEditSession) Set(k, v any) error { s.values[k] = v; return nil }
func (s *authEditSession) Delete(k any) error { delete(s.values, k); return nil }
