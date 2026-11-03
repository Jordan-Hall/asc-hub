// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package org_test

import (
	gocontext "context"
	"net"
	"net/http"
	"sync"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/org"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// fakeNewOrgAgents is asc serve for the agents switch of the new organization form.
type fakeNewOrgAgents struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedOrgSettingsServer

	mu  sync.Mutex
	set []*ascv1.SetOrgAgentPolicyRequest
}

func (f *fakeNewOrgAgents) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{asctrust.FeatureOrgSettings}}, nil
}

func (f *fakeNewOrgAgents) GetOrgSettings(gocontext.Context, *ascv1.GetOrgSettingsRequest) (*ascv1.GetOrgSettingsResponse, error) {
	return &ascv1.GetOrgSettingsResponse{
		Agents:               &ascv1.OrgAgentPolicy{AgentChangesetsNeedPerson: true, MonthlySpendCeilingMicroUsd: 20_000_000},
		InstanceAllowsAgents: true,
	}, nil
}

func (f *fakeNewOrgAgents) SetAgentPolicy(_ gocontext.Context, r *ascv1.SetOrgAgentPolicyRequest) (*ascv1.SetOrgAgentPolicyResponse, error) {
	f.mu.Lock()
	f.set = append(f.set, r)
	f.mu.Unlock()
	return &ascv1.SetOrgAgentPolicyResponse{Settings: &ascv1.GetOrgSettingsResponse{Agents: r.GetAgents()}}, nil
}

func startNewOrgAgents(t *testing.T) *fakeNewOrgAgents {
	t.Helper()
	f := &fakeNewOrgAgents{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterOrgSettingsServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

// createOrg posts the new organization form as user2.
func createOrg(t *testing.T, form forms.CreateOrgForm) (int, string, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "org/create")
	contexttest.LoadUser(t, ctx, 2)
	web.SetForm(ctx, &form)
	org.CreatePost(ctx)
	return resp.Code, ctx.Flash.SuccessMsg, ctx.Flash.WarningMsg
}

func newOrg(t *testing.T, name string) *organization.Organization {
	t.Helper()
	o, err := organization.GetOrgByName(db.DefaultContext, name)
	require.NoError(t, err)
	return o
}

func TestCreateOrgFirstTeamAndAgents(t *testing.T) {
	unittest.PrepareTestEnv(t)
	f := startNewOrgAgents(t)

	code, ok, warn := createOrg(t, forms.CreateOrgForm{OrgName: "nova-team", FullName: "  Nova Team ", CreateEveryone: true, AllowAgents: true})
	require.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "asc_work.neworg_created", ok, "the unit tests run without translations")
	assert.Empty(t, warn)
	o := newOrg(t, "nova-team")
	assert.Equal(t, "Nova Team", o.FullName)

	everyone, err := organization.GetTeam(db.DefaultContext, o.ID, "Everyone")
	require.NoError(t, err)
	assert.Equal(t, perm.AccessModeRead, everyone.AccessMode)
	assert.True(t, everyone.IncludesAllRepositories)
	require.NoError(t, everyone.LoadUnits(db.DefaultContext))
	assert.NotEmpty(t, everyone.Units)
	for _, u := range everyone.Units {
		assert.Equal(t, perm.AccessModeRead, u.AccessMode, "every unit is read only")
	}
	in, err := organization.IsTeamMember(db.DefaultContext, o.ID, everyone.ID, 2)
	require.NoError(t, err)
	assert.True(t, in, "the creator is in Everyone")

	require.Len(t, f.set, 1)
	assert.Equal(t, "nova-team", f.set[0].GetOrg())
	assert.True(t, f.set[0].GetAgents().GetAllowAgents())
	assert.True(t, f.set[0].GetAgents().GetAgentChangesetsNeedPerson(), "the rest of the policy is kept")
	assert.EqualValues(t, 20_000_000, f.set[0].GetAgents().GetMonthlySpendCeilingMicroUsd())
}

func TestCreateOrgWithoutExtras(t *testing.T) {
	unittest.PrepareTestEnv(t)
	f := startNewOrgAgents(t)

	code, _, warn := createOrg(t, forms.CreateOrgForm{OrgName: "orbit-team"})
	require.Equal(t, http.StatusSeeOther, code)
	assert.Empty(t, warn)
	o := newOrg(t, "orbit-team")
	_, err := organization.GetTeam(db.DefaultContext, o.ID, "Everyone")
	assert.True(t, organization.IsErrTeamNotExist(err), "no first team unless asked")
	assert.Empty(t, f.set, "agents are left as asc serve has them")
}

func TestCreateOrgAgentsFailureKeepsTheOrganization(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer asctrust.UseConn(nil)()

	code, ok, warn := createOrg(t, forms.CreateOrgForm{OrgName: "acme-team", AllowAgents: true})
	require.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "asc_work.neworg_created", ok)
	assert.Equal(t, "asc_work.neworg_agents_failed", warn)
	unittest.AssertExistsAndLoadBean(t, &user_model.User{LowerName: "acme-team", Type: user_model.UserTypeOrganization})
}
