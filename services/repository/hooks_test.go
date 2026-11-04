// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repository

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateWebhooksLeavesAgentHooks(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	template := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	generated := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})

	agent := &webhook.Webhook{
		RepoID:      template.ID,
		URL:         "asc-agent://fixer",
		HTTPMethod:  "POST",
		ContentType: webhook.ContentTypeJSON,
		Type:        webhook_module.ASCAGENT,
		Meta:        `{"agent":"fixer","target":"main","task":"Fix it.","run_as":2}`,
		HookEvent:   &webhook_module.HookEvent{ChooseEvents: true},
	}
	require.NoError(t, agent.UpdateEvent())
	require.NoError(t, webhook.CreateWebhook(db.DefaultContext, agent, ""))

	before, err := db.Find[webhook.Webhook](db.DefaultContext, webhook.ListWebhookOptions{RepoID: template.ID})
	require.NoError(t, err)
	had, err := db.Find[webhook.Webhook](db.DefaultContext, webhook.ListWebhookOptions{RepoID: generated.ID})
	require.NoError(t, err)
	require.NoError(t, GenerateWebhooks(db.DefaultContext, template, generated))

	got, err := db.Find[webhook.Webhook](db.DefaultContext, webhook.ListWebhookOptions{RepoID: generated.ID})
	require.NoError(t, err)
	assert.Len(t, got, len(had)+len(before)-1, "every hook but the agent hook is copied")
	for _, w := range got {
		assert.NotEqual(t, webhook_module.ASCAGENT, w.Type, "an agent hook runs as the person who added it, so it is not copied")
	}
}
