// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"fmt"
	"net/http"
	"strconv"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	system_model "forgejo.org/models/system"
	"forgejo.org/modules/base"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// The checks of the health page (board AdminHealth). Each check is one step with an id. The page runs every step when it
// is loaded. "Run again" runs the steps again one after the other from the page script (GET /admin/self_check/checks/{id}),
// so each row changes when its own check is done. A check that could not ask its question (the database did not answer
// the count of runners, for example) is "could not run": it did not pass and it did not fail.

const tplSelfCheckRow base.TplName = "admin/self_check_row"

// Check states. checkNoRun is a check that could not be made.
const (
	checkOK    = "ok"
	checkWarn  = "warn"
	checkErr   = "err"
	checkNoRun = "norun"
)

// healthCheck is one step of the self check. run returns false when the check does not apply to this hub.
type healthCheck struct {
	ID  string
	run func(ctx *context.Context) (checkRow, bool)
}

// healthChecks are the steps, in the order of the page.
func healthChecks() []healthCheck {
	list := []healthCheck{
		{"database", checkDatabase},
		{"collation", checkCollation},
		{"cache", checkCache},
	}
	if setting.Actions.Enabled {
		list = append(list, healthCheck{"runners", checkRunners})
	}
	return append(list, healthCheck{"notices", checkNotices})
}

// runHealthCheck runs one step and gives its row an id and a label.
func runHealthCheck(ctx *context.Context, c healthCheck) (checkRow, bool) {
	r, ok := c.run(ctx)
	if !ok {
		return r, false
	}
	r.ID = c.ID
	switch r.State {
	case checkOK:
		r.Label = ctx.Tr("asc_admin.check_passed")
	case checkWarn:
		r.Label = ctx.Tr("asc_admin.check_warning")
	case checkNoRun:
		r.Label = ctx.Tr("asc_admin.check_norun")
	default:
		r.State = checkErr
		r.Label = ctx.Tr("asc_admin.check_failed")
	}
	return r, true
}

// checkTally is the summary chip of the checks: "3 passed, 1 warnings, 0 failed", and how many could not run when any.
func checkTally(ctx *context.Context, ok, warn, fail, norun int) string {
	if norun > 0 {
		return ctx.Locale.TrString("asc_admin.check_tally_norun", strconv.Itoa(ok), strconv.Itoa(warn), strconv.Itoa(fail), strconv.Itoa(norun))
	}
	return ctx.Locale.TrString("asc_admin.check_tally", strconv.Itoa(ok), strconv.Itoa(warn), strconv.Itoa(fail))
}

func checkDatabase(ctx *context.Context) (checkRow, bool) {
	name := ctx.Tr("asc_admin.check_db")
	if err := db.GetEngine(ctx).Ping(); err != nil {
		return checkRow{State: checkErr, Name: name, Text: esc(err.Error())}, true
	}
	return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_db_ok", string(setting.Database.Type))}, true
}

// collationFacts asks the database for its collations and puts what is wrong in the page data, for the "Database
// problems" card. It returns false, with no data, when the database has no collation to check.
func collationFacts(ctx *context.Context) (bool, error) {
	r, err := db.CheckCollationsDefaultEngine()
	if err != nil || r == nil {
		return false, err
	}
	ctx.Data["DatabaseType"] = setting.Database.Type
	ctx.Data["DatabaseCheckResult"] = r
	mismatch := !r.CollationEquals(r.DatabaseCollation, r.ExpectedCollation)
	insensitive := !r.IsCollationCaseSensitive(r.DatabaseCollation)
	ctx.Data["DatabaseCheckCollationMismatch"] = mismatch
	ctx.Data["DatabaseCheckCollationCaseInsensitive"] = insensitive
	ctx.Data["DatabaseCheckInconsistentCollationColumns"] = r.InconsistentCollationColumns
	ctx.Data["DatabaseCheckHasProblems"] = mismatch || insensitive || len(r.InconsistentCollationColumns) > 0
	return true, nil
}

func checkCollation(ctx *context.Context) (checkRow, bool) {
	name := ctx.Tr("asc_admin.check_collation")
	has, err := collationFacts(ctx)
	switch {
	case err != nil:
		return checkRow{State: checkNoRun, Name: name, Text: esc(err.Error())}, true
	case !has:
		return checkRow{}, false
	case ctx.Data["DatabaseCheckCollationMismatch"] == true || len(inconsistentColumns(ctx)) > 0:
		return checkRow{State: checkErr, Name: name, Text: ctx.Tr("asc_admin.check_collation_bad")}, true
	case ctx.Data["DatabaseCheckCollationCaseInsensitive"] == true:
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.check_collation_ci")}, true
	}
	return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_collation_ok")}, true
}

func checkCache(ctx *context.Context) (checkRow, bool) {
	name, fix := ctx.Tr("asc_admin.sys_cache"), setting.AppSubURL+"/admin/config"
	elapsed, err := cache.Test()
	switch {
	case err != nil:
		return checkRow{State: checkErr, Name: name, Text: esc(err.Error()), Fix: fix}, true
	case elapsed > cache.SlowCacheThreshold:
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.sys_cache_slow", fmt.Sprint(elapsed)), Fix: fix}, true
	}
	return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_cache_ok")}, true
}

func checkRunners(ctx *context.Context) (checkRow, bool) {
	name, fix := ctx.Tr("asc_admin.sys_runners"), setting.AppSubURL+"/admin/actions/runners"
	total, err := db.Count[actions_model.ActionRunner](ctx, actions_model.FindRunnerOptions{WithVisible: true})
	if err != nil {
		return checkRow{State: checkNoRun, Name: name, Text: esc(err.Error())}, true
	}
	offline, err := db.Count[actions_model.ActionRunner](ctx, actions_model.FindRunnerOptions{WithVisible: true, IsOnline: optional.Some(false)})
	if err != nil {
		return checkRow{State: checkNoRun, Name: name, Text: esc(err.Error())}, true
	}
	switch {
	case total == 0:
		return checkRow{State: checkWarn, Name: name, Text: ctx.Tr("asc_admin.sys_runners_none"), Fix: fix}, true
	case offline > 0:
		return checkRow{State: checkErr, Name: name, Text: ctx.Tr("asc_admin.check_runners_off", total-offline, total), Fix: fix}, true
	}
	return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_runners_ok", total)}, true
}

func checkNotices(ctx *context.Context) (checkRow, bool) {
	name := ctx.Tr("asc_admin.notices")
	if n := system_model.CountNotices(ctx); n > 0 {
		return checkRow{State: checkWarn, Name: name, Text: ctx.Locale.TrN(n, "asc_admin.att_notices_one", "asc_admin.att_notices_other", n), Fix: setting.AppSubURL + "/admin/notices"}, true
	}
	return checkRow{State: checkOK, Name: name, Text: ctx.Tr("asc_admin.check_notices_ok")}, true
}

// SelfCheckStep serves GET /admin/self_check/checks/{id}: one row of the self check, run now. A check that does not
// apply to this hub answers 204 and the page removes its row.
func SelfCheckStep(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	id := ctx.Params("id")
	for _, c := range healthChecks() {
		if c.ID != id {
			continue
		}
		row, ok := runHealthCheck(ctx, c)
		if !ok {
			ctx.Status(http.StatusNoContent)
			return
		}
		ctx.Data["Row"] = row
		ctx.HTML(http.StatusOK, tplSelfCheckRow)
		return
	}
	ctx.NotFound("self check step", nil)
}
