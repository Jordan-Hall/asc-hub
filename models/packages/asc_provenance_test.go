// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages_test

import (
	"testing"

	packages_model "forgejo.org/models/packages"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscProvenance(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	got, err := packages_model.GetAscProvenance(ctx, 9001)
	require.NoError(t, err)
	assert.Nil(t, got, "a version uploaded by a person has none")

	want := &packages_model.AscProvenance{RepoID: 4, RunID: 791, Commit: "c2d72f548424103f01ee1dc02889c1e2bff816b0", Ref: "refs/heads/master"}
	require.NoError(t, packages_model.SetAscProvenance(ctx, 9001, want))
	got, err = packages_model.GetAscProvenance(ctx, 9001)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	for i, bad := range []string{
		`not json`,
		`{"repo":0,"run":1,"commit":"c2d72f548424103f01ee1dc02889c1e2bff816b0"}`,
		`{"repo":4,"run":1,"commit":"C2D72F548424103F01EE1DC02889C1E2BFF816B0"}`,
		`{"repo":4,"run":1,"commit":"c2d72f"}`,
		`{"repo":4,"run":1,"commit":"<b>c2d72f548424103f01ee1dc02889c1e2bff816b</b>"}`,
	} {
		id := int64(9100 + i)
		_, err := packages_model.InsertProperty(ctx, packages_model.PropertyTypeVersion, id, packages_model.AscProvenanceProperty, bad)
		require.NoError(t, err)
		got, err := packages_model.GetAscProvenance(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, got, bad)
	}

	sha256 := &packages_model.AscProvenance{RepoID: 4, RunID: 791, Commit: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	require.NoError(t, packages_model.SetAscProvenance(ctx, 9200, sha256))
	got, err = packages_model.GetAscProvenance(ctx, 9200)
	require.NoError(t, err)
	assert.Equal(t, sha256, got, "a sha256 commit")
}
