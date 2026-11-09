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
	"sync"
	"time"

	actions_model "forgejo.org/models/actions"
	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/models/moderation"
	system_model "forgejo.org/models/system"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/mailer"

	"google.golang.org/grpc"
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
	if n, err := newPeopleSince(ctx, time.Now().Add(-7*24*time.Hour)); err == nil {
		ctx.Data["StatPeopleWeek"] = n
	} else {
		log.Error("dashboard: new people this week: %v", err)
	}
	if a, ok := todaysActivity(ctx, time.Now()); ok {
		ctx.Data["Activity"] = a
	}
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

	system = append(system, mailRow(ctx, mailer.QueueStatus(), time.Now()))
	if row, ok := storageRow(ctx); ok {
		system = append(system, row)
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

// dashboardAscCall opens a call to asc serve as the site administrator for one figure of the dashboard, when asc is
// on and has feature. The call gives up after three seconds: the dashboard does not wait for asc.
func dashboardAscCall(ctx *context.Context, feature string) (grpc.ClientConnInterface, gocontext.Context, func(), bool) {
	if !setting.Asc.Enabled || ctx.Doer == nil || !ctx.Doer.IsAdmin || !asctrust.HasFeature(ctx, feature) {
		return nil, nil, nil, false
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, nil, nil, false
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		return nil, nil, nil, false
	}
	callCtx, cancel := gocontext.WithTimeout(callCtx, 3*time.Second)
	return conn, callCtx, func() { cancel(); release() }, true
}

// newPeopleSince counts the accounts of people (not organizations or bots) created since t.
func newPeopleSince(ctx gocontext.Context, t time.Time) (int64, error) {
	return db.GetEngine(ctx).Where("type = ? AND created_unix >= ?", user_model.UserTypeIndividual, t.Unix()).Count(new(user_model.User))
}

// activityTTL is how long the activity figures asc serve gave are shown before asc is asked again. The dashboard
// refreshes every 30 seconds in every open tab; the figures are of the whole server, the same for every administrator.
var activityTTL = time.Minute

var activityCache struct {
	sync.Mutex
	at  time.Time
	day int64
	v   asctrust.Activity
}

// todaysActivity is the activity of the whole server today (UTC) for the Changesets today and Agent runs today tiles,
// from asc serve (Usage.GetActivity). It is false when asc is off, does not have the figures or does not answer.
func todaysActivity(ctx *context.Context, now time.Time) (*asctrust.Activity, bool) {
	day := now.UTC().Unix() / 86400
	activityCache.Lock()
	if !activityCache.at.IsZero() && activityCache.day == day && now.Sub(activityCache.at) < activityTTL {
		a := activityCache.v
		activityCache.Unlock()
		return &a, true
	}
	activityCache.Unlock()
	conn, callCtx, done, ok := dashboardAscCall(ctx, asctrust.FeatureUsageActivity)
	if !ok {
		return nil, false
	}
	defer done()
	a, err := asctrust.LoadActivity(callCtx, conn)
	if err != nil {
		log.Debug("dashboard: activity: %v", err)
		return nil, false
	}
	activityCache.Lock()
	activityCache.at, activityCache.day, activityCache.v = now, day, a
	activityCache.Unlock()
	return &a, true
}

// storageRow is the "Object storage" row of the System card: what asc serve stores for the whole server this month
// against the server's limit. There is no row when asc is off or has no usage figures.
func storageRow(ctx *context.Context) (statusRow, bool) {
	conn, callCtx, done, ok := dashboardAscCall(ctx, asctrust.FeatureUsage)
	if !ok {
		return statusRow{}, false
	}
	defer done()
	name := ctx.Tr("asc_admin.sys_storage")
	u, err := asctrust.LoadUsage(callCtx, conn, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}, "")
	if err != nil {
		log.Debug("dashboard: storage usage: %v", err)
		return statusRow{name, ctx.Tr("asc_admin.sys_storage_unread"), "warn", ctx.Tr("asc_admin.state_degraded")}, true
	}
	m := u.Storage
	switch {
	case m.Over:
		return statusRow{name, ctx.Tr("asc_admin.sys_storage_of", m.UsedText(), m.LimitText()), "err", ctx.Tr("asc_admin.state_full")}, true
	case m.Limit > 0 && m.Used >= m.Limit/10*9:
		return statusRow{name, ctx.Tr("asc_admin.sys_storage_of", m.UsedText(), m.LimitText()), "warn", ctx.Tr("asc_admin.state_nearly_full")}, true
	case m.Limit > 0:
		return statusRow{name, ctx.Tr("asc_admin.sys_storage_of", m.UsedText(), m.LimitText()), "ok", ctx.Tr("asc_admin.state_healthy")}, true
	}
	return statusRow{name, ctx.Tr("asc_admin.sys_storage_used", m.UsedText()), "ok", ctx.Tr("asc_admin.state_healthy")}, true
}

