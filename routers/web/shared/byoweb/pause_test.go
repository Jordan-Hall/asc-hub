// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoStorageMigrationPause(t *testing.T) {
	f := setup(t)
	repo := repoLevel(t, false)
	m := f.AddMigration(&ascv1.StorageMigration{
		Repo: &ascv1.RepoRef{Name: "alice/widgets"}, State: ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING,
		ObjectsTotal: 100, ObjectsCopied: 64,
	})
	id := m.GetId()
	base := "/alice/widgets/settings/asc/storage/migrations/" + id

	t.Run("a running migration offers Pause and Cancel and keep current", func(t *testing.T) {
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.Contains(t, body, `value="64"`)
		assert.Contains(t, body, `action="`+base+`/pause"`)
		assert.Contains(t, body, `data-testid="asc-migration-pause"`)
		assert.Contains(t, body, tr(t, "ascbyo.storage.repo.cancel"))
		assert.NotContains(t, body, `data-testid="asc-migration-resume"`)
		assert.NotContains(t, body, `data-testid="asc-migration-paused"`)
	})

	t.Run("pause holds it and the page offers Resume", func(t *testing.T) {
		r := run(t, MigrationPause, repo, http.MethodPost, nil, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		require.Len(t, f.CallsTo("PauseMigration"), 1)
		assert.True(t, m.GetPaused())
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.Contains(t, body, tr(t, "ascbyo.migration.state.paused"))
		assert.Contains(t, body, `data-testid="asc-migration-paused"`)
		assert.Contains(t, body, `action="`+base+`/resume"`)
		assert.NotContains(t, body, `data-testid="asc-migration-pause"`)
		assert.Contains(t, body, `action="`+base+`/cancel"`, "a paused migration can still be cancelled")
		assert.NotContains(t, body, `data-testid="asc-migrate"`, "still the repository's active migration")

		r = run(t, MigrationStatus, repo, http.MethodGet, nil, id)
		var st migrationStatus
		contexttest.DecodeJSON(t, r.resp, &st)
		assert.True(t, st.Active)
		assert.True(t, st.Paused)
		assert.Equal(t, "copying", st.State)
		assert.Equal(t, tr(t, "ascbyo.migration.state.paused"), st.Label)
	})

	t.Run("resume carries on", func(t *testing.T) {
		r := run(t, MigrationResume, repo, http.MethodPost, nil, id)
		require.Equal(t, http.StatusSeeOther, r.resp.Code, r.body())
		assert.False(t, m.GetPaused())
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.Contains(t, body, `data-testid="asc-migration-pause"`)
	})

	t.Run("once it is switching there is nothing to pause", func(t *testing.T) {
		f.UpdateMigration(id, func(m *ascv1.StorageMigration) {
			m.State = ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_SWITCHING
		})
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.NotContains(t, body, `data-testid="asc-migration-pause"`)
		assert.NotContains(t, body, `action="`+base+`/cancel"`)
		r := run(t, MigrationPause, repo, http.MethodPost, nil, id)
		assert.NotEqual(t, http.StatusSeeOther, r.resp.Code)
		assert.False(t, m.GetPaused())
	})

	t.Run("a migration of another repository is not reachable", func(t *testing.T) {
		other := f.AddMigration(&ascv1.StorageMigration{Repo: &ascv1.RepoRef{Name: "mallory/secret"}, State: ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING})
		calls := len(f.CallsTo("PauseMigration"))
		r := run(t, MigrationPause, repo, http.MethodPost, nil, other.GetId())
		assert.Equal(t, http.StatusNotFound, r.resp.Code)
		assert.False(t, other.GetPaused())
		assert.Len(t, f.CallsTo("PauseMigration"), calls)
	})

	t.Run("not at the user level", func(t *testing.T) {
		calls := len(f.Calls())
		for name, fn := range map[string]func(*context.Context, *Level){"pause": MigrationPause, "resume": MigrationResume} {
			r := run(t, fn, userLevel(t), http.MethodPost, nil, id)
			assert.Equal(t, http.StatusNotFound, r.resp.Code, name)
		}
		assert.Len(t, f.Calls(), calls)
	})
}
