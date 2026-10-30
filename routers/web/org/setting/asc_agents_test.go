// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var orgForwardedKey = []byte("0123456789abcdef0123456789abcdef")

const orgToken = "internal-token-for-tests"

type fakeOrgSettings struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedOrgSettingsServer

	mu       sync.Mutex
	features []string
	md       []metadata.MD
	reqs     []any
	settings *ascv1.GetOrgSettingsResponse
	agents   []*ascv1.OrgAgent
}

func (f *fakeOrgSettings) note(ctx gocontext.Context, req any) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.md, f.reqs = append(f.md, md), append(f.reqs, req)
	f.mu.Unlock()
}

func (f *fakeOrgSettings) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeOrgSettings) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeOrgSettings) GetOrgSettings(ctx gocontext.Context, r *ascv1.GetOrgSettingsRequest) (*ascv1.GetOrgSettingsResponse, error) {
	f.note(ctx, r)
	return f.settings, nil
}

func (f *fakeOrgSettings) ListAgents(ctx gocontext.Context, r *ascv1.ListOrgAgentsRequest) (*ascv1.ListOrgAgentsResponse, error) {
	f.note(ctx, r)
	return &ascv1.ListOrgAgentsResponse{Agents: f.agents}, nil
}

func (f *fakeOrgSettings) SetAgentPolicy(ctx gocontext.Context, r *ascv1.SetOrgAgentPolicyRequest) (*ascv1.SetOrgAgentPolicyResponse, error) {
	f.note(ctx, r)
	return &ascv1.SetOrgAgentPolicyResponse{Settings: f.settings}, nil
}

func (f *fakeOrgSettings) SetMergeDefaults(ctx gocontext.Context, r *ascv1.SetOrgMergeDefaultsRequest) (*ascv1.SetOrgMergeDefaultsResponse, error) {
	f.note(ctx, r)
	return &ascv1.SetOrgMergeDefaultsResponse{Settings: f.settings}, nil
}

func (f *fakeOrgSettings) CreateAgent(ctx gocontext.Context, r *ascv1.CreateOrgAgentRequest) (*ascv1.CreateOrgAgentResponse, error) {
	f.note(ctx, r)
	return &ascv1.CreateOrgAgentResponse{Agent: r.GetAgent()}, nil
}

func (f *fakeOrgSettings) UpdateAgent(ctx gocontext.Context, r *ascv1.UpdateOrgAgentRequest) (*ascv1.UpdateOrgAgentResponse, error) {
	f.note(ctx, r)
	return &ascv1.UpdateOrgAgentResponse{Agent: &ascv1.OrgAgent{Name: r.GetName()}}, nil
}

func (f *fakeOrgSettings) DeleteAgent(ctx gocontext.Context, r *ascv1.DeleteOrgAgentRequest) (*ascv1.DeleteOrgAgentResponse, error) {
	f.note(ctx, r)
	return &ascv1.DeleteOrgAgentResponse{}, nil
}

func startOrgSettings(t *testing.T, features ...string) *fakeOrgSettings {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeOrgSettings{
		features: features,
		settings: &ascv1.GetOrgSettingsResponse{
			Agents:                        &ascv1.OrgAgentPolicy{AllowAgents: true, AgentChangesetsNeedPerson: true, MonthlySpendCeilingMicroUsd: 50_000_000},
			Merge:                         &ascv1.MergeDefaults{RequireApprovals: 2, RequireChecks: []string{"build", "lint"}, Strategy: "rebase", Set: true, RequireSigned: true},
			InstanceAllowsAgents:          true,
			EffectiveSpendCeilingMicroUsd: 50_000_000,
		},
		agents: []*ascv1.OrgAgent{{Name: "release-bot", DisplayName: "Release <b>bot</b>", RunBy: "alice", Paths: []string{"docs/**"}, State: ascv1.OrgAgentState_ORG_AGENT_STATE_PAUSED}},
	}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterOrgSettingsServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: orgToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, orgForwardedKey))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

type orgResp struct {
	status int
	body   string
	loc    string
	flash  string
}

// runOrg runs a handler for org3 as user (2 owns org3, 4 does not).
func runOrg(t *testing.T, method, target string, user int64, owner bool, form url.Values, params map[string]string, h func(*context.Context)) orgResp {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, method+" "+target)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, user)
	ctx.IsSigned = true
	contexttest.LoadOrganization(t, ctx, 3)
	ctx.Org.IsOwner = owner
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	if form != nil {
		ctx.Req.Form, ctx.Req.PostForm = form, form
	}
	for k, v := range params {
		ctx.SetParams(":"+k, v)
	}
	h(ctx)
	r := orgResp{status: rec.Code, body: rec.Body.String(), loc: rec.Header().Get("Location")}
	if ctx.Flash != nil {
		r.flash = ctx.Flash.ErrorMsg + ctx.Flash.SuccessMsg
	}
	return r
}

func manyLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("check-" + strings.Repeat("x", i%5+1) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\n")
	}
	return b.String()
}

func opOf() string { return uuid.Must(uuid.NewV7()).String() }

func (f *fakeOrgSettings) assertOwnerClaim(t *testing.T, i int) {
	t.Helper()
	f.mu.Lock()
	md := f.md[i]
	f.mu.Unlock()
	assert.Equal(t, []string{"Bearer " + orgToken}, md.Get("authorization"))
	user, role, err := ascidentity.Verify(orgForwardedKey, func(k string) string { return strings.Join(md.Get(k), ",") }, "org:org3", time.Now())
	require.NoError(t, err, "the claim is signed for the organization")
	assert.Equal(t, "2:user2", user)
	assert.Equal(t, ascidentity.RoleAdmin, role)
}

func TestAscAgentsPage(t *testing.T) {
	f := startOrgSettings(t, asctrust.FeatureOrgSettings)
	r := runOrg(t, "GET", "/org/org3/settings/asc/agents", 2, true, nil, nil, AscAgents)
	require.Equal(t, http.StatusOK, r.status, r.body)
	for _, want := range []string{`data-testid="asc-org-agents-policy"`, `data-testid="asc-org-agents-list"`, "agent:release-bot", `data-state="paused"`, `value="50"`, `data-testid="asc-org-agents-new"`} {
		assert.Contains(t, r.body, want)
	}
	assert.NotContains(t, r.body, "<b>bot</b>", "a name from the server is escaped")
	assert.Equal(t, "org3", f.reqs[0].(*ascv1.GetOrgSettingsRequest).GetOrg())
	f.assertOwnerClaim(t, 0)
}

func TestAscAgentsPageFailsClosed(t *testing.T) {
	f := startOrgSettings(t, asctrust.FeatureOrgSettings)
	r := runOrg(t, "GET", "/x", 4, false, nil, nil, AscAgents)
	assert.Equal(t, http.StatusForbidden, r.status, "a member who is not an owner")
	assert.Zero(t, f.calls())
	for name, h := range map[string]func(*context.Context){"policy": AscAgentsPolicy, "new": AscAgentsNew, "update": AscAgentsUpdate, "state": AscAgentsState, "delete": AscAgentsDelete, "merge": AscMergeSave} {
		r = runOrg(t, "POST", "/x", 4, false, url.Values{"op": {opOf()}}, map[string]string{"name": "a"}, h)
		assert.Equal(t, http.StatusForbidden, r.status, name)
	}
	assert.Zero(t, f.calls())

	g := startOrgSettings(t)
	r = runOrg(t, "GET", "/x", 2, true, nil, nil, AscAgents)
	assert.Contains(t, r.body, `data-testid="asc-org-agents-unsupported"`)
	assert.Zero(t, g.calls())
}

