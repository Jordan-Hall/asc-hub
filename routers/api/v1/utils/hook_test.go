// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package utils

import (
	"net/http"
	"reflect"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/models/webhook"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/structs"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestHookValidation(t *testing.T) {
	unittest.PrepareTestEnv(t)

	t.Run("Test Validation", func(t *testing.T) {
		ctx, _ := contexttest.MockAPIContext(t, "user2/repo1/hooks")
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		contexttest.LoadRepoCommit(t, ctx)
		contexttest.LoadUser(t, ctx, 2)

		checkCreateHookOption(ctx, &structs.CreateHookOption{
			Type: "gitea",
			Config: map[string]string{
				"content_type": "json",
				"url":          "https://example.com/webhook",
			},
		})
		assert.Equal(t, 0, ctx.Resp.WrittenStatus()) // not written yet
	})

	t.Run("Test Validation with invalid URL", func(t *testing.T) {
		ctx, _ := contexttest.MockAPIContext(t, "user2/repo1/hooks")
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		contexttest.LoadRepoCommit(t, ctx)
		contexttest.LoadUser(t, ctx, 2)

		checkCreateHookOption(ctx, &structs.CreateHookOption{
			Type: "gitea",
			Config: map[string]string{
				"content_type": "json",
				"url":          "example.com/webhook",
			},
		})
		assert.Equal(t, http.StatusUnprocessableEntity, ctx.Resp.WrittenStatus())
	})

	t.Run("Test Validation with invalid webhook type", func(t *testing.T) {
		ctx, _ := contexttest.MockAPIContext(t, "user2/repo1/hooks")
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		contexttest.LoadRepoCommit(t, ctx)
		contexttest.LoadUser(t, ctx, 2)

		checkCreateHookOption(ctx, &structs.CreateHookOption{
			Type: "unknown",
			Config: map[string]string{
				"content_type": "json",
				"url":          "example.com/webhook",
			},
		})
		assert.Equal(t, http.StatusUnprocessableEntity, ctx.Resp.WrittenStatus())
	})

	t.Run("Test Validation with empty content type", func(t *testing.T) {
		ctx, _ := contexttest.MockAPIContext(t, "user2/repo1/hooks")
		contexttest.LoadRepo(t, ctx, 1)
		contexttest.LoadGitRepo(t, ctx)
		contexttest.LoadRepoCommit(t, ctx)
		contexttest.LoadUser(t, ctx, 2)

		checkCreateHookOption(ctx, &structs.CreateHookOption{
			Type: "unknown",
			Config: map[string]string{
				"url": "https://example.com/webhook",
			},
		})
		assert.Equal(t, http.StatusUnprocessableEntity, ctx.Resp.WrittenStatus())
	})
}

func TestHookEventInclusion(t *testing.T) {
	ctx, _ := contexttest.MockAPIContext(t, "user2/repo1/hooks")
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadGitRepo(t, ctx)
	contexttest.LoadRepoCommit(t, ctx)
	contexttest.LoadUser(t, ctx, 2)

	opts := structs.CreateHookOption{
		Type: "forgejo",
		Config: structs.CreateHookOptionConfig{
			"content_type": "json",
			"url":          "http://example.com/webhook",
		},
		Events: []string{
			string(webhook_module.HookEventCreate),
			string(webhook_module.HookEventDelete),
			string(webhook_module.HookEventFork),
			string(webhook_module.HookEventIssues),
			string(webhook_module.HookEventPush),
			string(webhook_module.HookEventPullRequest),
			string(webhook_module.HookEventWiki),
			string(webhook_module.HookEventRepository),
			string(webhook_module.HookEventRelease),
			string(webhook_module.HookEventPackage),
			string(webhook_module.HookEventActionRunFailure),
			string(webhook_module.HookEventActionRunRecover),
			string(webhook_module.HookEventActionRunSuccess),
		},
	}
	hook, ok := addHook(ctx, &opts, 2, 1)
	require.True(t, ok)
	val := reflect.ValueOf(hook.HookEvents)
	ty := val.Type()
	for i := range val.NumField() {
		assert.Truef(t, val.Field(i).Interface().(bool), "missing '%s' event", ty.Field(i).Name)
	}
}

// An edit of the address of a webhook goes through the same transport rule as a create.
func TestEditHookRefusesHTTP(t *testing.T) {
	unittest.PrepareTestEnv(t)
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Webhook.AllowedHostList, "loopback")()

	ctx, resp := contexttest.MockAPIContext(t, "user2/repo1/hooks")
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadUser(t, ctx, 2)
	w := &webhook.Webhook{RepoID: 1, URL: "https://example.org/hook", HTTPMethod: "POST", ContentType: webhook.ContentTypeJSON, Type: webhook_module.GITEA}
	require.NoError(t, webhook.CreateWebhook(ctx, w, ""))

	ok := editHook(ctx, &structs.EditHookOption{Config: map[string]string{"url": "http://example.org/hook"}}, w)
	assert.False(t, ok)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.Code)

	ctx2, resp2 := contexttest.MockAPIContext(t, "user2/repo1/hooks")
	contexttest.LoadRepo(t, ctx2, 1)
	contexttest.LoadUser(t, ctx2, 2)
	w2 := &webhook.Webhook{RepoID: 1, URL: "https://example.org/hook", HTTPMethod: "POST", ContentType: webhook.ContentTypeJSON, Type: webhook_module.GITEA}
	require.NoError(t, webhook.CreateWebhook(ctx2, w2, ""))
	assert.True(t, editHook(ctx2, &structs.EditHookOption{Config: map[string]string{"url": "http://127.0.0.1:8080/hook"}}, w2))
	assert.NotEqual(t, http.StatusUnprocessableEntity, resp2.Code)
}
