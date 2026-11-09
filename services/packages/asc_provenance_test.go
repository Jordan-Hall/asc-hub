// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"context"
	"testing"

	actions_model "forgejo.org/models/actions"
	packages_model "forgejo.org/models/packages"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A version published by an Actions job keeps the repository, the run and the commit of the job; one uploaded without a
// task keeps none, and a task that is gone leaves the version without provenance.
func TestRecordAscProvenance(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	run := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionRun{ID: 791})

	recordAscProvenance(context.WithValue(t.Context(), PublishTaskKey, int64(47)), &packages_model.PackageVersion{ID: 9301})
	got, err := packages_model.GetAscProvenance(t.Context(), 9301)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(4), got.RepoID)
	assert.Equal(t, int64(791), got.RunID)
	assert.Equal(t, "c2d72f548424103f01ee1dc02889c1e2bff816b0", got.Commit)
	assert.Equal(t, run.Ref, got.Ref)

	recordAscProvenance(t.Context(), &packages_model.PackageVersion{ID: 9302})
	recordAscProvenance(context.WithValue(t.Context(), PublishTaskKey, "47"), &packages_model.PackageVersion{ID: 9303})
	recordAscProvenance(context.WithValue(t.Context(), PublishTaskKey, int64(999999)), &packages_model.PackageVersion{ID: 9304})
	for _, id := range []int64{9302, 9303, 9304} {
		pps, err := packages_model.GetPropertiesByName(t.Context(), packages_model.PropertyTypeVersion, id, packages_model.AscProvenanceProperty)
		require.NoError(t, err)
		assert.Empty(t, pps, id)
	}
}
