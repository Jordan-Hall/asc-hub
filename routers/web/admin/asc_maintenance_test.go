// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascmaintenance"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeMaintenance struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedMaintenanceServer

	mu       sync.Mutex
	features []string
	state    *ascv1.MaintenanceState
	sets     []*ascv1.SetMaintenanceRequest
	refuse   bool
}

func (f *fakeMaintenance) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeMaintenance) GetMaintenance(gocontext.Context, *ascv1.GetMaintenanceRequest) (*ascv1.GetMaintenanceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &ascv1.GetMaintenanceResponse{State: f.state}, nil
}

func (f *fakeMaintenance) SetMaintenance(_ gocontext.Context, r *ascv1.SetMaintenanceRequest) (*ascv1.SetMaintenanceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = append(f.sets, r)
	if f.refuse {
		return nil, status.Error(codes.InvalidArgument, "the version is not a version")
	}
	f.state = &ascv1.MaintenanceState{On: r.GetOn(), Message: r.GetMessage(), Until: r.GetUntil(), Version: r.GetVersion(), Updated: 1790812800, UpdatedBy: "1:user1"}
	return &ascv1.SetMaintenanceResponse{State: f.state}, nil
}

func (f *fakeMaintenance) setCalls() []*ascv1.SetMaintenanceRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*ascv1.SetMaintenanceRequest(nil), f.sets...)
}

func startMaintenance(t *testing.T, features ...string) *fakeMaintenance {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	ascmaintenance.Forget()
	t.Cleanup(ascmaintenance.Forget)
	f := &fakeMaintenance{features: features, state: &ascv1.MaintenanceState{}}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterMaintenanceServer(srv, f)
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

func maintenanceForm(on bool, version string) url.Values {
	v := url.Values{"message": {"moving to new disks"}, "version": {version}, "op": {bkOp()}, "on": {"0"}}
	if on {
		v["on"] = []string{"0", "1"}
	}
	return v
}

func TestAscMaintenanceBoard(t *testing.T) {
	f := startMaintenance(t, asctrust.FeatureMaintenance)
	f.state = &ascv1.MaintenanceState{On: true, Version: "2.4.0", Updated: 1790812800, UpdatedBy: "operator", RestoreStaged: true}
	r := runBackups(t, "GET", 1, nil, "", Maintenance)
	require.Equal(t, http.StatusOK, r.status, r.body)
	for _, want := range []string{`data-testid="asc-maintenance"`, `data-testid="asc-maintenance-restore"`, `>asc_admin.maintenance_asc_read_only<`, `>2.4.0<`, `>operator<`, `name="op"`, `name="version"`} {
		assert.Contains(t, r.body, want)
	}

	startMaintenance(t)
	r = runBackups(t, "GET", 1, nil, "", Maintenance)
	assert.Contains(t, r.body, `data-testid="asc-maintenance-unsupported"`)
	assert.NotContains(t, r.body, `name="op"`, "no switch of asc serve, no operation")

	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	r = runBackups(t, "GET", 1, nil, "", Maintenance)
	assert.NotContains(t, r.body, `data-testid="asc-maintenance`)
	assert.Contains(t, r.body, `name="version"`, "the hub's own switch keeps its version")
}

func TestAscMaintenanceSwitchesAscFirst(t *testing.T) {
	f := startMaintenance(t, asctrust.FeatureMaintenance)
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Minute)
	form := maintenanceForm(true, "2.4.0")
	form.Set("until", until.Format("2006-01-02T15:04"))
	r := runBackups(t, "POST", 1, form, "", MaintenancePost)
	require.Equal(t, http.StatusSeeOther, r.status, r.flash)
	assert.Equal(t, "settings.update_profile_success", r.flash)
	sets := f.setCalls()
	require.Len(t, sets, 1)
	assert.True(t, sets[0].GetOn())
	assert.Equal(t, "moving to new disks", sets[0].GetMessage())
	assert.Equal(t, "2.4.0", sets[0].GetVersion())
	assert.Equal(t, until.Unix(), sets[0].GetUntil())
	st := ascmaintenance.Load(t.Context())
	assert.True(t, st.On)
	assert.Equal(t, "2.4.0", st.Version)

	// asc serve refuses: the hub keeps what it had.
	f.refuse = true
	r = runBackups(t, "POST", 1, maintenanceForm(false, "2.4.0"), "", MaintenancePost)
	assert.NotEqual(t, "settings.update_profile_success", r.flash)
	assert.NotEmpty(t, r.flash)
	assert.True(t, ascmaintenance.Load(t.Context()).On, "a refusal changes nothing")
	f.refuse = false
	r = runBackups(t, "POST", 1, maintenanceForm(false, ""), "", MaintenancePost)
	assert.Equal(t, "settings.update_profile_success", r.flash)
	assert.False(t, ascmaintenance.Load(t.Context()).On)
	assert.Len(t, f.setCalls(), 3)
}

func TestAscMaintenanceRefusesBeforeCallingAsc(t *testing.T) {
	f := startMaintenance(t, asctrust.FeatureMaintenance)
	for name, form := range map[string]url.Values{
		"bad version": maintenanceForm(true, "2.4 beta"),
		"past":        func() url.Values { v := maintenanceForm(true, ""); v.Set("until", "2020-01-01T00:00"); return v }(),
		"bad time":    func() url.Values { v := maintenanceForm(true, ""); v.Set("until", "tomorrow"); return v }(),
		"control":     func() url.Values { v := maintenanceForm(true, ""); v.Set("message", "bell\a"); return v }(),
	} {
		r := runBackups(t, "POST", 1, form, "", MaintenancePost)
		assert.NotEqual(t, "settings.update_profile_success", r.flash, name)
	}
	assert.Empty(t, f.setCalls())
	assert.False(t, ascmaintenance.Load(t.Context()).On)
}

func TestAscMaintenanceNotLinked(t *testing.T) {
	f := startMaintenance(t)
	r := runBackups(t, "POST", 1, maintenanceForm(true, "2.4.0"), "", MaintenancePost)
	assert.Equal(t, "settings.update_profile_success", r.flash)
	assert.Empty(t, f.setCalls(), "a server without the service is not called")
	assert.True(t, ascmaintenance.Load(t.Context()).On)
}

func TestMaintenancePageShowsTheVersion(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, rec := contexttest.MockContext(t, "POST /user2/repo1/issues/new")
	ctx.Render = templates.HTMLRenderer()
	ctx.Data["CsrfTokenHtml"] = template.HTML("")
	ctx.Data["AscMaintenance"] = ascmaintenance.State{On: true, Message: "moving to new disks", Version: "2.4.0"}
	ctx.HTML(http.StatusServiceUnavailable, "status/503")
	body := rec.Body.String()
	assert.Contains(t, body, "asc_err.maintenance_version")
	assert.Contains(t, body, ">2.4.0<")
	assert.Contains(t, body, "moving to new disks")
}
