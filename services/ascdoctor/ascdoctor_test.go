// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdoctor

import (
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func goodReport() *Report {
	return &Report{
		Asc: "0.9.0", Git: "git version 2.47.1", OS: "linux", Arch: "x86_64",
		Checks: []Check{
			{Name: CheckGit, Level: LevelOk},
			{Name: CheckShim, Level: LevelWarn},
			{Name: CheckDaemon, Level: LevelInfo},
			{Name: "token", Level: LevelFail},
			{Name: "free-space", Level: LevelWarn},
		},
	}
}

func TestValidate(t *testing.T) {
	require.NoError(t, goodReport().Validate())
	bad := map[string]func(r *Report){
		"no asc version":    func(r *Report) { r.Asc = "" },
		"no os":             func(r *Report) { r.OS = "" },
		"markup in git":     func(r *Report) { r.Git = "<b>git</b>" },
		"a path in arch":    func(r *Report) { r.Arch = `C:\Users\x` },
		"a long version":    func(r *Report) { r.Asc = strings.Repeat("1", 81) },
		"no checks":         func(r *Report) { r.Checks = nil },
		"too many checks":   func(r *Report) { r.Checks = make([]Check, MaxChecks+1) },
		"an unknown level":  func(r *Report) { r.Checks[0].Level = "bad" },
		"a name with space": func(r *Report) { r.Checks[0].Name = "real git" },
		"an upper name":     func(r *Report) { r.Checks[0].Name = "Real" },
		"a long name":       func(r *Report) { r.Checks[0].Name = strings.Repeat("a", 41) },
	}
	for name, change := range bad {
		r := goodReport()
		change(r)
		assert.ErrorIs(t, r.Validate(), ErrBadReport, name)
	}
	var none *Report
	assert.ErrorIs(t, none.Validate(), ErrBadReport)
}

func TestLevelAndProblems(t *testing.T) {
	r := goodReport()
	assert.Equal(t, LevelOk, r.Level(CheckGit))
	assert.Equal(t, LevelWarn, r.Level(CheckShim))
	assert.Empty(t, r.Level("nothing"))
	assert.Equal(t, []Check{{"token", LevelFail}, {CheckShim, LevelWarn}, {"free-space", LevelWarn}}, r.Problems(), "failures first")
	var none *Report
	assert.Empty(t, none.Level(CheckGit))
	assert.Empty(t, none.Problems())
}

func TestSaveLoad(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	got, err := Load(ctx, 2)
	require.NoError(t, err)
	assert.Nil(t, got, "no report yet")

	now := time.Date(2026, 11, 9, 10, 30, 15, 500, time.UTC)
	require.NoError(t, Save(ctx, 2, goodReport(), now))
	got, err = Load(ctx, 2)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "git version 2.47.1", got.Git)
	assert.Len(t, got.Checks, 5)
	assert.True(t, got.Reported.Equal(now.Truncate(time.Second)))
	other, err := Load(ctx, 3)
	require.NoError(t, err)
	assert.Nil(t, other, "the report of another person")

	r := goodReport()
	r.OS = ""
	require.ErrorIs(t, Save(ctx, 2, r, now), ErrBadReport)
	got, _ = Load(ctx, 2)
	assert.Equal(t, "linux", got.OS, "a refused report changes nothing")

	// a stored value that no longer reads is no report
	require.NoError(t, user_model.SetUserSetting(ctx, 2, SettingKey, "{not json"))
	got, err = Load(ctx, 2)
	require.NoError(t, err)
	assert.Nil(t, got)
}
