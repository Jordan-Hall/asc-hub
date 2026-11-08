// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscCollaborationMaintain(t *testing.T) {
	type Repository struct {
		ID int64 `xorm:"pk autoincr"`
	}
	type User struct {
		ID int64 `xorm:"pk autoincr"`
	}
	type Collaboration struct {
		ID          int64 `xorm:"pk autoincr"`
		RepoID      int64 `xorm:"UNIQUE(s) INDEX NOT NULL REFERENCES(repository, id)"`
		UserID      int64 `xorm:"UNIQUE(s) INDEX NOT NULL REFERENCES(user, id)"`
		Mode        int   `xorm:"DEFAULT 2 NOT NULL"`
		CreatedUnix int64 `xorm:"INDEX created"`
		UpdatedUnix int64 `xorm:"INDEX updated"`
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(Repository), new(User), new(Collaboration))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Insert(&Repository{ID: 1}, &User{ID: 2})
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `collaboration` (`repo_id`, `user_id`, `mode`) VALUES (1, 2, 2)")
	require.NoError(t, err)
	require.NoError(t, addAscCollaborationMaintain(x))
	require.NoError(t, addAscCollaborationMaintain(x))

	var got []bool
	require.NoError(t, x.Table("collaboration").Cols("asc_maintain").Find(&got))
	assert.Equal(t, []bool{false}, got, "a collaboration there before is not a Maintain one")
}
