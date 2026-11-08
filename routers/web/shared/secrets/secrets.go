// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package secrets

import (
	"errors"

	"forgejo.org/models/db"
	secret_model "forgejo.org/models/secret"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
	"forgejo.org/modules/web"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
	secrets_service "forgejo.org/services/secrets"
)

func SetSecretsContext(ctx *context.Context, ownerID, repoID int64) {
	secrets, err := db.Find[secret_model.Secret](ctx, secret_model.FindSecretsOptions{OwnerID: ownerID, RepoID: repoID})
	if err != nil {
		ctx.ServerError("FindSecrets", err)
		return
	}

	ctx.Data["Secrets"] = secrets

	// Who last set each value (board RepoSettingsSecrets, "Updated by X, 12 d ago").
	ids := make([]int64, 0, len(secrets))
	for _, s := range secrets {
		if s.UpdaterID > 0 {
			ids = append(ids, s.UpdaterID)
		}
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
	ctx.Data["SecretUpdaters"] = updaters
}

func CreateSecretPost(ctx *context.Context, ownerID, repoID int64, redirectURL string) {
	form := web.GetForm(ctx).(*forms.CreateSecretForm)

	normalizedData := util.ReserveLineBreakForTextarea(form.Data)
	secret, err := secret_model.InsertEncryptedSecret(ctx, ownerID, repoID, form.Name, normalizedData)
	if err != nil {
		log.Error("InsertEncryptedSecret failed: %v", err)
		ctx.JSONError(ctx.Tr("secrets.creation.failed"))
		return
	}
	if err := secret_model.MarkUpdated(ctx, secret.ID, ctx.Doer.ID); err != nil {
		ctx.ServerError("MarkUpdated", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("secrets.creation.success", secret.Name))
	ctx.JSONRedirect(redirectURL)
}

func EditSecretPost(ctx *context.Context, ownerID, repoID, id int64, redirectURL string) {
	form := web.GetForm(ctx).(*forms.EditSecretForm)

	secret, err := secret_model.GetSecretByID(ctx, ownerID, repoID, id)
	if errors.Is(err, util.ErrNotExist) {
		ctx.NotFound("GetSecretByID", err)
		return
	} else if err != nil {
		ctx.ServerError("GetSecretByID", err)
		return
	}

	// The name is locked (board RepoSettingsSecrets, "Update value"): an edit replaces the value only.
	if form.Data == "" {
		ctx.JSONError(ctx.Tr("actions.secrets.mutation.failure_message", secret.Name))
		return
	}
	secret.SetData(util.ReserveLineBreakForTextarea(form.Data))

	err = secret_model.UpdateSecret(ctx, secret)
	if err != nil {
		log.Error("UpdateSecret failed: %v", err)
		ctx.JSONError(ctx.Tr("actions.secrets.mutation.failure_message", secret.Name))
		return
	}
	if err := secret_model.MarkUpdated(ctx, secret.ID, ctx.Doer.ID); err != nil {
		ctx.ServerError("MarkUpdated", err)
		return
	}

	ctx.Flash.Success(ctx.Tr("actions.secrets.mutation.success_message", secret.Name))
	ctx.JSONRedirect(redirectURL)
}

func DeleteSecretPost(ctx *context.Context, ownerID, repoID, id int64, redirectURL string) {
	err := secrets_service.DeleteSecretByID(ctx, ownerID, repoID, id)
	if err != nil {
		log.Error("DeleteSecretByID(%d) failed: %v", id, err)
		ctx.JSONError(ctx.Tr("secrets.deletion.failed"))
		return
	}

	ctx.Flash.Success(ctx.Tr("secrets.deletion.success"))
	ctx.JSONRedirect(redirectURL)
}
