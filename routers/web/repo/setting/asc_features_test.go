// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"testing"

	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func featureNames(ctx *context.Context) []string {
	rows, _ := ctx.Data["AscFeatureRows"].([]ascFeatureRow)
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

// TestAscFeaturesAsc checks the Features card of an asc repository: the five switches in board order, the save
// handler turning the asc switches and the units on and off, and the 404 the switched-off pages answer.
func TestAscFeaturesAsc(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ctx, _ := ascRepoCtx(t, 1, 2, "asc")

	ascFeaturesData(ctx)
	assert.Equal(t, []string{"agents", "deps", "releases", "meaning", "wiki"}, featureNames(ctx))
	assert.Equal(t, true, ctx.Data["AscFeatureStacks"])
	for _, r := range ctx.Data["AscFeatureRows"].([]ascFeatureRow) {
		assert.True(t, r.On, r.Name)
	}

	// agents and releases are left out of the form: both go off
	post, resp := contexttest.MockContext(t, "POST /user2/repo1/settings/asc/features?deps=on&meaning=on&wiki=on")
	post.Doer = ctx.Doer
	post.IsSigned = true
	contexttest.LoadRepo(t, post, 1)
	AscFeaturesPost(post)
	assert.Equal(t, http.StatusSeeOther, resp.Code)
	assert.Equal(t, "/user2/repo1/settings#features", resp.Header().Get("Location"))
	assert.NotEmpty(t, post.Flash.SuccessMsg)

	off, err := repo_model.AscFeaturesOff(post, 1)
	require.NoError(t, err)
	assert.Equal(t, map[repo_model.AscFeature]bool{repo_model.AscFeatureAgents: true}, off)
	repo, err := repo_model.GetRepositoryByID(post, 1)
	require.NoError(t, err)
	assert.False(t, repo.UnitEnabled(post, unit_model.TypeReleases))
	assert.True(t, repo.UnitEnabled(post, unit_model.TypeWiki))

	after, _ := contexttest.MockContext(t, "GET /user2/repo1/settings")
	contexttest.LoadRepo(t, after, 1)
	ascFeaturesData(after)
	on := map[string]bool{}
	for _, r := range after.Data["AscFeatureRows"].([]ascFeatureRow) {
		on[r.Name] = r.On
	}
	assert.Equal(t, map[string]bool{"agents": false, "deps": true, "releases": false, "meaning": true, "wiki": true}, on)

	t.Run("a switched-off page answers 404", func(t *testing.T) {
		gate, gateResp := contexttest.MockContext(t, "GET /user2/repo1/agents")
		contexttest.LoadRepo(t, gate, 1)
		context.AscRepoFeature(repo_model.AscFeatureAgents)(gate)
		assert.Equal(t, http.StatusNotFound, gateResp.Code)

		open, openResp := contexttest.MockContext(t, "GET /user2/repo1/asc/deps")
		contexttest.LoadRepo(t, open, 1)
		context.AscRepoFeature(repo_model.AscFeatureDeps)(open)
		assert.False(t, open.Written())
		assert.Equal(t, http.StatusOK, openResp.Code)
	})

	t.Run("turning everything back on", func(t *testing.T) {
		post, _ := contexttest.MockContext(t, "POST /user2/repo1/settings/asc/features?agents=on&deps=on&meaning=on&releases=on&wiki=on")
		contexttest.LoadRepo(t, post, 1)
		AscFeaturesPost(post)
		off, err := repo_model.AscFeaturesOff(post, 1)
		require.NoError(t, err)
		assert.Empty(t, off)
		repo, err := repo_model.GetRepositoryByID(post, 1)
		require.NoError(t, err)
		assert.True(t, repo.UnitEnabled(post, unit_model.TypeReleases))
	})
}

// TestAscFeaturesGitRepo checks that a git repository gets only the unit switches and that a save leaves the asc
// switches alone.
func TestAscFeaturesGitRepo(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ctx, _ := ascRepoCtx(t, 1, 2, "git")
	ascFeaturesData(ctx)
	assert.Equal(t, []string{"releases", "wiki"}, featureNames(ctx))
	assert.Equal(t, false, ctx.Data["AscFeatureStacks"])

	post, _ := contexttest.MockContext(t, "POST /user2/repo1/settings/asc/features?releases=on&wiki=on")
	contexttest.LoadRepo(t, post, 1)
	AscFeaturesPost(post)
	off, err := repo_model.AscFeaturesOff(post, 1)
	require.NoError(t, err)
	assert.Empty(t, off)
}
