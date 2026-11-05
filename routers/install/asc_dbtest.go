// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package install

import (
	stdctx "context"
	"net/http"
	"strings"
	"sync"
	"time"

	"forgejo.org/models/db"
	db_install "forgejo.org/models/db/install"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// ascDbTestHeader must be on a database test request. A cross-site page cannot send a custom
// header without a CORS preflight, which the install server never answers.
const ascDbTestHeader = "X-Asc-Install"

// ascDbTestTimeout bounds one connection attempt, so a host that drops packets cannot hold the page.
const ascDbTestTimeout = 15 * time.Second

// ascDbTestMu lets one test run at a time: a test swaps the global database settings while it runs.
var ascDbTestMu sync.Mutex

// ascDbTestResult is the answer to POST /test-db.
type ascDbTestResult struct {
	OK      bool   `json:"ok"`
	Head    string `json:"head,omitempty"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Version string `json:"version,omitempty"`
}

// ascDbFailure names the form field to fix for a database error, and the locale key of the fix.
func ascDbFailure(err error, dbType string) (field, hintKey string) {
	if err == nil {
		return "", ""
	}
	msg := strings.ToLower(err.Error())
	has := func(parts ...string) bool {
		for _, p := range parts {
			if strings.Contains(msg, p) {
				return true
			}
		}
		return false
	}
	switch {
	case dbType == "sqlite3":
		return "db_path", "asc_install.db_hint_path"
	case has("password authentication failed", "access denied for user", "login failed for user"):
		return "db_passwd", "asc_install.db_hint_passwd"
	case has("role ") && has("does not exist"):
		return "db_user", "asc_install.db_hint_user"
	case has("unknown database") || (has("database ") && has("does not exist")):
		return "db_name", "asc_install.db_hint_name"
	case has("connection refused", "no such host", "i/o timeout", "deadline exceeded", "network is unreachable", "dial tcp", "cannot assign requested address"):
		return "db_host", "asc_install.db_hint_host"
	}
	return "", "asc_install.db_hint_other"
}

// ascDbTarget is what the error message says ASC Hub tried to reach.
func ascDbTarget(dbType, host, path string) string {
	if dbType == "sqlite3" {
		return path
	}
	return host
}

// TestDatabase connects with the database part of the install form and says what answered.
// No tables are written: the connection is closed and the earlier settings come back.
func TestDatabase(ctx *context.Context) {
	if setting.InstallLock {
		ctx.JSON(http.StatusConflict, ascDbTestResult{Message: ctx.Locale.TrString("asc_install.db_locked")})
		return
	}
	if ctx.Req.Header.Get(ascDbTestHeader) != "1" {
		ctx.JSON(http.StatusForbidden, ascDbTestResult{Message: ctx.Locale.TrString("asc_install.db_bad_request")})
		return
	}
	if !ascDbTestMu.TryLock() {
		ctx.JSON(http.StatusTooManyRequests, ascDbTestResult{Message: ctx.Locale.TrString("asc_install.db_busy")})
		return
	}
	defer ascDbTestMu.Unlock()

	dbType := ctx.Req.FormValue("db_type")
	known := false
	for _, t := range setting.SupportedDatabaseTypes {
		known = known || t == dbType
	}
	if !known {
		ctx.JSON(http.StatusBadRequest, ascDbTestResult{Message: ctx.Locale.TrString("asc_install.db_bad_request"), Field: "db_type"})
		return
	}

	saved := setting.Database
	defer func() {
		db.UnsetDefaultEngine()
		setting.Database = saved
	}()
	setting.Database.Type = setting.DatabaseType(dbType)
	setting.Database.Host = ctx.Req.FormValue("db_host")
	setting.Database.User = ctx.Req.FormValue("db_user")
	setting.Database.Passwd = ctx.Req.FormValue("db_passwd")
	setting.Database.Name = ctx.Req.FormValue("db_name")
	setting.Database.Schema = ctx.Req.FormValue("db_schema")
	setting.Database.SSLMode = ctx.Req.FormValue("ssl_mode")
	setting.Database.Path = ctx.Req.FormValue("db_path")

	fail := func(err error) {
		field, hint := ascDbFailure(err, dbType)
		ctx.JSON(http.StatusOK, ascDbTestResult{
			Head:    ctx.Locale.TrString("asc_install.db_fail_h"),
			Message: ctx.Locale.TrString("asc_install.db_fail", ascDbTarget(dbType, setting.Database.Host, setting.Database.Path), err),
			Field:   field,
			Hint:    ctx.Locale.TrString(hint),
		})
	}

	if dbType == "sqlite3" && setting.Database.Path == "" {
		ctx.JSON(http.StatusOK, ascDbTestResult{Message: ctx.Locale.TrString("install.err_empty_db_path"), Field: "db_path", Hint: ctx.Locale.TrString("asc_install.db_hint_path")})
		return
	}

	tctx, cancel := stdctx.WithTimeout(ctx, ascDbTestTimeout)
	defer cancel()
	db.UnsetDefaultEngine()
	if err := db.InitEngine(tctx); err != nil {
		fail(err)
		return
	}
	if err := db_install.CheckDatabaseConnection(); err != nil {
		fail(err)
		return
	}

	res := ascDbTestResult{OK: true, Message: ctx.Locale.TrString("asc_install.db_ok_nover", setting.DatabaseTypeNames[dbType])}
	if x, err := db.GetMasterEngine(db.GetEngine(db.DefaultContext)); err == nil {
		if v, err := x.DBVersion(); err == nil && v != nil && v.Number != "" {
			res.Version = v.Number
			res.Message = ctx.Locale.TrString("asc_install.db_ok", setting.DatabaseTypeNames[dbType], v.Number)
		}
	}
	ctx.JSON(http.StatusOK, res)
}

// ascDbFailed marks the database card as failed for the full page render after Install.
func ascDbFailed(ctx *context.Context, err error, dbType string) {
	field, hint := ascDbFailure(err, dbType)
	ctx.Data["Err_DbSetting"] = true
	ctx.Data["DbErrField"] = field
	ctx.Data["DbErrHint"] = ctx.Locale.TrString(hint)
}
