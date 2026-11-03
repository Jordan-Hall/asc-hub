// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages_test

import (
	"testing"

	"forgejo.org/models/db"
	packages_model "forgejo.org/models/packages"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/optional"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountPackagesByType(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	add := func(name string, typ packages_model.Type, versions ...string) {
		p, err := packages_model.TryInsertPackage(db.DefaultContext, &packages_model.Package{OwnerID: 2, Name: name, LowerName: name, Type: typ})
		require.NoError(t, err)
		for _, v := range versions {
			_, err := packages_model.GetOrInsertVersion(db.DefaultContext, &packages_model.PackageVersion{PackageID: p.ID, Version: v, LowerVersion: v})
			require.NoError(t, err)
		}
	}
	add("nova-client", packages_model.TypeCargo, "1.0.0", "1.1.0")
	add("nova-sdk", packages_model.TypeNpm, "4.2.1")
	add("orbit-sdk", packages_model.TypeNpm, "0.1.0")
	add("nova-server", packages_model.TypeContainer, "2.0.0")
	add("nova-empty", packages_model.TypeGeneric)

	opts := &packages_model.PackageSearchOptions{OwnerID: 2, Type: packages_model.TypeNpm, IsInternal: optional.Some(false)}
	counts, err := packages_model.CountPackagesByType(db.DefaultContext, opts)
	require.NoError(t, err)
	assert.Equal(t, []packages_model.TypeCount{
		{Type: packages_model.TypeNpm, Count: 2},
		{Type: packages_model.TypeCargo, Count: 1},
		{Type: packages_model.TypeContainer, Count: 1},
	}, counts, "the most packages first; the chosen type does not narrow the chips; a package without versions is not listed")
	assert.Equal(t, packages_model.TypeNpm, opts.Type, "the options are not changed")

	opts.Name = packages_model.SearchValue{Value: "nova"}
	counts, err = packages_model.CountPackagesByType(db.DefaultContext, opts)
	require.NoError(t, err)
	assert.Equal(t, []packages_model.TypeCount{
		{Type: packages_model.TypeCargo, Count: 1},
		{Type: packages_model.TypeContainer, Count: 1},
		{Type: packages_model.TypeNpm, Count: 1},
	}, counts)
}
