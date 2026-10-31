// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package container

import (
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/packages"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewRulesCountsWhatARuleWouldDeleteAndDeletesNothing(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	none, err := PreviewRules(db.DefaultContext)
	require.NoError(t, err)
	assert.Empty(t, none, "no rule is enabled")

	// a package of the owner with three old versions, and a rule that keeps nothing and removes what is older than a day
	owner, typ := int64(2), packages.TypeGeneric
	pkg := &packages.Package{OwnerID: owner, Type: typ, Name: "preview", LowerName: "preview"}
	_, err = db.GetEngine(db.DefaultContext).Insert(pkg)
	require.NoError(t, err)
	for _, v := range []string{"1.0", "1.1", "1.2"} {
		_, err = db.GetEngine(db.DefaultContext).NoAutoTime().Insert(&packages.PackageVersion{PackageID: pkg.ID, CreatorID: owner, Version: v, LowerVersion: v, CreatedUnix: 1_000_000})
		require.NoError(t, err)
	}
	before, err := db.GetEngine(db.DefaultContext).Count(new(packages.PackageVersion))
	require.NoError(t, err)
	rule, err := packages.InsertCleanupRule(db.DefaultContext, &packages.PackageCleanupRule{Enabled: true, OwnerID: owner, Type: typ, RemoveDays: 1})
	require.NoError(t, err)

	got, err := PreviewRules(db.DefaultContext)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, rule.ID, got[0].Rule.ID)
	assert.NotEmpty(t, got[0].Owner)
	assert.Empty(t, got[0].Err)
	targets, err := GetCleanupTargets(db.DefaultContext, rule, true)
	require.NoError(t, err)
	assert.Equal(t, len(targets), got[0].Versions, "the preview counts what the run would delete")
	assert.Equal(t, 3, got[0].Versions)
	assert.Equal(t, 1, got[0].Packages)

	after, err := db.GetEngine(db.DefaultContext).Count(new(packages.PackageVersion))
	require.NoError(t, err)
	assert.Equal(t, before, after, "looking deletes nothing")
}
