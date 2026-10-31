// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"net/http"
	"strconv"

	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/cron"
)

func Queues(ctx *context.Context) {
	if !setting.IsProd {
		initTestQueueOnce()
	}
	ctx.Data["Title"] = ctx.Tr("asc_admin.monitor")
	ctx.Data["PageIsAdminMonitorQueue"] = true
	monitorData(ctx)
	ctx.HTML(http.StatusOK, tplQueue)
}

// monitorData fills the monitor page: the queues that have work or workers running now, apart from the idle ones (a page of
// fifteen empty rows hides the one that matters), and the scheduled tasks with how many failed their last run.
func monitorData(ctx *context.Context) {
	busy := map[int64]queue.ManagedWorkerPoolQueue{}
	idle := map[int64]queue.ManagedWorkerPoolQueue{}
	for id, q := range queue.GetManager().ManagedQueues() {
		if q.GetQueueItemNumber() != 0 || q.GetWorkerActiveNumber() > 0 {
			busy[id] = q
		} else {
			idle[id] = q
		}
	}
	ctx.Data["Queues"] = busy
	ctx.Data["IdleQueues"] = idle
	ctx.Data["QueuesTotal"] = len(busy) + len(idle)
	entries := cron.ListTasks()
	failed := 0
	for _, e := range entries {
		if e.Status == "error" {
			failed++
		}
	}
	ctx.Data["Entries"] = entries
	ctx.Data["CronFailed"] = failed
}

// QueueManage shows details for a specific queue
func QueueManage(ctx *context.Context) {
	qid := ctx.ParamsInt64("qid")
	mq := queue.GetManager().GetManagedQueue(qid)
	if mq == nil {
		ctx.Status(http.StatusNotFound)
		return
	}
	ctx.Data["Title"] = ctx.Tr("admin.monitor.queue", mq.GetName())
	ctx.Data["PageIsAdminMonitor"] = true
	ctx.Data["Queue"] = mq
	ctx.HTML(http.StatusOK, tplQueueManage)
}

// QueueSet sets the maximum number of workers and other settings for this queue
func QueueSet(ctx *context.Context) {
	qid := ctx.ParamsInt64("qid")
	mq := queue.GetManager().GetManagedQueue(qid)
	if mq == nil {
		ctx.Status(http.StatusNotFound)
		return
	}

	maxNumberStr := ctx.FormString("max-number")

	var err error
	var maxNumber int
	if len(maxNumberStr) > 0 {
		maxNumber, err = strconv.Atoi(maxNumberStr)
		if err != nil {
			ctx.Flash.Error(ctx.Tr("admin.monitor.queue.settings.maxnumberworkers.error"))
			ctx.Redirect(setting.AppSubURL + "/admin/monitor/queue/" + strconv.FormatInt(qid, 10))
			return
		}
		if maxNumber < -1 {
			maxNumber = -1
		}
	} else {
		maxNumber = mq.GetWorkerMaxNumber()
	}

	mq.SetWorkerMaxNumber(maxNumber)
	ctx.Flash.Success(ctx.Tr("admin.monitor.queue.settings.changed"))
	ctx.Redirect(setting.AppSubURL + "/admin/monitor/queue/" + strconv.FormatInt(qid, 10))
}

func QueueRemoveAllItems(ctx *context.Context) {
	// Queue in Forgejo doesn't have transaction support
	// So in rare cases, the queue could be corrupted/out-of-sync
	// Site admin could remove all items from the queue to make it work again
	qid := ctx.ParamsInt64("qid")
	mq := queue.GetManager().GetManagedQueue(qid)
	if mq == nil {
		ctx.Status(http.StatusNotFound)
		return
	}

	if err := mq.RemoveAllItems(ctx); err != nil {
		ctx.ServerError("RemoveAllItems", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("admin.monitor.queue.settings.remove_all_items.success"))
	ctx.Redirect(setting.AppSubURL + "/admin/monitor/queue/" + strconv.FormatInt(qid, 10))
}
