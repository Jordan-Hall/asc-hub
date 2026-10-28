// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLicenseFileName(t *testing.T) {
	assert.Equal(t, "LICENSE", licenseFileName([]string{"README.md", "LICENSE"}))
	assert.Equal(t, "License.md", licenseFileName([]string{"Cargo.toml", "License.md"}))
	assert.Equal(t, "COPYING", licenseFileName([]string{"COPYING"}))
	assert.Empty(t, licenseFileName([]string{"README.md", "licenses-list.txt"}))
	assert.Empty(t, licenseFileName(nil))
}

func TestHomeStackOfNeedsPullAccess(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.NotEmpty(t, pr.MergedCommitID, "the fixture is a merged pull request")

	name, link := homeStackOf(t.Context(), pr.BaseRepoID, "/user2/repo1", pr.MergedCommitID, false)
	assert.Empty(t, name, "code-only access sees no pull request data")
	assert.Empty(t, link)

	name, link = homeStackOf(t.Context(), pr.BaseRepoID, "/user2/repo1", pr.MergedCommitID, true)
	assert.Equal(t, pr.HeadBranch, name)
	assert.Equal(t, fmt.Sprintf("/user2/repo1/pulls/%d", pr.Index), link)

	name, _ = homeStackOf(t.Context(), pr.BaseRepoID, "/user2/repo1", "0000000000000000000000000000000000000000", true)
	assert.Empty(t, name, "a commit that landed through no pull request has no stack")
}
