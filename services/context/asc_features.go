// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
)

// ascFeatureData sets AscAgentsOff, AscDepsOff and AscMeaningOff: the switches of the Features card (board
// RepoSettingsGeneral) an administrator turned off. A failed read leaves every switch on: the pages check again.
func ascFeatureData(ctx *Context, repo *repo_model.Repository) {
	off, err := repo_model.AscFeaturesOff(ctx, repo.ID)
	if err != nil {
		log.Error("AscFeaturesOff(%d): %v", repo.ID, err)
	}
	ctx.Data["AscAgentsOff"] = off[repo_model.AscFeatureAgents]
	ctx.Data["AscDepsOff"] = off[repo_model.AscFeatureDeps]
	ctx.Data["AscMeaningOff"] = off[repo_model.AscFeatureMeaning]
}

// AscRepoFeature answers 404 for every page of a switch that is off for the repository, as if the page did not
// exist; it runs after RepoAssignment.
func AscRepoFeature(f repo_model.AscFeature) func(ctx *Context) {
	return func(ctx *Context) {
		if ctx.Repo.Repository == nil {
			return
		}
		on, err := repo_model.AscFeatureEnabled(ctx, ctx.Repo.Repository.ID, f)
		if err != nil {
			ctx.ServerError("AscFeatureEnabled", err)
			return
		}
		if !on {
			ctx.NotFound("AscRepoFeature", nil)
		}
	}
}
