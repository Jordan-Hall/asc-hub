// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package actions

import (
	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascscope"
	"forgejo.org/modules/log"
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/shared/secrets"
	actions_service "forgejo.org/services/actions"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

func SetVariablesContext(ctx *context.Context, ownerID, repoID int64) {
	variables, err := db.Find[actions_model.ActionVariable](ctx, actions_model.FindVariablesOpts{
		OwnerID: ownerID,
		RepoID:  repoID,
	})
	if err != nil {
		ctx.ServerError("FindVariables", err)
		return
	}
	ctx.Data["Variables"] = variables

	// Who last set each variable and the runs that receive it (board RepoSettingsSecrets, "Available to").
	ids := make([]int64, 0, len(variables))
	names := make(map[int64]string, len(variables))
	for _, v := range variables {
		if v.UpdaterID > 0 {
			ids = append(ids, v.UpdaterID)
		}
		names[v.ID] = secrets.ScopeNames(ctx, v.Scopes)
	}
	updaters := make(map[int64]*user_model.User, len(ids))
	if len(ids) > 0 {
		users, err := user_model.GetUsersByIDs(ctx, ids)
		if err != nil {
			ctx.ServerError("GetUsersByIDs", err)
			return
		}
		for _, u := range users {
			updaters[u.ID] = u
		}
	}
	ctx.Data["VariableUpdaters"] = updaters
	ctx.Data["VariableScopeNames"] = names
	ctx.Data["AscScopes"] = ascscope.Scopes
	ctx.Data["AscDefaultScope"] = ascscope.DefaultScope
}

func CreateVariable(ctx *context.Context, ownerID, repoID int64, redirectURL string) {
	form := web.GetForm(ctx).(*forms.EditVariableForm)
	scopes, ok := secrets.ScopesOfForm(ctx)
	if !ok {
		return
	}

	v, err := actions_service.CreateVariable(ctx, ctx.Doer.ID, ownerID, repoID, form.Name, form.Data, scopes)
	if err != nil {
		log.Error("CreateVariable: %v", err)
		ctx.JSONError(ctx.Tr("actions.variables.creation.failed"))
		return
	}

	ctx.Flash.Success(ctx.Tr("actions.variables.creation.success", v.Name))
	ctx.JSONRedirect(redirectURL)
}

func UpdateVariable(ctx *context.Context, ownerID, repoID int64, redirectURL string) {
	id := ctx.ParamsInt64(":variable_id")
	form := web.GetForm(ctx).(*forms.EditVariableForm)
	scopes, ok := secrets.ScopesOfForm(ctx)
	if !ok {
		return
	}

	if ok, err := actions_service.UpdateVariable(ctx, ctx.Doer.ID, id, ownerID, repoID, form.Name, form.Data, scopes); err != nil || !ok {
		if !ok {
			ctx.JSONError(ctx.Tr("actions.variables.not_found"))
		} else {
			log.Error("UpdateVariable: %v", err)
			ctx.JSONError(ctx.Tr("actions.variables.update.failed"))
		}
		return
	}
	ctx.Flash.Success(ctx.Tr("actions.variables.update.success"))
	ctx.JSONRedirect(redirectURL)
}

func DeleteVariable(ctx *context.Context, ownerID, repoID int64, redirectURL string) {
	id := ctx.ParamsInt64(":variable_id")

	if ok, err := actions_model.DeleteVariable(ctx, id, ownerID, repoID); err != nil || !ok {
		if !ok {
			ctx.JSONError(ctx.Tr("actions.variables.not_found"))
		} else {
			log.Error("Delete variable [%d] failed: %v", id, err)
			ctx.JSONError(ctx.Tr("actions.variables.deletion.failed"))
		}
		return
	}
	ctx.Flash.Success(ctx.Tr("actions.variables.deletion.success"))
	ctx.JSONRedirect(redirectURL)
}