func TestAscAgentsChanges(t *testing.T) {
	f := startOrgSettings(t, asctrust.FeatureOrgSettings)

	r := runOrg(t, "POST", "/x", 2, true, url.Values{"op": {opOf()}, "allow_agents": {"1"}, "ceiling_usd": {"75.5"}}, nil, AscAgentsPolicy)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/org/org3/settings/asc/agents", r.loc)
	p := f.reqs[0].(*ascv1.SetOrgAgentPolicyRequest)
	assert.True(t, p.GetAgents().GetAllowAgents())
	assert.False(t, p.GetAgents().GetAgentChangesetsNeedPerson(), "an unchecked switch is off")
	assert.EqualValues(t, 75_500_000, p.GetAgents().GetMonthlySpendCeilingMicroUsd())
	f.assertOwnerClaim(t, 0)

	r = runOrg(t, "POST", "/x", 2, true, url.Values{"op": {opOf()}, "name": {"docs-bot"}, "display_name": {"Docs"}, "run_by": {"alice"}, "paths": {"docs/**\r\n docs/**\nREADME.md"}, "repos": {"org3/repo3"}}, nil, AscAgentsNew)
	assert.Equal(t, http.StatusSeeOther, r.status)
	c := f.reqs[1].(*ascv1.CreateOrgAgentRequest)
	assert.Equal(t, []string{"docs/**", "README.md"}, c.GetAgent().GetPaths(), "lines are trimmed and de-duplicated")
	assert.Equal(t, []string{"org3/repo3"}, c.GetAgent().GetRepos())

	r = runOrg(t, "POST", "/x", 2, true, url.Values{"op": {opOf()}, "state": {"active"}}, map[string]string{"name": "release-bot"}, AscAgentsState)
	assert.Equal(t, http.StatusSeeOther, r.status)
	u := f.reqs[2].(*ascv1.UpdateOrgAgentRequest)
	assert.Equal(t, ascv1.OrgAgentState_ORG_AGENT_STATE_ACTIVE, u.GetState())
	assert.False(t, u.GetReplacePaths(), "a state change leaves the lists alone")

	r = runOrg(t, "POST", "/x", 2, true, url.Values{"op": {opOf()}, "display_name": {"New"}, "paths": {""}, "repos": {""}}, map[string]string{"name": "release-bot"}, AscAgentsUpdate)
	assert.Equal(t, http.StatusSeeOther, r.status)
	u = f.reqs[3].(*ascv1.UpdateOrgAgentRequest)
	assert.True(t, u.GetReplacePaths() && u.GetReplaceRepos(), "an edit replaces the lists, an empty one clears it")

	r = runOrg(t, "POST", "/x", 2, true, url.Values{"op": {opOf()}}, map[string]string{"name": "release-bot"}, AscAgentsDelete)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "release-bot", f.reqs[4].(*ascv1.DeleteOrgAgentRequest).GetName())

	n := f.calls()
	for _, bad := range []struct {
		h    func(*context.Context)
		form url.Values
		p    map[string]string
	}{
		{AscAgentsNew, url.Values{"op": {opOf()}, "name": {"Bad Name"}}, nil},
		{AscAgentsNew, url.Values{"op": {opOf()}, "name": {"ok"}, "repos": {"not-a-repo"}}, nil},
		{AscAgentsNew, url.Values{"op": {opOf()}, "name": {"ok"}, "run_by": {"a/b"}}, nil},
		{AscAgentsPolicy, url.Values{"op": {opOf()}, "ceiling_usd": {"lots"}}, nil},
		{AscAgentsState, url.Values{"op": {opOf()}, "state": {"deleted"}}, map[string]string{"name": "a"}},
		{AscAgentsDelete, url.Values{"op": {opOf()}}, map[string]string{"name": "../x"}},
		{AscAgentsDelete, url.Values{"op": {"nope"}}, map[string]string{"name": "ok"}},
	} {
		r = runOrg(t, "POST", "/x", 2, true, bad.form, bad.p, bad.h)
		assert.Equal(t, http.StatusSeeOther, r.status)
		assert.NotEmpty(t, r.flash, "%v", bad.form)
	}
	assert.Equal(t, n, f.calls(), "a mistake of the form is refused before a call")
}

func TestAscMergeDefaults(t *testing.T) {
	f := startOrgSettings(t, asctrust.FeatureOrgSettings)
	r := runOrg(t, "GET", "/org/org3/settings/asc/merge", 2, true, nil, nil, AscMerge)
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Contains(t, r.body, `value="2"`)
	assert.Contains(t, r.body, "build\nlint")
	assert.Contains(t, r.body, `<option value="rebase" selected>`)
	assert.Contains(t, r.body, `name="require_signed" value="1" checked`)

	form := url.Values{"op": {opOf()}, "require_approvals": {"3"}, "require_checks": {"build\r\nlint\r\n"}, "strategy": {"fast-forward"}, "queue": {"1"}, "human_approval": {"1"}}
	r = runOrg(t, "POST", "/x", 2, true, form, nil, AscMergeSave)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/org/org3/settings/asc/merge", r.loc)
	m := f.reqs[1].(*ascv1.SetOrgMergeDefaultsRequest).GetMerge()
	assert.EqualValues(t, 3, m.GetRequireApprovals())
	assert.Equal(t, []string{"build", "lint"}, m.GetRequireChecks())
	assert.Equal(t, "fast-forward", m.GetStrategy())
	assert.True(t, m.GetQueue() && m.GetHumanApproval())
	assert.False(t, m.GetRequireSigned() || m.GetAllowForce(), "what is not checked is off")

	n := f.calls()
	for _, bad := range []url.Values{
		{"op": {opOf()}, "strategy": {"squash"}},
		{"op": {opOf()}, "require_approvals": {"21"}},
		{"op": {opOf()}, "require_checks": {manyLines(33)}},
		{"op": {opOf()}, "require_checks": {strings.Repeat("c", 101)}},
	} {
		r = runOrg(t, "POST", "/x", 2, true, bad, nil, AscMergeSave)
		assert.NotEmpty(t, r.flash, "%v", bad)
		assert.Equal(t, n, f.calls(), "refused before a call: %.60v", bad)
	}
}
