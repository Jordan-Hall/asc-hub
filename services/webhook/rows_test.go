// SPDX-License-Identifier: MIT

package webhook

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	webhook_model "forgejo.org/models/webhook"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventGroups(t *testing.T) {
	everything := &webhook_model.Webhook{HookEvent: &webhook_module.HookEvent{SendEverything: true}}
	assert.Empty(t, EventGroups(everything))

	push := &webhook_model.Webhook{HookEvent: &webhook_module.HookEvent{PushOnly: true}}
	assert.Equal(t, []string{"repo.settings.event_push"}, EventGroups(push))

	chosen := &webhook_model.Webhook{HookEvent: &webhook_module.HookEvent{ChooseEvents: true, HookEvents: webhook_module.HookEvents{
		Release: true, IssueLabel: true, PullRequestSync: true, PullRequestReviewRequest: true, ActionRunFailure: true,
		ReleasePublished: true, ChangeLanded: true, AgentRunFinished: true,
	}}}
	assert.Equal(t, []string{
		"asc_hooks.ev_change_landed", "asc_hooks.ev_agent_run_finished", "asc_hooks.ev_release_published", "repo.settings.event_release", "asc_hooks.ev_issues", "asc_hooks.ev_pull_requests", "asc_hooks.ev_reviews", "asc_hooks.ev_action_runs",
	}, EventGroups(chosen))
}

func TestListRows(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	hooks, err := db.Find[webhook_model.Webhook](db.DefaultContext, webhook_model.ListWebhookOptions{RepoID: 1})
	require.NoError(t, err)
	require.Len(t, hooks, 2)
	hooks[1].HookEvent = &webhook_module.HookEvent{ChooseEvents: true, HookEvents: webhook_module.HookEvents{Create: true, Push: true, Issues: true, Wiki: true}}

	rows, err := ListRows(db.DefaultContext, hooks)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// hook 1 delivered three times; the newest, task 3, succeeded
	assert.EqualValues(t, 3, rows[0].Last.ID)
	assert.False(t, rows[0].Failed())
	assert.Equal(t, 0, rows[0].Status(), "task 3 has no response recorded")

	// hook 2 never delivered: no Replay, and four event groups shown as two and "+2 more"
	assert.Nil(t, rows[1].Last)
	assert.False(t, rows[1].Failed())
	assert.False(t, rows[1].AllEvents)
	assert.Equal(t, []string{"repo.settings.event_create", "repo.settings.event_push"}, rows[1].Events)
	assert.Equal(t, 2, rows[1].MoreEvents)

	failed := &ListRow{Last: &webhook_model.HookTask{IsSucceed: false, ResponseInfo: &webhook_model.HookResponse{Status: 502}}}
	assert.True(t, failed.Failed())
	assert.Equal(t, 502, failed.Status())
	assert.Equal(t, "502 Bad Gateway", failed.StatusLine())
	odd := &ListRow{Last: &webhook_model.HookTask{ResponseInfo: &webhook_model.HookResponse{Status: 599}}}
	assert.Equal(t, "599", odd.StatusLine())

	none, err := ListRows(db.DefaultContext, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestListRowHost(t *testing.T) {
	r := &ListRow{Webhook: &webhook_model.Webhook{URL: "https://deploy.acme.example/asc"}}
	assert.Equal(t, "deploy.acme.example", r.Host())
	r.URL = "not a url"
	assert.Equal(t, "not a url", r.Host())
}

func TestWebhookSecrets(t *testing.T) {
	assert.True(t, HasSecret(webhook_module.FORGEJO))
	assert.True(t, HasSecret(webhook_module.GITEA))
	assert.True(t, HasSecret(webhook_module.GOGS))
	assert.False(t, HasSecret(webhook_module.SLACK))
	a, b := NewSecret(), NewSecret()
	assert.NotEqual(t, a, b)
	assert.GreaterOrEqual(t, len(a), 43)
}

func TestListRowURLParts(t *testing.T) {
	row := &ListRow{Webhook: &webhook_model.Webhook{URL: "https://ci.acme.dev/hooks/asc"}}
	assert.Equal(t, "https://", row.URLScheme())
	assert.Equal(t, "ci.acme.dev/hooks/asc", row.URLRest())
	bare := &ListRow{Webhook: &webhook_model.Webhook{URL: "ci.acme.dev"}}
	assert.Empty(t, bare.URLScheme())
	assert.Equal(t, "ci.acme.dev", bare.URLRest())
}

func TestListRowKind(t *testing.T) {
	kind := func(t webhook_module.HookType) string {
		return (&ListRow{Webhook: &webhook_model.Webhook{Type: t}}).Kind()
	}
	assert.Equal(t, "asc_hooks.kind_generic", kind(webhook_module.FORGEJO))
	assert.Equal(t, "asc_hooks.kind_generic", kind(webhook_module.GITEA))
	assert.Equal(t, "asc_hooks.kind_chat", kind(webhook_module.SLACK))
	assert.Equal(t, "asc_hooks.kind_chat", kind(webhook_module.MATRIX))
	assert.Equal(t, "asc_hooks.kind_service", kind(webhook_module.PACKAGIST))
}

// A hook counts as failing from its second failure in a row.
func TestListRowFailing(t *testing.T) {
	assert.False(t, (&ListRow{FailStreak: 0}).Failing())
	assert.False(t, (&ListRow{FailStreak: 1}).Failing())
	assert.True(t, (&ListRow{FailStreak: 2}).Failing())
}
