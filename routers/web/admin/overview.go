// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"

	actions_model "forgejo.org/models/actions"
	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	system_model "forgejo.org/models/system"
	"forgejo.org/modules/base"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// attentionItem is one row of "Needs attention" on the dashboard: what is wrong, where to go.
type attentionItem struct {
	Tone   string // "warn" or "err"
	Icon   string
	Title  template.HTML
	Text   template.HTML
	Tag    template.HTML
	Action template.HTML
	Link   string
	Post   string // when set, Action is a button that posts to this address with Op
	Op     string
}

// statusRow is one row of the "System" card: a named part of the site and how it is doing.
type statusRow struct {
	Name  template.HTML
	Text  template.HTML
	State string // "ok", "warn" or "err"
	Label template.HTML
}

// dashboardFacts fills the figures, the attention list and the system rows of the dashboard.
func dashboardFacts(ctx *context.Context) {
	counter := activities_model.GetStatistic(ctx).Counter
	ctx.Data["StatUsers"] = counter.User
	ctx.Data["StatOrgs"] = counter.Org
	ctx.Data["StatRepos"] = counter.Repo
	ctx.Data["StatIssuesOpen"] = counter.IssueOpen
	ctx.Data["StatIssuesClosed"] = counter.IssueClosed
	ctx.Data["AuditRecent"] = ascRecentAudit(ctx)

	var attention []attentionItem
	var system []statusRow

	// the database answers: the page itself needed it, so the question is only how fast
	if err := db.GetEngine(ctx).Ping(); err != nil {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_database"), esc(err.Error()), "err", ctx.Tr("asc_admin.state_down")})
	} else {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_database"), ctx.Tr("asc_admin.sys_database_ok", string(setting.Database.Type)), "ok", ctx.Tr("asc_admin.state_healthy")})
	}

	if elapsed, err := cache.Test(); err != nil {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_cache"), esc(err.Error()), "err", ctx.Tr("asc_admin.state_down")})
		attention = append(attention, attentionItem{"err", "database", ctx.Tr("asc_admin.att_cache_title"), esc(err.Error()), ctx.Tr("asc_admin.sys_cache"), ctx.Tr("asc_admin.open"), setting.AppSubURL + "/admin/self_check", "", ""})
	} else if elapsed > cache.SlowCacheThreshold {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_cache"), ctx.Tr("asc_admin.sys_cache_slow", elapsed.String()), "warn", ctx.Tr("asc_admin.state_degraded")})
	} else {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_cache"), ctx.Tr("asc_admin.sys_cache_ok", elapsed.String()), "ok", ctx.Tr("asc_admin.state_healthy")})
	}

	waiting, queues := 0, 0
	for _, q := range queue.GetManager().ManagedQueues() {
		queues++
		if n := q.GetQueueItemNumber(); n > 0 {
			waiting += n
		}
	}
	if waiting > 0 {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_queues"), ctx.Tr("asc_admin.sys_queues_waiting", waiting, queues), "warn", ctx.Tr("asc_admin.state_busy")})
	} else {
		system = append(system, statusRow{ctx.Tr("asc_admin.sys_queues"), ctx.Tr("asc_admin.sys_queues_idle", queues), "ok", ctx.Tr("asc_admin.state_healthy")})
	}

	if setting.Actions.Enabled {
		total, _ := db.Count[actions_model.ActionRunner](ctx, actions_model.FindRunnerOptions{WithVisible: true})
		offline, _ := db.Count[actions_model.ActionRunner](ctx, actions_model.FindRunnerOptions{WithVisible: true, IsOnline: optional.Some(false)})
		online := total - offline
		switch {
		case total == 0:
			system = append(system, statusRow{ctx.Tr("asc_admin.sys_runners"), ctx.Tr("asc_admin.sys_runners_none"), "warn", ctx.Tr("asc_admin.state_none")})
		case offline > 0:
			system = append(system, statusRow{ctx.Tr("asc_admin.sys_runners"), ctx.Tr("asc_admin.sys_runners_of", online, total), "err", ctx.Tr("asc_admin.state_down")})
			attention = append(attention, attentionItem{"err", "cpu", ctx.Locale.TrN(offline, "asc_admin.att_runners_one", "asc_admin.att_runners_other", offline), ctx.Tr("asc_admin.att_runners_text"), ctx.Tr("asc_admin.sys_runners"), ctx.Tr("asc_admin.open"), setting.AppSubURL + "/admin/actions/runners", "", ""})
		default:
			system = append(system, statusRow{ctx.Tr("asc_admin.sys_runners"), ctx.Tr("asc_admin.sys_runners_of", online, total), "ok", ctx.Tr("asc_admin.state_healthy")})
		}
	}

	if setting.Moderation.Enabled {
		if reports, err := moderation.GetOpenReports(ctx); err == nil && len(reports) > 0 {
			ctx.Data["OpenReports"] = len(reports)
			attention = append(attention, attentionItem{"warn", "flag", ctx.Locale.TrN(len(reports), "asc_admin.att_reports_one", "asc_admin.att_reports_other", len(reports)), ctx.Tr("asc_admin.att_reports_text"), ctx.Tr("asc_admin.grp_people"), ctx.Tr("asc_admin.review"), setting.AppSubURL + "/admin/moderation/reports", "", ""})
		}
	}

	if n := system_model.CountNotices(ctx); n > 0 {
		attention = append(attention, attentionItem{"warn", "bell", ctx.Locale.TrN(n, "asc_admin.att_notices_one", "asc_admin.att_notices_other", n), ctx.Tr("asc_admin.att_notices_text"), ctx.Tr("asc_admin.notices"), ctx.Tr("asc_admin.review"), setting.AppSubURL + "/admin/notices", "", ""})
	}

	if item, ok := backupAttention(ctx); ok {
		attention = append(attention, item)
	}

	if ctx.Data["NeedMajorUpdate"] == true || ctx.Data["NeedMinorUpdate"] == true {
		attention = append([]attentionItem{{"warn", "cloud-download", ctx.Tr("asc_admin.att_update_title"), ctx.Tr("asc_admin.att_update_text"), ctx.Tr("asc_admin.system"), ctx.Tr("asc_admin.open"), setting.AppSubURL + "/admin/config", "", ""}}, attention...)
	}

	ctx.Data["Attention"] = attention
	ctx.Data["SystemRows"] = system
}

