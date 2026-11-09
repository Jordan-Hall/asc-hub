// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascscope"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeAgent turns the fixture user id into an agent (a bot account).
func makeAgent(t *testing.T, id int64) {
	t.Helper()
	_, err := db.GetEngine(t.Context()).ID(id).Cols("type").Update(&user_model.User{Type: user_model.UserTypeBot})
	require.NoError(t, err)
}

func TestAscRunScope(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	makeAgent(t, 5)

	scope := func(run *ActionRun) ascscope.Scope {
		t.Helper()
		s, err := run.AscScope(t.Context())
		require.NoError(t, err)
		return s
	}
	assert.Equal(t, ascscope.ScopeChecks, scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Ref: "refs/heads/main", Event: webhook_module.HookEventPush}))
	assert.Equal(t, ascscope.ScopeChecks, scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventPullRequest, PullRequestPosterID: 4}),
		"a pull request a person opened")
	assert.Equal(t, ascscope.ScopeReleases, scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventRelease}))
	assert.Equal(t, ascscope.ScopeReleases, scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Ref: "refs/tags/v1.0", Event: webhook_module.HookEventPush}))
	assert.Equal(t, ascscope.ScopeAgents, scope(&ActionRun{RepoID: 1, TriggerUserID: 5, Ref: "refs/heads/main", Event: webhook_module.HookEventPush}),
		"an agent pushed")
	assert.Equal(t, ascscope.ScopeAgents, scope(&ActionRun{RepoID: 1, TriggerUserID: 5, Ref: "refs/tags/v1.0", Event: webhook_module.HookEventPush}),
		"an agent's run is an agent run even for a tag")
	assert.Equal(t, ascscope.ScopeAgents, scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventPullRequestSync, PullRequestPosterID: 5}),
		"a person's push to a pull request an agent opened")
	assert.Equal(t, ascscope.Scope(0), scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventPullRequest, PullRequestPosterID: 99999}),
		"an author who no longer exists: nobody can tell, so the run receives nothing")
	assert.Equal(t, ascscope.Scope(0), scope(&ActionRun{RepoID: 1, TriggerUserID: 99999, Ref: "refs/heads/main", Event: webhook_module.HookEventPush}),
		"a starter who no longer exists")
	assert.Equal(t, ascscope.Scope(0), scope(&ActionRun{RepoID: 1, TriggerUserID: user_model.GhostUserID, Ref: "refs/heads/main", Event: webhook_module.HookEventPush}),
		"a starter recorded as the ghost")
	assert.Equal(t, ascscope.Scope(0), scope(&ActionRun{RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventPullRequest, PullRequestPosterID: user_model.GhostUserID}),
		"an author recorded as the ghost")
	assert.Equal(t, ascscope.Scope(0), scope(&ActionRun{
		RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventIssueComment,
		EventPayload: `{"issue":{"user":{"id":-1},"pull_request":{}}}`,
	}), "a payload naming the ghost as the author")
	assert.Equal(t, ascscope.ScopeChecks, scope(&ActionRun{RepoID: 1, TriggerUserID: user_model.ActionsUserID, Ref: "refs/heads/main", Event: webhook_module.HookEventSchedule}),
		"a schedule runs the default branch's reviewed workflow")
	assert.Equal(t, ascscope.ScopeAgents, scope(&ActionRun{
		RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventPullRequest, TriggerEvent: "pull_request_target",
		EventPayload: `{"action":"labeled","pull_request":{"id":3,"user":{"id":5}}}`,
	}), "pull_request_target records no pull request on the run: the author comes from the payload")
	assert.Equal(t, ascscope.ScopeAgents, scope(&ActionRun{
		RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventIssueComment,
		EventPayload: `{"issue":{"user":{"id":5},"pull_request":{"merged":false}}}`,
	}), "a comment on a pull request an agent opened")
	assert.Equal(t, ascscope.ScopeChecks, scope(&ActionRun{
		RepoID: 1, TriggerUserID: 2, Event: webhook_module.HookEventIssueComment,
		EventPayload: `{"issue":{"user":{"id":5},"pull_request":null}}`,
	}), "a comment on an issue an agent opened runs no agent code")
}

