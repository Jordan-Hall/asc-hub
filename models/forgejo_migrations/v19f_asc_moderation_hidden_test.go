// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"testing"

	migration_tests "forgejo.org/models/gitea_migrations/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_addAscModerationHidden(t *testing.T) {
	x, deferable := migration_tests.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, addAscModerationHidden(x))
	require.NoError(t, addAscModerationHidden(x), "running it again changes nothing")

	_, err := x.Exec("INSERT INTO `asc_moderation_hidden` (`content_type`, `content_id`, `saved`, `moderator_id`, `created_unix`) VALUES (2, 7, '{}', 1, 1)")
	require.NoError(t, err)
	_, err = x.Exec("INSERT INTO `asc_moderation_hidden` (`content_type`, `content_id`, `saved`, `moderator_id`, `created_unix`) VALUES (2, 7, '{}', 1, 2)")
	require.Error(t, err, "one hide per content")
	_, err = x.Exec("INSERT INTO `asc_moderation_hidden` (`content_type`, `content_id`, `saved`, `moderator_id`, `created_unix`) VALUES (3, 7, '{}', 1, 2)")
	require.NoError(t, err, "the same id of another kind of content")
	n, err := x.Table("asc_moderation_hidden").Count()
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)
}
