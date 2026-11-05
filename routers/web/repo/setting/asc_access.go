// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// ascAgentScope says what the policy lets the agents of the repository write, for the agent rows of the access page
// (board RepoSettingsAccess: "Limited by policy to src/ and tests/"). asc is asked only for an asc repository with an
// agent among its collaborators. When it cannot answer the rows say nothing more: the role still applies.
func ascAgentScope(ctx *context.Context, users []*repo_model.Collaborator) *asctrust.AgentScope {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo.StorageBackend != "asc" || !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		return nil
	}
	agents := false
	for _, u := range users {
		agents = agents || u.IsBot()
	}
	if !agents {
		return nil
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc access: no connection to asc serve: %v", err)
		return nil
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		log.Warn("asc access: credential: %v", err)
		return nil
	}
	defer release()
	scope, err := asctrust.LoadAgentScope(callCtx, conn, name)
	if err != nil {
		log.Warn("asc access: agent scope of %s: %v", name, err)
		return nil
	}
	if scope.Paths == "" && !scope.Off {
		return nil
	}
	return &scope
}
