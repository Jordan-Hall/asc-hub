// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"context"

	actions_model "forgejo.org/models/actions"
	packages_model "forgejo.org/models/packages"
	"forgejo.org/modules/log"
)

type publishTaskKey struct{}

// PublishTaskKey is the request context key of the Actions task that authenticated a package request (an int64). The
// package middleware sets it from the task token; a version created in that request records the task's provenance.
var PublishTaskKey any = publishTaskKey{}

// recordAscProvenance keeps where version pv was published from when an Actions task publishes it. A failure is logged
// and the version is kept without provenance: the page then says nothing about where it came from, never something wrong.
func recordAscProvenance(ctx context.Context, pv *packages_model.PackageVersion) {
	taskID, ok := ctx.Value(PublishTaskKey).(int64)
	if !ok || taskID <= 0 {
		return
	}
	p, err := ascProvenanceOfTask(ctx, taskID)
	if err == nil {
		err = packages_model.SetAscProvenance(ctx, pv.ID, p)
	}
	if err != nil {
		log.Warn("packages: the provenance of version %d from task %d: %v", pv.ID, taskID, err)
	}
}

func ascProvenanceOfTask(ctx context.Context, taskID int64) (*packages_model.AscProvenance, error) {
	task, err := actions_model.GetTaskByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := task.LoadJob(ctx); err != nil {
		return nil, err
	}
	if err := task.Job.LoadRun(ctx); err != nil {
		return nil, err
	}
	return &packages_model.AscProvenance{RepoID: task.RepoID, RunID: task.Job.RunID, Commit: task.CommitSHA, Ref: task.Job.Run.Ref}, nil
}
