// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	webhook_model "forgejo.org/models/webhook"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ascTestHook(t *testing.T, repoID int64, ev webhook_module.HookEvent) *webhook_model.Webhook {
	t.Helper()
	hook := &webhook_model.Webhook{
		RepoID:      repoID,
		URL:         "https://hooks.example.org/asc",
		ContentType: webhook_model.ContentTypeJSON,
		IsActive:    true,
		Type:        webhook_module.FORGEJO,
		HookEvent:   &ev,
	}
	require.NoError(t, hook.UpdateEvent())
	require.NoError(t, webhook_model.CreateWebhook(t.Context(), hook, ""))
	return hook
}

func ascHookTasks(t *testing.T, hookID int64) []*webhook_model.HookTask {
	t.Helper()
	var tasks []*webhook_model.HookTask
	require.NoError(t, db.GetEngine(db.DefaultContext).Where("hook_id = ?", hookID).OrderBy("id").Find(&tasks))
	return tasks
}

func TestAscRunQueuesOnceForEachEventID(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	chosen := webhook_module.HookEvent{ChooseEvents: true}
	chosen.SetAscEvent(webhook_module.HookEventAgentRunStarted, true)
	started := ascTestHook(t, repo.ID, chosen)
	everything := ascTestHook(t, repo.ID, webhook_module.HookEvent{SendEverything: true})
	pushOnly := ascTestHook(t, repo.ID, webhook_module.HookEvent{ChooseEvents: true, HookEvents: webhook_module.HookEvents{Push: true}})

	require.NoError(t, AscRun(t.Context(), repo, "op-1", webhook_module.HookEventAgentRunStarted, 7, "", 0))
	// asc's outbox sends an event again when it did not see the answer: the hooks queue it once
	require.NoError(t, AscRun(t.Context(), repo, "op-1", webhook_module.HookEventAgentRunStarted, 7, "", 0))
	require.NoError(t, AscRun(t.Context(), repo, "op-2", webhook_module.HookEventAgentRunFinished, 7, "succeeded", 3))

	tasks := ascHookTasks(t, started.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, webhook_module.HookEventAgentRunStarted, tasks[0].EventType)
	assert.JSONEq(t, `{"event":"agent_run.started","repository":"user2/repo1","run":{"id":7,"changes":0},"url":"`+repo.HTMLURL()+`/asc/agents/runs/7"}`, tasks[0].PayloadContent)

	tasks = ascHookTasks(t, everything.ID)
	require.Len(t, tasks, 2)
	assert.Equal(t, webhook_module.HookEventAgentRunFinished, tasks[1].EventType)
	assert.JSONEq(t, `{"event":"agent_run.finished","repository":"user2/repo1","run":{"id":7,"state":"succeeded","changes":3},"url":"`+repo.HTMLURL()+`/asc/agents/runs/7"}`, tasks[1].PayloadContent)
	assert.NotEqual(t, tasks[0].UUID, tasks[1].UUID)

	assert.Empty(t, ascHookTasks(t, pushOnly.ID))

	// the same event id on another hook is another delivery
	other := ascTestHook(t, repo.ID, chosen)
	require.NoError(t, AscRun(t.Context(), repo, "op-1", webhook_module.HookEventAgentRunStarted, 7, "", 0))
	assert.Len(t, ascHookTasks(t, other.ID), 1)
	assert.Len(t, ascHookTasks(t, started.ID), 1)
}

func TestAscPolicyPayload(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	chosen := webhook_module.HookEvent{ChooseEvents: true}
	chosen.SetAscEvents([]string{"policy.changed"})
	hook := ascTestHook(t, repo.ID, chosen)

	require.NoError(t, AscPolicy(t.Context(), repo, "op-9", "ab12", "cd34", "Two reviews before a change lands.", "user2"))
	tasks := ascHookTasks(t, hook.ID)
	require.Len(t, tasks, 1)
	assert.Equal(t, webhook_module.HookEventPolicyChanged, tasks[0].EventType)
	var p api.AscHookPayload
	require.NoError(t, json.Unmarshal([]byte(tasks[0].PayloadContent), &p))
	assert.Equal(t, api.AscHookPayload{
		Event:      "policy.changed",
		Repository: "user2/repo1",
		Policy:     &api.AscHookPolicy{Before: "ab12", After: "cd34", Summary: "Two reviews before a change lands."},
		Sender:     "user2",
		URL:        repo.HTMLURL() + "/settings/asc/policy",
	}, p)
}

