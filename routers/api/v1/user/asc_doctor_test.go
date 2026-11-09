// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"
	"forgejo.org/modules/web"
	"forgejo.org/services/ascdoctor"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postAscDoctor(t *testing.T, form *api.AscDoctorReport) int {
	t.Helper()
	ctx, resp := contexttest.MockAPIContext(t, "POST /api/v1/user/asc/doctor")
	contexttest.LoadUser(t, ctx, 2)
	web.SetForm(ctx, form)
	ReportAscDoctor(ctx)
	return resp.Code
}

func TestReportAscDoctor(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	good := func() *api.AscDoctorReport {
		return &api.AscDoctorReport{
			Asc: "0.9.0", Git: "git version 2.47.1", OS: "windows", Arch: "x86_64",
			Checks: []api.AscDoctorCheck{{Name: "real-git", Level: "ok"}, {Name: "shim-dir", Level: "warn"}},
		}
	}

	assert.Equal(t, http.StatusNoContent, postAscDoctor(t, good()))
	got, err := ascdoctor.Load(db.DefaultContext, 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "windows", got.OS)
	assert.Equal(t, "warn", got.Level("shim-dir"))
	assert.False(t, got.Reported.IsZero())

	bad := good()
	bad.Checks[0].Level = "great"
	assert.Equal(t, http.StatusUnprocessableEntity, postAscDoctor(t, bad))
	got, _ = ascdoctor.Load(db.DefaultContext, 2)
	assert.Equal(t, "ok", got.Level("real-git"), "a refused report keeps the last one")

	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	assert.Equal(t, http.StatusNotFound, postAscDoctor(t, good()))
}
