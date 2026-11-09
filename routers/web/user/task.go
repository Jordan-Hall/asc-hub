// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"
	"strconv"

	admin_model "forgejo.org/models/admin"
	"forgejo.org/modules/json"
	"forgejo.org/services/context"
	repo_service "forgejo.org/services/repository"

	"github.com/dustin/go-humanize"
)

// TaskStatus returns task's status
func TaskStatus(ctx *context.Context) {
	task, opts, err := admin_model.GetMigratingTaskByID(ctx, ctx.ParamsInt64("task"), ctx.Doer.ID)
	if err != nil {
		if admin_model.IsErrTaskDoesNotExist(err) {
			ctx.JSON(http.StatusNotFound, map[string]any{
				"error": "task `" + strconv.FormatInt(ctx.ParamsInt64("task"), 10) + "` does not exist",
			})
			return
		}
		ctx.JSON(http.StatusInternalServerError, map[string]any{
			"err": err,
		})
		return
	}

	message := task.Message
	var done, total int64

	if task.Message != "" && task.Message[0] == '{' {
		// assume message is actually a translatable string
		var translatableMessage admin_model.TranslatableMessage
		if err := json.Unmarshal([]byte(task.Message), &translatableMessage); err != nil {
			translatableMessage = admin_model.TranslatableMessage{
				Format: "repo.migrate.migrating_failed.error",
				Args:   []any{task.Message},
			}
		}

		// Convert float64 to integers. Currently no usage of floats.
		for i := range translatableMessage.Args {
			if arg, ok := translatableMessage.Args[i].(float64); ok {
				translatableMessage.Args[i] = int64(arg)
			}
		}

		args := translatableMessage.Args
		// The history import of an asc repository: the counts drive the progress bar, the text groups their digits.
		if translatableMessage.Format == repo_service.ImportProgressFormat && len(args) == 2 {
			d, dok := args[0].(int64)
			n, nok := args[1].(int64)
			if dok && nok {
				args = []any{humanize.Comma(d), humanize.Comma(n)}
				if n > 0 && d >= 0 && d <= n {
					done, total = d, n
				}
			}
		}
		message = ctx.Locale.TrString(translatableMessage.Format, args...)
	}

	ctx.JSON(http.StatusOK, map[string]any{
		"status": task.Status,
		// The reason of a failure may name remote addresses and internals of the instance.
		"message":   repo_service.RedactMigrationMessage(message),
		"repo-id":   task.RepoID,
		"repo-name": opts.RepoName,
		"start":     task.StartTime,
		"end":       task.EndTime,
		// How far the history import is, when the migration says; total 0 when it does not.
		"done":  done,
		"total": total,
	})
}
