// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package system_test

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/system"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscDocsFeedbackCountsEachPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := db.DefaultContext
	for _, a := range []struct {
		slug    string
		helpful bool
	}{{"install", true}, {"install", true}, {"install", false}, {"overview", false}} {
		require.NoError(t, system.AddAscDocsFeedback(ctx, a.slug, a.helpful))
	}
	c, err := system.CountAscDocsFeedback(ctx, "install")
	require.NoError(t, err)
	assert.Equal(t, system.AscDocsFeedbackCount{Yes: 2, No: 1}, c)
	c, err = system.CountAscDocsFeedback(ctx, "overview")
	require.NoError(t, err)
	assert.Equal(t, system.AscDocsFeedbackCount{Yes: 0, No: 1}, c)
	c, err = system.CountAscDocsFeedback(ctx, "none")
	require.NoError(t, err)
	assert.Zero(t, c)
}
