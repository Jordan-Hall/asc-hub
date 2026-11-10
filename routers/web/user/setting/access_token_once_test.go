// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"testing"

	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mapSession is the three methods of a session the token card uses; the rest of the interface is unused.
type mapSession struct {
	session.Store
	values map[any]any
}

func (m *mapSession) Get(k any) any      { return m.values[k] }
func (m *mapSession) Set(k, v any) error { m.values[k] = v; return nil }
func (m *mapSession) Delete(k any) error { delete(m.values, k); return nil }

func TestNewAccessTokenShownOnce(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "user/settings/applications")
	ctx.Session = &mapSession{values: map[any]any{}}

	assert.Empty(t, takeNewAccessToken(ctx), "nothing was made yet")

	rememberNewAccessToken(ctx, "secret-token-value")
	assert.Empty(t, ctx.Flash.InfoMsg, "the token is not a flash message any more")

	assert.Equal(t, "secret-token-value", takeNewAccessToken(ctx))
	assert.Empty(t, takeNewAccessToken(ctx), "the second view does not show it again")
}

func TestSettingsOrgRows(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, _ := contexttest.MockContext(t, "user/settings/organization")
	contexttest.LoadUser(t, ctx, 2)

	org := unittest.AssertExistsAndLoadBean(t, &organization.Organization{ID: 3})
	rows, err := settingsOrgRows(ctx, []*organization.Organization{org}, ctx.Doer.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	owned, err := org.IsOwnedBy(ctx, ctx.Doer.ID)
	require.NoError(t, err)
	assert.Equal(t, owned, rows[0].IsOwner)
	owners, err := org.GetOwnerTeam(ctx)
	require.NoError(t, err)
	assert.Equal(t, owned && owners.NumMembers <= 1, rows[0].IsLastOwner)
}

// An owner's row links to the organization's settings; a member's row offers Leave.
func TestOrganizationPageRows(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, resp := contexttest.MockContext(t, "user/settings/organization")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	Organization(ctx)
	require.Equal(t, http.StatusOK, ctx.Resp.Status())

	rows := ctx.Data["OrgRows"].([]SettingsOrgRow)
	require.NotEmpty(t, rows)
	body := resp.Body.String()
	assert.Contains(t, body, "asc_work.sp_orgs_yours")
	owners := 0
	for _, r := range rows {
		if r.IsOwner {
			owners++
			assert.Contains(t, body, `href="`+r.Org.OrganisationLink()+`/settings"`)
			if r.IsLastOwner {
				assert.NotContains(t, body, `data-url="`+r.Org.OrganisationLink()+`/members/action/leave"`, "the last owner cannot leave")
			} else {
				assert.Contains(t, body, `data-url="`+r.Org.OrganisationLink()+`/members/action/leave"`, "an owner who is not the last can leave")
			}
		} else {
			assert.Contains(t, body, `data-url="`+r.Org.OrganisationLink()+`/members/action/leave"`)
		}
	}
	assert.Positive(t, owners, "user2 owns an organization in the fixtures")
	assert.Less(t, owners, len(rows), "user2 is only a member of another organization in the fixtures")
}

func TestInviteRole(t *testing.T) {
	assert.Equal(t, "owner", inviteRole(&organization.Team{Name: organization.OwnerTeamName, AccessMode: perm.AccessModeOwner}))
	assert.Equal(t, "maintainer", inviteRole(&organization.Team{LowerName: "maintainers", AccessMode: perm.AccessModeAdmin}))
	assert.Equal(t, "member", inviteRole(&organization.Team{LowerName: "members", AccessMode: perm.AccessModeWrite}))
}
