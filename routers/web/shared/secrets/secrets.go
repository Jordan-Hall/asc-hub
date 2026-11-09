// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package secrets

import (
	"errors"
	"strings"

	"forgejo.org/models/db"
	secret_model "forgejo.org/models/secret"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascscope"
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
	ctx.Data["AscScopes"] = ascscope.Scopes
	ctx.Data["AscDefaultScope"] = ascscope.DefaultScope
	names := make(map[int64]string, len(secrets))
	for _, s := range secrets {
		names[s.ID] = ScopeNames(ctx, s.Scopes)
	}
	ctx.Data["SecretScopeNames"] = names
}

// ScopeNames names the scopes of s for a sentence, such as "Checks, Agent runs, Releases".
func ScopeNames(ctx *context.Context, s ascscope.Scope) string {
	words := make([]string, 0, len(ascscope.Scopes))
	for _, x := range s.List() {
		words = append(words, ctx.Locale.TrString("secrets.scope."+x.Word()))
	}
	return strings.Join(words, ", ")
}

// ScopesOfForm reads the scope boxes of a posted dialog (see ascscope.OfForm); ok is false, and a JSON error is
// sent, when the dialog had them and none is ticked.
func ScopesOfForm(ctx *context.Context) (ascscope.Scope, bool) {
	scopes, ok := ascscope.OfForm(ctx.Req.Form["scopes"], ctx.Req.FormValue("scopes_sent") != "")
	if !ok {
		ctx.JSONError(ctx.Tr("secrets.scopes.none"))
	}
	return scopes, ok
}

func CreateSecretPost(ctx *context.Context, ownerID, repoID int64, redirectURL string) {
	form := web.GetForm(ctx).(*forms.CreateSecretForm)
	scopes, ok := ScopesOfForm(ctx)
	if !ok {
		return
	}

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
	if scopes != 0 {
		if err := secret_model.SetScopes(ctx, secret.ID, scopes); err != nil {
			ctx.ServerError("SetScopes", err)
			return
		}
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

	scopes, ok := ScopesOfForm(ctx)
	if !ok {
		return
	}
	// The name is locked (board RepoSettingsSecrets, "Update value"): an edit replaces the value, the scopes or
	// both. An empty value keeps the one stored, so the scopes can change without typing the value again.
	if form.Data == "" && scopes == 0 {
		ctx.JSONError(ctx.Tr("actions.secrets.mutation.failure_message", secret.Name))
		return
	}
	if form.Data != "" {
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
	}
	if scopes != 0 {
		if err := secret_model.SetScopes(ctx, secret.ID, scopes); err != nil {
			ctx.ServerError("SetScopes", err)
			return
		}
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
