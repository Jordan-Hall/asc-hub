// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/gitrepo"
	webhook_module "forgejo.org/modules/webhook"
)

// GenerateGitHooks generates git hooks from a template repository
func GenerateGitHooks(ctx context.Context, templateRepo, generateRepo *repo_model.Repository) error {
	generateGitRepo, err := gitrepo.OpenRepository(ctx, generateRepo)
	if err != nil {
		return err
	}
	defer generateGitRepo.Close()

	templateGitRepo, err := gitrepo.OpenRepository(ctx, templateRepo)
	if err != nil {
		return err
	}
	defer templateGitRepo.Close()

	templateHooks, err := templateGitRepo.Hooks()
	if err != nil {
		return err
	}

	for _, templateHook := range templateHooks {
		generateHook, err := generateGitRepo.GetHook(templateHook.Name())
		if err != nil {
			return err
		}

		generateHook.Content = templateHook.Content
		if err := generateHook.Update(); err != nil {
			return err
		}
	}
	return nil
}

// GenerateWebhooks generates webhooks from a template repository. An ASC agent hook is not copied: it runs as the
// person who added it, and that person did not ask for runs in the new repository.
func GenerateWebhooks(ctx context.Context, templateRepo, generateRepo *repo_model.Repository) error {
	templateWebhooks, err := db.Find[webhook.Webhook](ctx, webhook.ListWebhookOptions{RepoID: templateRepo.ID})
	if err != nil {
		return err
	}

	ws := make([]*webhook.Webhook, 0, len(templateWebhooks))
	for _, templateWebhook := range templateWebhooks {
		if templateWebhook.Type == webhook_module.ASCAGENT {
			continue
		}
		ws = append(ws, &webhook.Webhook{
			RepoID:      generateRepo.ID,
			URL:         templateWebhook.URL,
			HTTPMethod:  templateWebhook.HTTPMethod,
			ContentType: templateWebhook.ContentType,
			Secret:      templateWebhook.Secret,
			HookEvent:   templateWebhook.HookEvent,
			IsActive:    templateWebhook.IsActive,
			Type:        templateWebhook.Type,
			OwnerID:     templateWebhook.OwnerID,
			Events:      templateWebhook.Events,
			Meta:        templateWebhook.Meta,
		})
	}
	return webhook.CreateWebhooks(ctx, ws)
}
