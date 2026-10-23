// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// This file is the only one of the permissions tests that knows how a task stores its `permissions:` and where the
// repository setting lives.

// setActionsPackagesWrite sets the Actions setting that lets `permissions: packages: write` write the owner's packages.
func setActionsPackagesWrite(t *testing.T, repoID int64, allow bool) {
	t.Helper()
	repo, err := repo_model.GetRepositoryByID(t.Context(), repoID)
	require.NoError(t, err)
	cfgUnit, err := repo.GetUnit(t.Context(), unit.TypeActions)
	require.NoError(t, err)
	cfgUnit.ActionsConfig().AllowPackagesWrite = allow
	require.NoError(t, repo_model.UpdateRepoUnit(t.Context(), cfgUnit))
}

type permissionsTask struct {
	// Permissions is the `permissions:` of the workflow as YAML, empty for a workflow without one.
	Permissions string
	IsFork      bool
	// AllowPackagesWrite is the repository's Actions setting when the task starts.
	AllowPackagesWrite bool
}

// newPermissionsTask makes the running task 47 the job of the repository, with the permissions that Forgejo stores on
// a task started from a workflow with Permissions, and returns its token.
func newPermissionsTask(t *testing.T, repo *repo_model.Repository, o permissionsTask) *actions_model.ActionTask {
	t.Helper()
	setActionsPackagesWrite(t, repo.ID, o.AllowPackagesWrite)

	task := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionTask{ID: 47})
	task.RepoID = repo.ID
	task.OwnerID = repo.OwnerID
	task.IsForkPullRequest = o.IsFork
	task.Status = actions_model.StatusRunning

	var node yaml.Node
	if o.Permissions != "" {
		var doc yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(o.Permissions), &doc))
		node = *doc.Content[0]
	}
	permissions, err := actions_model.ResolveTokenPermissions(&node, &yaml.Node{}, actions_model.TokenCeiling{
		IsForkPullRequest:  o.IsFork,
		AllowPackagesWrite: o.AllowPackagesWrite,
	})
	require.NoError(t, err)
	task.Permissions = permissions.String()

	task.GenerateToken()
	require.NoError(t, actions_model.UpdateTask(db.DefaultContext, task, "repo_id", "owner_id", "is_fork_pull_request", "status", "permissions", "token", "token_salt", "token_hash", "token_last_eight"))
	return task
}

func storedTaskPermissions(t *testing.T, task *actions_model.ActionTask) map[string]string {
	t.Helper()
	p, err := task.TokenPermissions()
	require.NoError(t, err)
	if p == nil {
		return nil
	}
	got := map[string]string{}
	for scope, level := range p.Scopes {
		got[scope] = level.String()
	}
	return got
}
