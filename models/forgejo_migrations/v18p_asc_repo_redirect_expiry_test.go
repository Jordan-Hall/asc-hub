// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"
	"time"

	migration_tests "forgejo.org/models/gitea_migrations/test"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addRepoRedirectExpiry(t *testing.T) {
	type RepoRedirect struct {
		ID             int64  `xorm:"pk autoincr"`
		OwnerID        int64  `xorm:"UNIQUE(s)"`
		LowerName      string `xorm:"UNIQUE(s) INDEX NOT NULL"`
		RedirectRepoID int64
	}
	x, deferable := migration_tests.PrepareTestEnv(t, 0, new(RepoRedirect))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	_, err := x.Exec("INSERT INTO `repo_redirect` (`owner_id`, `lower_name`, `redirect_repo_id`) VALUES (1, 'old', 2)")
	require.NoError(t, err)

	require.NoError(t, addRepoRedirectExpiry(x))
	require.NoError(t, addRepoRedirectExpiry(x))

	// the redirects made before keep redirecting for 90 days from the upgrade
	var expires int64
	has, err := x.SQL("SELECT `expires_unix` FROM `repo_redirect` WHERE `lower_name` = 'old'").Get(&expires)
	require.NoError(t, err)
	require.True(t, has)
	want := timeutil.TimeStampNow().AddDuration(90 * 24 * time.Hour)
	assert.InDelta(t, int64(want), expires, 60)
}