// backupAttention asks asc serve how its scheduled backups are doing and returns a row when they are overdue or the
// last one failed. The row offers "Back up now" unless a backup is already running. When asc is off, has no backups,
// or does not answer within a few seconds, there is no row: the Backups page shows the problem.
func backupAttention(ctx *context.Context) (attentionItem, bool) {
	if !setting.Asc.Enabled || ctx.Doer == nil || !ctx.Doer.IsAdmin || !asctrust.HasFeature(ctx, asctrust.FeatureBackups) {
		return attentionItem{}, false
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return attentionItem{}, false
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		return attentionItem{}, false
	}
	defer release()
	callCtx, cancel := gocontext.WithTimeout(callCtx, 3*time.Second)
	defer cancel()
	st, err := asctrust.BackupHealth(callCtx, conn)
	if err != nil {
		log.Debug("dashboard: backup status: %v", err)
		return attentionItem{}, false
	}
	if !st.Configured || (!st.Overdue && !st.Failing()) {
		return attentionItem{}, false
	}
	item := attentionItem{Tone: "warn", Icon: "cloud", Tag: ctx.Tr("asc_admin.backups"), Link: setting.AppSubURL + "/admin/asc/backups"}
	if st.Overdue {
		item.Tone = "err"
		item.Title = ctx.Tr("asc_admin.att_backup_overdue_title")
	} else {
		item.Title = ctx.Tr("asc_admin.att_backup_failed_title")
	}
	if st.LastSuccess.IsZero() {
		item.Text = ctx.Tr("asc_admin.att_backup_never")
	} else {
		item.Text = ctx.Tr("asc_admin.att_backup_last_good", st.LastSuccess.UTC().Format("2 Jan 2006 15:04 UTC"))
	}
	if st.Running {
		item.Action = ctx.Tr("asc_admin.open")
	} else {
		item.Action = ctx.Tr("asc_shell.bk_run")
		item.Post = setting.AppSubURL + "/admin/asc/backups/run"
		item.Op = asctrust.NewOpID()
	}
	return item, true
}

func esc(s string) template.HTML { return template.HTML(html.EscapeString(s)) }

// checkRow is one line of the self check: what was tried, how it went, and where to fix it.
type checkRow struct {
	ID    string // the step of the check (asc_health.go)
	State string // "ok", "warn", "err" or "norun"
	Name  template.HTML
	Text  template.HTML
	Label template.HTML
	Fix   string
}

// healthFacts runs the checks of the self check page and fills the gauges of the runtime.
func healthFacts(ctx *context.Context) {
	var rows []checkRow
	ok, warn, fail, norun := 0, 0, 0, 0
	for _, c := range healthChecks() {
		r, applies := runHealthCheck(ctx, c)
		if !applies {
			continue
		}
		switch r.State {
		case checkOK:
			ok++
		case checkWarn:
			warn++
		case checkNoRun:
			norun++
		default:
			fail++
		}
		rows = append(rows, r)
	}

	updateSystemStatus()
	ctx.Data["SysStatus"] = sysStatus
	pct := int64(0)
	if sysStatus.MemSys > 0 {
		pct = sysStatus.MemAllocated * 100 / sysStatus.MemSys
	}
	ctx.Data["MemPercent"] = pct
	ctx.Data["CPUGauge"] = cpuGauge()
	ctx.Data["DiskGauge"] = diskGauge(setting.AppDataPath)
	ctx.Data["Checks"] = rows
	ctx.Data["ChecksOK"], ctx.Data["ChecksWarn"], ctx.Data["ChecksFail"], ctx.Data["ChecksNoRun"] = ok, warn, fail, norun
	ctx.Data["ChecksTally"] = checkTally(ctx, ok, warn, fail, norun)
	var report strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&report, "%s: %s. %s\n", r.Name, r.Label, r.Text)
	}
	ctx.Data["HealthReport"] = report.String()
}

func inconsistentColumns(ctx *context.Context) []string {
	cols, _ := ctx.Data["DatabaseCheckInconsistentCollationColumns"].([]string)
	return cols
}

const tplBackups base.TplName = "admin/backups"

// Backups serves GET /admin/asc/backups. The hub cannot copy its own volume while it runs (docs/asc/OPERATIONS.md,
// "Backup and restore"), so the page tells how the backup tool is run, what an archive holds and how to check and restore it.
func Backups(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_admin.backups")
	ctx.Data["PageIsAdminBackups"] = true
	ctx.Data["AppDataPath"] = setting.AppDataPath
	ctx.Data["DatabaseType"] = string(setting.Database.Type)
	ascBackupsLoad(ctx)
	ctx.HTML(http.StatusOK, tplBackups)
}
