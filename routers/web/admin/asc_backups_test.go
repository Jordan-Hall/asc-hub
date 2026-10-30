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

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

const goodBackupID = "bk_abcdefghij0123456789"

type fakeBackups struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedBackupsServer

	mu       sync.Mutex
	features []string
	reqs     []any
}

func (f *fakeBackups) note(r any) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.mu.Unlock()
}

func (f *fakeBackups) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeBackups) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeBackups) ListBackups(_ gocontext.Context, r *ascv1.ListBackupsRequest) (*ascv1.ListBackupsResponse, error) {
	f.note(r)
	return &ascv1.ListBackupsResponse{
		Backups: []*ascv1.Backup{
			{Id: goodBackupID, State: ascv1.BackupState_BACKUP_STATE_SUCCEEDED, Trigger: ascv1.BackupTrigger_BACKUP_TRIGGER_SCHEDULE, Target: "nas", Started: 1790812800, Finished: 1790812900, SizeBytes: 5 << 20, Verify: ascv1.BackupVerify_BACKUP_VERIFY_VERIFIED, Restorable: true, Note: "Restore test <b>passed</b>"},
			{Id: "bk_zzzzzzzzzzzzzzzzzzzz", State: ascv1.BackupState_BACKUP_STATE_FAILED, Target: "nas", Started: 1790726400, Note: "Target unreachable: timeout"},
		},
		Schedule: &ascv1.BackupSchedule{Enabled: true, IntervalHours: 24, MinuteOfDay: 120, Target: "nas", KeepDays: 14, KeepWeeks: 8, IncludeObjects: true},
		Status:   &ascv1.BackupStatus{Configured: true, Overdue: true},
	}, nil
}

func (f *fakeBackups) ListTargets(_ gocontext.Context, r *ascv1.ListBackupTargetsRequest) (*ascv1.ListBackupTargetsResponse, error) {
	f.note(r)
	return &ascv1.ListBackupTargetsResponse{Targets: []*ascv1.BackupTarget{{Name: "nas", Kind: "local", Location: "/backups"}}}, nil
}

func (f *fakeBackups) RunBackup(_ gocontext.Context, r *ascv1.RunBackupRequest) (*ascv1.RunBackupResponse, error) {
	f.note(r)
	return &ascv1.RunBackupResponse{Backup: &ascv1.Backup{Id: goodBackupID, State: ascv1.BackupState_BACKUP_STATE_RUNNING}}, nil
}

func (f *fakeBackups) SetSchedule(_ gocontext.Context, r *ascv1.SetBackupScheduleRequest) (*ascv1.SetBackupScheduleResponse, error) {
	f.note(r)
	return &ascv1.SetBackupScheduleResponse{Schedule: r.GetSchedule()}, nil
}

func (f *fakeBackups) VerifyBackup(_ gocontext.Context, r *ascv1.VerifyBackupRequest) (*ascv1.VerifyBackupResponse, error) {
	f.note(r)
	return &ascv1.VerifyBackupResponse{Backup: &ascv1.Backup{Id: r.GetId(), State: ascv1.BackupState_BACKUP_STATE_SUCCEEDED, Verify: ascv1.BackupVerify_BACKUP_VERIFY_RUNNING}}, nil
}

func (f *fakeBackups) PreviewRestore(_ gocontext.Context, r *ascv1.PreviewRestoreRequest) (*ascv1.PreviewRestoreResponse, error) {
	f.note(r)
	return &ascv1.PreviewRestoreResponse{
		Backup: &ascv1.Backup{Id: r.GetId(), State: ascv1.BackupState_BACKUP_STATE_SUCCEEDED, Target: "nas", Started: 1790812800}, Confirmation: "RESTORE",
		LosesChangesSince: 1790812900, ReadOnlyDuring: true, Verified: true, Database: "sqlite", Objects: 12, ObjectBytes: 2048, Warnings: []string{"Keys <i>stored</i> by people need the vault key"},
	}, nil
}

func (f *fakeBackups) Restore(_ gocontext.Context, r *ascv1.RestoreBackupRequest) (*ascv1.RestoreBackupResponse, error) {
	f.note(r)
	return &ascv1.RestoreBackupResponse{BackupId: r.GetId(), RestartRequired: true}, nil
}

func startBackups(t *testing.T, features ...string) *fakeBackups {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	f := &fakeBackups{features: features}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterBackupsServer(srv, f)
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

type bkResp struct {
	status int
	body   string
	loc    string
	flash  string
}

func runBackups(t *testing.T, method string, user int64, form url.Values, id string, h func(*context.Context)) bkResp {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, method+" /admin/asc/backups")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, user)
	ctx.IsSigned = true
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	if form != nil {
		ctx.Req.Form, ctx.Req.PostForm = form, form
	}
	if id != "" {
		ctx.SetParams(":id", id)
	}
	h(ctx)
	r := bkResp{status: rec.Code, body: rec.Body.String(), loc: rec.Header().Get("Location")}
	if ctx.Flash != nil {
		r.flash = ctx.Flash.ErrorMsg + ctx.Flash.SuccessMsg
	}
	return r
}

func bkOp() string { return uuid.Must(uuid.NewV7()).String() }