// storeGauge is the Object storage gauge of the health page: what asc serve stores for the whole server, against the
// server's storage limit when one is set. False when asc is off, has no usage figures or does not answer.
func storeGauge(ctx *context.Context) (asctrust.Meter, bool) {
	conn, callCtx, done, ok := dashboardAscCall(ctx, asctrust.FeatureUsage)
	if !ok {
		return asctrust.Meter{}, false
	}
	defer done()
	u, err := asctrust.LoadUsage(callCtx, conn, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_INSTANCE}, "")
	if err != nil {
		log.Debug("health: storage usage: %v", err)
		return asctrust.Meter{}, false
	}
	return u.Storage, true
}

// mailWaitWarn is how long the oldest mail may wait before the Mail row warns.
const mailWaitWarn = 5 * time.Minute

// mailRow is the "Mail" row of the System card: the outgoing mail queue, how long its oldest mail has waited, and
// whether sending fails.
func mailRow(ctx *context.Context, q mailer.QueueState, now time.Time) statusRow {
	name := ctx.Tr("asc_admin.sys_mail")
	if !q.Enabled {
		return statusRow{name, ctx.Tr("asc_admin.sys_mail_off"), "warn", ctx.Tr("asc_admin.state_none")}
	}
	age := ""
	if !q.Oldest.IsZero() {
		age = now.Sub(q.Oldest).Round(time.Second).String()
	}
	switch {
	case q.Failing() && q.Waiting > 0:
		return statusRow{name, ctx.Tr("asc_admin.sys_mail_failing_waiting", q.Waiting), "err", ctx.Tr("asc_admin.state_down")}
	case q.Failing():
		return statusRow{name, ctx.Tr("asc_admin.sys_mail_failing"), "err", ctx.Tr("asc_admin.state_down")}
	case q.Waiting > 0 && age != "" && now.Sub(q.Oldest) > mailWaitWarn:
		return statusRow{name, ctx.Tr("asc_admin.sys_mail_waiting_age", q.Waiting, age), "warn", ctx.Tr("asc_admin.state_degraded")}
	case q.Waiting > 0 && age != "":
		return statusRow{name, ctx.Tr("asc_admin.sys_mail_waiting_age", q.Waiting, age), "ok", ctx.Tr("asc_admin.state_busy")}
	case q.Waiting > 0:
		return statusRow{name, ctx.Tr("asc_admin.sys_mail_waiting", q.Waiting), "ok", ctx.Tr("asc_admin.state_busy")}
	}
	return statusRow{name, ctx.Tr("asc_admin.sys_mail_empty"), "ok", ctx.Tr("asc_admin.state_healthy")}
}

// backupAttention asks asc serve how its scheduled backups are doing and returns a row when they are overdue or the
// last one failed. The row offers "Back up now" unless a backup is already running. When asc is off, has no backups,
// or does not answer within a few seconds, there is no row: the Backups page shows the problem.
func backupAttention(ctx *context.Context) (attentionItem, bool) {
	conn, callCtx, done, ok := dashboardAscCall(ctx, asctrust.FeatureBackups)
	if !ok {
		return attentionItem{}, false
	}
	defer done()
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
	if m, ok := storeGauge(ctx); ok {
		ctx.Data["StoreGauge"] = m
	}
	ctx.Data["Checks"] = rows
	ctx.Data["ChecksOK"], ctx.Data["ChecksWarn"], ctx.Data["ChecksFail"], ctx.Data["ChecksNoRun"] = ok, warn, fail, norun
	ctx.Data["ChecksTally"] = checkTally(ctx, ok, warn, fail, norun)
	ctx.Data["ChecksTone"] = checksTone(warn, fail, norun)
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
