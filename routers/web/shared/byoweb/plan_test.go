// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	quota_model "forgejo.org/models/quota"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoStoragePlan(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := setup(t)
	ctx := db.DefaultContext
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	lvl := repoLevel(t, true)
	lvl.Repo = repo

	t.Cleanup(test.MockVariableValue(&setting.Quota.Enabled, true))
	_, err := quota_model.CreateRule(ctx, "asc-plan", 100<<30, quota_model.LimitSubjects{quota_model.LimitSubjectSizeGitAll})
	require.NoError(t, err)
	g, err := quota_model.CreateGroup(ctx, "asc-plan")
	require.NoError(t, err)
	require.NoError(t, g.AddRuleByName(ctx, "asc-plan"))
	require.NoError(t, g.AddUserByID(ctx, repo.OwnerID))

	counted := func(target string, fallback *string) *ascv1.GetRepoStorageUsageResponse {
		r := &ascv1.GetRepoStorageUsageResponse{
			Rows: []*ascv1.StorageUsageRow{
				{Kind: ascv1.StorageUsageKind_STORAGE_USAGE_KIND_HISTORY, Bytes: 8 << 30, Objects: 10},
				{Kind: ascv1.StorageUsageKind_STORAGE_USAGE_KIND_LARGE_FILES, Bytes: 30 << 30, Objects: 4},
			},
			ComputedAt: time.Now().Unix(), Month: "2026-11", TargetId: target,
		}
		if fallback != nil {
			r.HasFallback, r.FallbackTargetId = true, *fallback
		}
		return r
	}
	reload := func() *repo_model.Repository {
		return unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repo.ID})
	}
	planText := func(t *testing.T) string {
		used, err := quota_model.GetUsedForUser(ctx, repo.OwnerID)
		require.NoError(t, err)
		l := locale(t)
		return tr(t, "ascbyo.storage.usage.plan", l.TrSize(used.CalculateFor(quota_model.LimitSubjectSizeGitAll)), l.TrSize(100<<30))
	}

	t.Run("on the server's storage: the size is recorded and the plan shown", func(t *testing.T) {
		f.RepoUsage = counted("", nil)
		r := run(t, StorageIndex, lvl, http.MethodGet, nil, "")
		require.Equal(t, http.StatusOK, r.resp.Code)
		got := reload()
		assert.Equal(t, int64(8<<30), got.GitSize)
		assert.Equal(t, int64(30<<30), got.LFSSize)
		body := r.body()
		assert.Contains(t, body, `data-testid="asc-usage-plan"`)
		assert.Contains(t, body, planText(t))
		assert.Contains(t, body, tr(t, "ascbyo.storage.usage.own_bucket"))
		assert.Contains(t, body, `href="#asc-add-h" data-testid="asc-usage-own"`, "no target to use yet: add one")
	})

	t.Run("over 80% of the plan the meter warns", func(t *testing.T) {
		f.RepoUsage = counted("", nil)
		f.RepoUsage.Rows[1].Bytes = 85 << 30
		body := run(t, StorageIndex, lvl, http.MethodGet, nil, "").body()
		assert.Contains(t, body, `class="asc-meter asc-meter-warn"`)
	})

	t.Run("on its own bucket: nothing on the plan", func(t *testing.T) {
		f.RepoUsage = counted("st_own", nil)
		body := run(t, StorageIndex, lvl, http.MethodGet, nil, "").body()
		assert.NotContains(t, body, `data-testid="asc-usage-plan"`)
		got := reload()
		assert.Zero(t, got.GitSize)
		assert.Zero(t, got.LFSSize)
	})

	t.Run("while objects move the size is left as it is", func(t *testing.T) {
		require.NoError(t, repo_model.UpdateRepoSize(ctx, repo.ID, 5, 6))
		def := ""
		f.RepoUsage = counted("st_own", &def)
		body := run(t, StorageIndex, lvl, http.MethodGet, nil, "").body()
		assert.NotContains(t, body, `data-testid="asc-usage-plan"`, "part of it is on its own bucket already")
		got := reload()
		assert.Equal(t, int64(5), got.GitSize)
		assert.Equal(t, int64(6), got.LFSSize)
	})

	t.Run("quotas off: no plan", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Quota.Enabled, false)()
		f.RepoUsage = counted("", nil)
		body := run(t, StorageIndex, lvl, http.MethodGet, nil, "").body()
		assert.NotContains(t, body, `data-testid="asc-usage-plan"`)
	})

	t.Run("no limit on the plan: no plan line", func(t *testing.T) {
		_, err := quota_model.CreateRule(ctx, "asc-unlimited", -1, quota_model.LimitSubjects{quota_model.LimitSubjectSizeAll})
		require.NoError(t, err)
		u, err := quota_model.CreateGroup(ctx, "asc-unlimited")
		require.NoError(t, err)
		require.NoError(t, u.AddRuleByName(ctx, "asc-unlimited"))
		require.NoError(t, u.AddUserByID(ctx, repo.OwnerID))
		defer func() { require.NoError(t, quota_model.DeleteGroupByName(ctx, "asc-unlimited")) }()
		f.RepoUsage = counted("", nil)
		body := run(t, StorageIndex, lvl, http.MethodGet, nil, "").body()
		assert.NotContains(t, body, `data-testid="asc-usage-plan"`)
	})
}

func TestRepoStorageAssignShowsTheCurrentTarget(t *testing.T) {
	f := setup(t)
	lvl := repoLevel(t, false)
	other := f.AddTarget(lvl.Scope.Owner(), "other", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	cur := f.AddTarget(lvl.Scope.Owner(), "current", ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED)
	f.RepoUsage = &ascv1.GetRepoStorageUsageResponse{ComputedAt: time.Now().Unix(), TargetId: cur.GetId()}
	body := run(t, StorageIndex, lvl, http.MethodGet, nil, "").body()
	assert.Contains(t, body, `<option value="`+cur.GetId()+`" selected>`)
	assert.Contains(t, body, `<option value="`+other.GetId()+`">`)
	assert.NotContains(t, body, `data-testid="asc-usage-plan"`, "not on the server's storage")
}
