// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/taskout"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/web"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/cron"
	"forgejo.org/services/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flush of a queue names the items it removes, before (the confirmation) and after (the message).
func TestQueueFlushNamesTheCount(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	q, err := queue.NewWorkerPoolQueueWithContext(t.Context(), "asc-flush-test", setting.QueueSettings{
		Type: "level", Datadir: t.TempDir(), Length: 100, BatchLength: 1, MaxWorkers: 1,
	}, func(items ...string) []string { return nil }, false)
	require.NoError(t, err)
	// The queue does not run, so the items wait.
	for _, s := range []string{"a", "b", "c"} {
		require.NoError(t, q.Push(s))
	}
	queue.GetManager().AddManagedQueue(q)
	var qid int64
	for id, mq := range queue.GetManager().ManagedQueues() {
		if mq.GetName() == "asc-flush-test" {
			qid = id
		}
	}
	require.NotZero(t, qid)
	defer func() {
		m := queue.GetManager()
		delete(m.Queues, qid)
	}()

	ctx, rec := contexttest.MockContext(t, "GET /admin/monitor/queue/"+strconv.FormatInt(qid, 10))
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams(":qid", strconv.FormatInt(qid, 10))
	QueueManage(ctx)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `<div class="ds-stat-value">3</div>`, "the waiting items")
	assert.Contains(t, body, `data-modal="#adm-q-flush-confirm"`, "a queue with items offers the flush, behind a confirmation")
	assert.Contains(t, body, "asc_admin.queue_flush_text", "the confirmation names the count")

	ctx, _ = contexttest.MockContext(t, "POST /admin/monitor/queue/"+strconv.FormatInt(qid, 10)+"/remove-all-items")
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams(":qid", strconv.FormatInt(qid, 10))
	QueueRemoveAllItems(ctx)
	assert.Contains(t, ctx.Flash.SuccessMsg, "asc_admin.queue_flushed_", "the message counts the removed items (the test locale does not choose the plural)")
	assert.Zero(t, q.GetQueueItemNumber())

	// An empty queue has no flush, and flushing it again says the plain message.
	ctx, rec = contexttest.MockContext(t, "GET /admin/monitor/queue/"+strconv.FormatInt(qid, 10))
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams(":qid", strconv.FormatInt(qid, 10))
	QueueManage(ctx)
	assert.NotContains(t, rec.Body.String(), `adm-q-flush-confirm`)
	ctx, _ = contexttest.MockContext(t, "POST /admin/monitor/queue/x/remove-all-items")
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams(":qid", strconv.FormatInt(qid, 10))
	QueueRemoveAllItems(ctx)
	assert.Equal(t, "admin.monitor.queue.settings.remove_all_items.success", ctx.Flash.SuccessMsg)
}

func runPanel(t *testing.T, query string) string {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, "GET /admin/monitor/cron"+query)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	CronTasks(ctx)
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func cronOutput(t *testing.T, name, from string) (int, taskout.Output) {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, "GET /admin/monitor/cron/"+name+"/output?from="+from)
	contexttest.LoadUser(t, ctx, 1)
	ctx.SetParams(":name", name)
	CronOutput(ctx)
	var out taskout.Output
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	}
	return rec.Code, out
}

// "Run now" on the monitor page opens a panel that follows the output of the run.
func TestRunNowShowsTheOutputOfTheRun(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	release := make(chan struct{})
	began := make(chan struct{}, 4)
	fail := false
	defer cron.UseTestTask("asc_test_run", func(ctx gocontext.Context, _ *user_model.User, _ cron.Config) error {
		taskout.Say(ctx, "checked events 1 to 50000 of 90000")
		began <- struct{}{}
		<-release
		taskout.Say(ctx, "checked events 50001 to the head (90000)")
		if fail {
			return errors.New("the hash of event 61000 does not match")
		}
		return nil
	})()

	// The button posts to the dashboard handler, which starts the run and sends the page to the panel.
	ctx, rec := contexttest.MockContext(t, "POST /admin")
	contexttest.LoadUser(t, ctx, 1)
	web.SetForm(ctx, &forms.AdminDashboardForm{Op: "asc_test_run", From: "monitor"})
	DashboardPost(ctx)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, setting.AppSubURL+"/admin/monitor/cron?run=asc_test_run&after=0#adm-run", rec.Header().Get("Location"))
	<-began

	body := runPanel(t, "?run=asc_test_run&after=0")
	assert.Contains(t, body, `data-testid="cron-run"`)
	assert.Contains(t, body, `data-state="running"`)
	assert.Contains(t, body, `data-cron-output="`+setting.AppSubURL+`/admin/monitor/cron/asc_test_run/output"`)
	assert.Contains(t, body, "<li>checked events 1 to 50000 of 90000</li>")
	assert.Contains(t, body, "asc_admin.cron_run_running")

	code, out := cronOutput(t, "asc_test_run", "1")
	require.Equal(t, http.StatusOK, code)
	assert.True(t, out.Running)
	assert.Equal(t, 1, out.From)
	assert.Empty(t, out.Lines, "the reader has the first line")

	close(release)
	require.Eventually(t, func() bool {
		_, out = cronOutput(t, "asc_test_run", "1")
		return !out.Running
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"checked events 50001 to the head (90000)"}, out.Lines)
	assert.False(t, out.Failed)
	assert.NotEmpty(t, out.Doer)

	body = runPanel(t, "?run=asc_test_run&after=0")
	assert.Contains(t, body, `data-state="finished"`)
	assert.Contains(t, body, "asc_admin.cron_result_ok")

	// Before the clicked run begins the panel waits and shows no lines of the last run.
	body = runPanel(t, "?run=asc_test_run&after=1")
	assert.Contains(t, body, `data-state="waiting"`)
	assert.NotContains(t, body, "<li>checked events")

	// A run that fails shows the error as its last line.
	fail = true
	release = make(chan struct{})
	close(release)
	cron.LookupTask("asc_test_run").RunWithUser(&user_model.User{ID: 1, Name: "user1"}, nil)
	_, out = cronOutput(t, "asc_test_run", "0")
	assert.True(t, out.Failed)
	assert.Equal(t, "error: the hash of event 61000 does not match", out.Lines[len(out.Lines)-1])
	body = runPanel(t, "?run=asc_test_run&after=1")
	assert.Contains(t, body, `data-state="failed"`)

	// An unknown task has no panel and no output.
	assert.NotContains(t, runPanel(t, "?run=nope"), `data-testid="cron-run"`)
	code, _ = cronOutput(t, "nope", "0")
	assert.Equal(t, http.StatusNotFound, code)
}
