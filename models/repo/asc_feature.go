// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"slices"

	"forgejo.org/models/db"
)

// AscFeature names one switch of the Features card in the repository settings (board RepoSettingsGeneral) that is
// not a Forgejo unit. Releases and the wiki are units; stacks and change review cannot be turned off.
type AscFeature string

const (
	AscFeatureAgents  AscFeature = "agents"  // agent runs: the Agents pages, new runs and their tokens
	AscFeatureDeps    AscFeature = "deps"    // dependencies: the scan, findings and update proposals
	AscFeatureMeaning AscFeature = "meaning" // meaning search: the semantic mode of code search
)

// AscFeatures is every switch, in the order of the Features card.
var AscFeatures = []AscFeature{AscFeatureAgents, AscFeatureDeps, AscFeatureMeaning}

// Valid reports whether f is a known switch.
func (f AscFeature) Valid() bool { return slices.Contains(AscFeatures, f) }

// AscRepoFeatureOff is a switch an administrator turned off. Every switch is on until a row says otherwise, so a
// repository made before the switches existed keeps everything it had.
type AscRepoFeatureOff struct {
	ID      int64  `xorm:"pk autoincr"`
	RepoID  int64  `xorm:"UNIQUE(repo_feature) NOT NULL"`
	Feature string `xorm:"UNIQUE(repo_feature) VARCHAR(32) NOT NULL"`
}

func init() {
	db.RegisterModel(new(AscRepoFeatureOff))
}

// TableName is the table of the model.
func (AscRepoFeatureOff) TableName() string { return "asc_repo_feature_off" }

// AscFeaturesOff returns the switches that are off for the repository.
func AscFeaturesOff(ctx context.Context, repoID int64) (map[AscFeature]bool, error) {
	var rows []AscRepoFeatureOff
	if err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Find(&rows); err != nil {
		return nil, err
	}
	off := make(map[AscFeature]bool, len(rows))
	for _, r := range rows {
		off[AscFeature(r.Feature)] = true
	}
	return off, nil
}

// AscFeatureEnabled reports whether the switch is on for the repository.
func AscFeatureEnabled(ctx context.Context, repoID int64, f AscFeature) (bool, error) {
	has, err := db.GetEngine(ctx).Exist(&AscRepoFeatureOff{RepoID: repoID, Feature: string(f)})
	return !has, err
}

// SetAscFeature turns the switch on or off for the repository. Setting it to what it already is changes nothing.
func SetAscFeature(ctx context.Context, repoID int64, f AscFeature, on bool) error {
	if on {
		_, err := db.GetEngine(ctx).Delete(&AscRepoFeatureOff{RepoID: repoID, Feature: string(f)})
		return err
	}
	enabled, err := AscFeatureEnabled(ctx, repoID, f)
	if err != nil || !enabled {
		return err
	}
	_, err = db.GetEngine(ctx).Insert(&AscRepoFeatureOff{RepoID: repoID, Feature: string(f)})
	return err
}