func TestAscBackupsBoard(t *testing.T) {
	f := startBackups(t, asctrust.FeatureBackups)
	r := runBackups(t, "GET", 1, nil, "", Backups)
	require.Equal(t, http.StatusOK, r.status, r.body)
	for _, want := range []string{`data-testid="asc-backups"`, `data-testid="asc-backups-overdue"`, `data-testid="asc-backups-history"`, `value="02:00"`, `/admin/asc/backups/` + goodBackupID + `/restore`, "Target unreachable"} {
		assert.Contains(t, r.body, want)
	}
	assert.NotContains(t, r.body, "<b>passed</b>", "a note from the server is escaped")
	assert.Contains(t, r.body, "docker compose", "the instructions for the hub's own volume stay")

	g := startBackups(t)
	r = runBackups(t, "GET", 1, nil, "", Backups)
	assert.Contains(t, r.body, `data-testid="asc-backups-unsupported"`)
	assert.Zero(t, g.calls())

	// Not a site administrator: the board is not even asked for.
	n := f.calls()
	_ = runBackups(t, "GET", 2, nil, "", Backups)
	assert.Equal(t, n, f.calls())
}

func TestAscBackupsActions(t *testing.T) {
	f := startBackups(t, asctrust.FeatureBackups)

	r := runBackups(t, "POST", 1, url.Values{"op": {bkOp()}, "target": {"nas"}}, "", AscBackupsRun)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "nas", f.reqs[0].(*ascv1.RunBackupRequest).GetTarget())

	form := url.Values{"op": {bkOp()}, "enabled": {"1"}, "interval_hours": {"12"}, "time": {"03:30"}, "target": {"nas"}, "keep_days": {"30"}, "keep_weeks": {"0"}, "include_objects": {"1"}}
	r = runBackups(t, "POST", 1, form, "", AscBackupsSchedule)
	assert.Equal(t, http.StatusSeeOther, r.status)
	s := f.reqs[1].(*ascv1.SetBackupScheduleRequest).GetSchedule()
	assert.True(t, s.GetEnabled() && s.GetIncludeObjects())
	assert.False(t, s.GetIncludeVaultKey())
	assert.EqualValues(t, 12, s.GetIntervalHours())
	assert.EqualValues(t, 3*60+30, s.GetMinuteOfDay())

	r = runBackups(t, "POST", 1, url.Values{"op": {bkOp()}}, goodBackupID, AscBackupsVerify)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, goodBackupID, f.reqs[2].(*ascv1.VerifyBackupRequest).GetId())

	n := f.calls()
	for _, bad := range []url.Values{
		{"interval_hours": {"0"}, "time": {"03:30"}, "target": {"nas"}, "keep_days": {"30"}, "keep_weeks": {"0"}},
		{"interval_hours": {"169"}, "time": {"03:30"}, "target": {"nas"}, "keep_days": {"30"}, "keep_weeks": {"0"}},
		{"interval_hours": {"12"}, "time": {"25:00"}, "target": {"nas"}, "keep_days": {"30"}, "keep_weeks": {"0"}},
		{"interval_hours": {"12"}, "time": {"03:30"}, "target": {"../x"}, "keep_days": {"30"}, "keep_weeks": {"0"}},
		{"interval_hours": {"12"}, "time": {"03:30"}, "target": {"nas"}, "keep_days": {"0"}, "keep_weeks": {"0"}},
		{"interval_hours": {"12"}, "time": {"03:30"}, "target": {"nas"}, "keep_days": {"30"}, "keep_weeks": {"521"}},
	} {
		bad.Set("op", bkOp())
		r = runBackups(t, "POST", 1, bad, "", AscBackupsSchedule)
		assert.NotEmpty(t, r.flash, "%v", bad)
	}
	r = runBackups(t, "POST", 1, url.Values{"op": {bkOp()}}, "../etc", AscBackupsVerify)
	assert.NotEmpty(t, r.flash)
	r = runBackups(t, "POST", 1, url.Values{"op": {bkOp()}, "target": {"a/b"}}, "", AscBackupsRun)
	assert.NotEmpty(t, r.flash)
	assert.Equal(t, n, f.calls(), "a mistake of the form is refused before a call")

	// A person who is not a site administrator can do none of it.
	for name, h := range map[string]func(*context.Context){"run": AscBackupsRun, "schedule": AscBackupsSchedule, "verify": AscBackupsVerify, "restore": AscBackupsRestore, "form": AscBackupsRestoreForm} {
		r = runBackups(t, "POST", 2, form, goodBackupID, h)
		assert.Equal(t, http.StatusForbidden, r.status, name)
	}
	assert.Equal(t, n, f.calls())
}

func TestAscBackupsRestore(t *testing.T) {
	f := startBackups(t, asctrust.FeatureBackups)
	r := runBackups(t, "GET", 1, nil, goodBackupID, AscBackupsRestoreForm)
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Contains(t, r.body, `data-testid="asc-restore-warning"`)
	assert.Contains(t, r.body, `data-testid="asc-restore-form"`)
	assert.NotContains(t, r.body, "<i>stored</i>", "a warning from the server is escaped")

	n := f.calls()
	r = runBackups(t, "POST", 1, url.Values{"op": {bkOp()}, "confirm": {"restore"}}, goodBackupID, AscBackupsRestore)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/admin/asc/backups/"+goodBackupID+"/restore", r.loc, "a wrong word goes back to the form")
	assert.NotEmpty(t, r.flash)
	assert.Equal(t, n, f.calls(), "the word is checked before a call")

	r = runBackups(t, "POST", 1, url.Values{"op": {bkOp()}, "confirm": {"RESTORE"}}, goodBackupID, AscBackupsRestore)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/admin/asc/backups", r.loc)
	req := f.reqs[n].(*ascv1.RestoreBackupRequest)
	assert.Equal(t, goodBackupID, req.GetId())
	assert.Equal(t, "RESTORE", req.GetConfirm())

	r = runBackups(t, "GET", 1, nil, "../x", AscBackupsRestoreForm)
	assert.Equal(t, http.StatusNotFound, r.status)
}
