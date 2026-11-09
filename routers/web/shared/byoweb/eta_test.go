// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

func TestRepoStorageMigrationBytesAndETA(t *testing.T) {
	f := setup(t)
	repo := repoLevel(t, false)
	m := f.AddMigration(&ascv1.StorageMigration{
		Repo: &ascv1.RepoRef{Name: "alice/widgets"}, State: ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_COPYING,
		Created: 1000, Updated: 1600, ObjectsTotal: 100, ObjectsCopied: 50, BytesTotal: 2 << 20, BytesCopied: 1 << 20,
	})
	id := m.GetId()
	progress := tr(t, "ascbyo.storage.repo.progress", 50, 100) + " \u00b7 " +
		tr(t, "ascbyo.storage.repo.progress_bytes", "1 MiB", "2 MiB") + " \u00b7 " + tr(t, "ascbyo.storage.repo.eta_minutes_n", 10)

	t.Run("the page shows the bytes and the time left", func(t *testing.T) {
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.Contains(t, body, progress)
		assert.Contains(t, body, "about 10 minutes left")
		assert.Contains(t, body, `data-testid="asc-migration-redone" hidden`, "nothing was copied again yet")
	})

	t.Run("the status carries the same line for the script", func(t *testing.T) {
		var st migrationStatus
		contexttest.DecodeJSON(t, run(t, MigrationStatus, repo, http.MethodGet, nil, id).resp, &st)
		assert.Equal(t, progress, st.Progress)
		assert.EqualValues(t, 600, st.ETASeconds)
		assert.EqualValues(t, 1<<20, st.BytesCopied)
		assert.EqualValues(t, 2<<20, st.BytesTotal)
		assert.Empty(t, st.Redone)
	})

	t.Run("the check reports the copies it redid, and no estimate", func(t *testing.T) {
		f.UpdateMigration(id, func(m *ascv1.StorageMigration) {
			m.State = ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_VERIFYING
			m.Mismatched = 2
		})
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.Contains(t, body, tr(t, "ascbyo.storage.repo.redone_n", 2))
		assert.NotContains(t, body, `data-testid="asc-migration-redone" hidden`)
		assert.NotContains(t, body, "minutes left")
		var st migrationStatus
		contexttest.DecodeJSON(t, run(t, MigrationStatus, repo, http.MethodGet, nil, id).resp, &st)
		assert.Equal(t, tr(t, "ascbyo.storage.repo.redone_n", 2), st.Redone)
		assert.Zero(t, st.ETASeconds)
	})

	t.Run("the history keeps the count once it is done", func(t *testing.T) {
		f.UpdateMigration(id, func(m *ascv1.StorageMigration) {
			m.State = ascv1.StorageMigrationState_STORAGE_MIGRATION_STATE_DONE
			m.Mismatched = 1
		})
		body := run(t, StorageIndex, repo, http.MethodGet, nil, "").body()
		assert.Contains(t, body, `data-testid="asc-history-redone">`+tr(t, "ascbyo.storage.repo.redone_1", 1))
		assert.NotContains(t, body, `data-testid="asc-migration-active"`)
	})
}
