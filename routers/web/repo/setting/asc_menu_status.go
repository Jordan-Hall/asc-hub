// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	secret_model "forgejo.org/models/secret"
	webhook_model "forgejo.org/models/webhook"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/context"
)

// MenuStatus puts the short statuses the phone menu of the repository settings shows next to its entries into the
// page data (AscSetStatus, by entry): who can see the repository, how many people have access, how many webhooks
// there are and whether one fails, and how many secrets. A count that cannot be read is left out, not shown wrong.
func MenuStatus(ctx *context.Context) {
	if ctx.Repo == nil || ctx.Repo.Repository == nil {
		return
	}
	repo := ctx.Repo.Repository
	status := map[string]string{}
	if repo.IsPrivate {
		status["options"] = ctx.Locale.TrString("repo.desc.private")
	} else {
		status["options"] = ctx.Locale.TrString("repo.settings.visibility.public")
	}
	if n, err := db.Count[repo_model.Collaboration](ctx, repo_model.FindCollaborationOptions{RepoID: repo.ID}); err == nil && n > 0 {
		status["access"] = string(ctx.Locale.TrN(n, "asc_shell.sn_status_access_one", "asc_shell.sn_status_access_other", n))
	}
	if hooks, err := db.Find[webhook_model.Webhook](ctx, webhook_model.ListWebhookOptions{RepoID: repo.ID}); err == nil {
		failing := 0
		for _, h := range hooks {
			if h.LastStatus == webhook_module.HookStatusFail {
				failing++
			}
		}
		switch {
		case failing > 0:
			status["hooks"] = string(ctx.Locale.TrN(failing, "asc_shell.sn_status_failing_one", "asc_shell.sn_status_failing_other", failing))
			ctx.Data["AscSetHooksFailing"] = true
		case len(hooks) > 0:
			status["hooks"] = string(ctx.Locale.TrN(len(hooks), "asc_shell.sn_status_hooks_one", "asc_shell.sn_status_hooks_other", len(hooks)))
		}
	}
	if n, err := db.Count[secret_model.Secret](ctx, secret_model.FindSecretsOptions{RepoID: repo.ID}); err == nil && n > 0 {
		status["secrets"] = string(ctx.Locale.TrN(n, "asc_shell.sn_status_secrets_one", "asc_shell.sn_status_secrets_other", n))
	}
	ctx.Data["AscSetStatus"] = status
}
