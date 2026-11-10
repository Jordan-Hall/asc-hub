// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A new asc repository is offered master as its trunk, as asc init and the docs say; a git one keeps DEFAULT_BRANCH.
func TestNewRepoTrunkIsMasterForAsc(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Repository.DefaultBranch, "main")()
	create := func() map[string]any {
		ctx, _ := contexttest.MockContext(t, "repo/create")
		contexttest.LoadUser(t, ctx, 2)
		Create(ctx)
		return ctx.Data
	}
	assert.Equal(t, "main", create()["default_branch"])

	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&setting.Asc.Writer, setting.AscWriterAsc)()
	defer test.MockVariableValue(&setting.Asc.NewRepositories, setting.AscWriterAsc)()
	d := create()
	assert.Equal(t, true, d["NewRepoIsAsc"])
	assert.Equal(t, "master", d["default_branch"])
}
