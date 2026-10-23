// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package permissions

import (
	"net/http"

	actions_model "forgejo.org/models/actions"
	access_model "forgejo.org/models/perm/access"
	user_model "forgejo.org/models/user"
)

func RepoAccess(ctx Context) {
	if ctx.Doer() != nil && ctx.Doer().ID == user_model.ActionsUserID && ctx.Authentication().ActionsTaskID().Has() {
		_, taskID := ctx.Authentication().ActionsTaskID().Get()
		task, err := actions_model.GetTaskByID(ctx.Context(), taskID)
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "actions_model.GetTaskByID", err)
			return
		}
		if task.RepoID != ctx.Repository().ID {
			ctx.NotFound()
			return
		}

		// The default capability of the task, narrowed by the `permissions:` of its workflow.
		taskPerm, err := access_model.GetActionRepoPermission(ctx.Context(), ctx.Repository(), task)
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "GetActionRepoPermission", err)
			return
		}
		ctx.SetPermission(&taskPerm)
	} else {
		permission, err := access_model.GetUserRepoPermissionWithReducer(ctx.Context(), ctx.Repository(), ctx.Doer(), ctx.Reducer())
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "GetUserRepoPermissionWithReducer", err)
			return
		}
		ctx.SetPermission(&permission)
	}

	if !ctx.Permission().HasAccess() {
		ctx.NotFound()
		return
	}
}
