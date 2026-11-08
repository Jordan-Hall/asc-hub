// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	repo_service "forgejo.org/services/repository"
)

// ascFeatureRow is one switch of the Features card (board RepoSettingsGeneral).
type ascFeatureRow struct {
	Name     string // the form field
	Label    string // locale key of the name
	Hint     string // locale key of the line under it
	On       bool
	Disabled bool // the site switched the unit off for every repository
}

// ascFeaturesData fills AscFeatureRows for the settings page: agent runs, dependencies and meaning search for an
// asc repository, releases and the wiki for every repository.
func ascFeaturesData(ctx *context.Context) {
	repo := ctx.Repo.Repository
	isAsc := setting.Asc.Enabled && repo.GetStorageBackend() == "asc"
	off := map[repo_model.AscFeature]bool{}
	if isAsc {
		var err error
		if off, err = repo_model.AscFeaturesOff(ctx, repo.ID); err != nil {
			ctx.ServerError("AscFeaturesOff", err)
			return
		}
	}
	releases := ascFeatureRow{Name: "releases", Label: "repo.releases", Hint: "asc_rp.feat_releases_hint", On: repo.UnitEnabled(ctx, unit_model.TypeReleases), Disabled: unit_model.TypeReleases.UnitGlobalDisabled()}
	wiki := ascFeatureRow{Name: "wiki", Label: "repo.wiki", Hint: "asc_rp.feat_wiki_hint", On: repo.UnitEnabled(ctx, unit_model.TypeWiki) || repo.UnitEnabled(ctx, unit_model.TypeExternalWiki), Disabled: unit_model.TypeWiki.UnitGlobalDisabled()}
	rows := []ascFeatureRow{releases, wiki}
	if isAsc {
		// board order: Agent runs, Dependencies, Releases, Meaning search, Wiki
		rows = []ascFeatureRow{
			{Name: "agents", Label: "asc_rp.feat_agents", Hint: "asc_rp.feat_agents_hint", On: !off[repo_model.AscFeatureAgents]},
			{Name: "deps", Label: "asc_rp.feat_deps", Hint: "asc_rp.feat_deps_hint", On: !off[repo_model.AscFeatureDeps]},
			releases,
			{Name: "meaning", Label: "asc_rp.feat_meaning", Hint: "asc_rp.feat_meaning_hint", On: !off[repo_model.AscFeatureMeaning]},
			wiki,
		}
	}
	ctx.Data["AscFeatureRows"] = rows
	ctx.Data["AscFeatureStacks"] = isAsc
}

// AscFeaturesPost saves the Features card. A switch that is left out of the form is off. The wiki switch keeps an
// external wiki that is set up and only turns the wiki off or, when there is none, on as a wiki in the repository.
func AscFeaturesPost(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if setting.Asc.Enabled && repo.GetStorageBackend() == "asc" {
		for _, f := range repo_model.AscFeatures {
			if err := repo_model.SetAscFeature(ctx, repo.ID, f, ctx.FormString(string(f)) == "on"); err != nil {
				ctx.ServerError("SetAscFeature", err)
				return
			}
		}
	}

	var units []repo_model.RepoUnit
	var drop []unit_model.Type
	if !unit_model.TypeReleases.UnitGlobalDisabled() {
		on := ctx.FormString("releases") == "on"
		if had := repo.UnitEnabled(ctx, unit_model.TypeReleases); on && !had {
			units = append(units, repo_model.RepoUnit{RepoID: repo.ID, Type: unit_model.TypeReleases})
		} else if !on && had {
			drop = append(drop, unit_model.TypeReleases)
		}
	}
	if !unit_model.TypeWiki.UnitGlobalDisabled() {
		on := ctx.FormString("wiki") == "on"
		internal, external := repo.UnitEnabled(ctx, unit_model.TypeWiki), repo.UnitEnabled(ctx, unit_model.TypeExternalWiki)
		if on && !internal && !external {
			units = append(units, repo_model.RepoUnit{RepoID: repo.ID, Type: unit_model.TypeWiki, Config: new(repo_model.UnitConfig)})
		} else if !on && (internal || external) {
			drop = append(drop, unit_model.TypeWiki, unit_model.TypeExternalWiki)
		}
	}
	if len(units) > 0 || len(drop) > 0 {
		if err := repo_service.UpdateRepositoryUnits(ctx, repo, units, drop); err != nil {
			ctx.ServerError("UpdateRepositoryUnits", err)
			return
		}
	}
	ctx.Flash.Success(ctx.Tr("repo.settings.update_settings_success"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings#features")
}