func TestAscPayloadText(t *testing.T) {
	change := &api.AscHookChange{Number: 12, Title: "Retry the ledger sync", Base: "main", Head: "retry", Reviewer: "priya"}
	for _, c := range []struct {
		p     api.AscHookPayload
		text  string
		color int
	}{
		{api.AscHookPayload{Event: "change.opened", Change: change}, "Change [#12 Retry the ledger sync](u) opened in acme/payments by @sam", yellowColor},
		{api.AscHookPayload{Event: "change.landed", Change: change}, "Change [#12 Retry the ledger sync](u) landed on [main](" + setting.AppURL + "acme/payments/src/branch/main) in acme/payments by @sam", purpleColor},
		{api.AscHookPayload{Event: "review.requested", Change: change}, "Review of [#12 Retry the ledger sync](u) requested from @priya in acme/payments by @sam", yellowColor},
		{api.AscHookPayload{Event: "agent_run.started", Run: &api.AscHookRun{ID: 4}}, "[Agent run #4](u) started in acme/payments by @sam", yellowColor},
		{api.AscHookPayload{Event: "agent_run.finished", Run: &api.AscHookRun{ID: 4, State: "succeeded", Changes: 1}}, "[Agent run #4](u) succeeded with 1 change in acme/payments by @sam", greenColor},
		{api.AscHookPayload{Event: "agent_run.finished", Run: &api.AscHookRun{ID: 4, State: "failed"}}, "[Agent run #4](u) failed with 0 changes in acme/payments by @sam", redColor},
		{api.AscHookPayload{Event: "release.published", Release: &api.AscHookRelease{Tag: "v2.18.0", Base: "main@a41f9c2", Changes: 9}}, "Release [v2.18.0](u) published in acme/payments, 9 changes since the last one by @sam", greenColor},
		{api.AscHookPayload{Event: "policy.changed", Policy: &api.AscHookPolicy{Summary: "Two reviews."}}, "[Policy changed](u) in acme/payments: Two reviews. by @sam", orangeColor},
		{api.AscHookPayload{Event: "release.published"}, "", 0},
	} {
		c.p.Repository, c.p.URL, c.p.Sender = "acme/payments", "u", "sam"
		text, color := webhookPayloadFormatter{
			linkFormatter: func(url, text string) string { return "[" + text + "](" + url + ")" },
			nameFormatter: func(name string) string { return "@" + name },
			withSender:    true,
		}.getAscPayloadInfo(&c.p)
		assert.Equal(t, c.text, text, c.p.Event)
		assert.Equal(t, c.color, color, c.p.Event)
	}
}

func TestAscPayloadChatHooks(t *testing.T) {
	p := &api.AscHookPayload{
		Event: "release.published", Repository: "acme/payments", Sender: "priya", URL: "https://hub.example.org/acme/payments/releases/tag/v2.18.0",
		Release: &api.AscHookRelease{Tag: "v2.18.0", Base: "main@a41f9c2", Changes: 9},
	}
	data, err := p.JSONPayload()
	require.NoError(t, err)
	assert.JSONEq(t, `{"event":"release.published","repository":"acme/payments","release":{"tag":"v2.18.0","base":"main@a41f9c2","changes":9},"sender":"priya","url":"https://hub.example.org/acme/payments/releases/tag/v2.18.0"}`, string(data))

	slack, err := slackConvertor{}.Asc(p)
	require.NoError(t, err)
	assert.Contains(t, slack.Text, "<https://hub.example.org/acme/payments/releases/tag/v2.18.0|v2.18.0>")
	assert.Contains(t, slack.Text, "9 changes since the last one")

	discord, err := discordConvertor{}.Asc(p)
	require.NoError(t, err)
	require.Len(t, discord.Embeds, 1)
	assert.Contains(t, discord.Embeds[0].Title, "v2.18.0")
	assert.Equal(t, greenColor, discord.Embeds[0].Color)
}