func TestPullRequestPosterOfPayload(t *testing.T) {
	assert.Zero(t, pullRequestPosterOfPayload(""))
	assert.Zero(t, pullRequestPosterOfPayload("not json"))
	assert.Zero(t, pullRequestPosterOfPayload(`{"ref":"refs/heads/main"}`))
	assert.EqualValues(t, 7, pullRequestPosterOfPayload(`{"pull_request":{"user":{"id":7}},"issue":{"user":{"id":8},"pull_request":{}}}`))
	assert.EqualValues(t, 8, pullRequestPosterOfPayload(`{"issue":{"user":{"id":8},"pull_request":{}}}`))
	assert.Zero(t, pullRequestPosterOfPayload(`{"issue":{"user":{"id":8}}}`))
}

// A variable reaches a run only when its scopes include the run's, at every level; a repository variable the run
// may not have hides the owner and global variables of the same name.
func TestGetVariablesOfRunByScope(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	makeAgent(t, 5)

	insert := func(ownerID, repoID int64, name, data string, scopes ascscope.Scope) {
		v, err := InsertVariable(ctx, ownerID, repoID, name, data)
		require.NoError(t, err)
		assert.Equal(t, ascscope.DefaultScope, v.Scopes)
		require.NoError(t, SetVariableMeta(ctx, v.ID, 2, scopes))
	}
	insert(0, 0, "GLOBAL_PLAIN", "global", ascscope.DefaultScope)
	insert(2, 0, "OWNER_AGENTS", "owner agents", ascscope.AllScopes)
	insert(2, 0, "SHADOWED", "owner wide", ascscope.AllScopes)
	insert(0, 1, "SHADOWED", "repo checks", ascscope.ScopeChecks)

	vars := func(trigger int64) map[string]string {
		t.Helper()
		got, err := GetVariablesOfRun(ctx, &ActionRun{RepoID: 1, TriggerUserID: trigger, Ref: "refs/heads/main", Event: webhook_module.HookEventPush})
		require.NoError(t, err)
		return got
	}
	assert.Equal(t, map[string]string{"GLOBAL_PLAIN": "global", "OWNER_AGENTS": "owner agents", "SHADOWED": "repo checks"}, vars(2))
	assert.Equal(t, map[string]string{"OWNER_AGENTS": "owner agents"}, vars(5), "an agent's push")
}

func TestSetVariableMeta(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	v, err := InsertVariable(ctx, 0, 1, "META", "value")
	require.NoError(t, err)

	require.NoError(t, SetVariableMeta(ctx, v.ID, 2, ascscope.ScopeAgents))
	got := unittest.AssertExistsAndLoadBean(t, &ActionVariable{ID: v.ID})
	assert.EqualValues(t, 2, got.UpdaterID)
	assert.Equal(t, ascscope.ScopeAgents, got.Scopes)

	require.NoError(t, SetVariableMeta(ctx, v.ID, 4, 0))
	got = unittest.AssertExistsAndLoadBean(t, &ActionVariable{ID: v.ID})
	assert.EqualValues(t, 4, got.UpdaterID)
	assert.Equal(t, ascscope.ScopeAgents, got.Scopes, "scopes 0 keeps them")

	require.Error(t, SetVariableMeta(ctx, v.ID, 2, 8))
	assert.Equal(t, ascscope.ScopeAgents, unittest.AssertExistsAndLoadBean(t, &ActionVariable{ID: v.ID}).Scopes)
}

func TestVariableLastSet(t *testing.T) {
	assert.EqualValues(t, 20, (&ActionVariable{CreatedUnix: 10, UpdatedUnix: 20}).LastSet())
	assert.EqualValues(t, 10, (&ActionVariable{CreatedUnix: 10}).LastSet())
}
