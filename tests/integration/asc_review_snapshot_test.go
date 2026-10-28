// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"forgejo.org/tests"

	"github.com/stretchr/testify/require"
)

// Page snapshots of the review pages that come from the stock fixtures (the compare form, a pull request, its files
// and its commits), for the design review. Set ASC_REVIEW_SNAPSHOT_DIR to a directory and run this test: every page is
// requested as user2 and written as <name>.html, ready to be opened with the built stylesheet. Without the variable it
// does nothing.
func TestAscReviewSnapshotFixturePages(t *testing.T) {
	dir := os.Getenv("ASC_REVIEW_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set ASC_REVIEW_SNAPSHOT_DIR to write page snapshots")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	for name, path := range map[string]string{
		"compare-pr":        "/user2/repo1/compare/master...branch2",
		"compare-nothing":   "/user2/repo1/compare/master...master",
		"compare-tag":       "/user2/repo1/compare/v1.1...master",
		"pull":              "/user2/repo1/pulls/3",
		"pull3-files":       "/user2/repo1/pulls/3/files",
		"pull3-split":       "/user2/repo1/pulls/3/files?style=split",
		"pull3-commits":     "/user2/repo1/pulls/3/commits",
		"compare-diff":      "/user2/repo1/compare/master...branch1",
		"pull-files":        "/user2/repo1/pulls/2/files",
		"pull-commits":      "/user2/repo1/pulls/2/commits",
		"pull-files-split":  "/user2/repo1/pulls/2/files?style=split",
		"pulls-list":        "/user2/repo1/pulls",
		"commit-diff":       "/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d",
		"diff-compare-open": "/user2/repo1/compare/master...pr-existing",
	} {
		req := NewRequest(t, "GET", path)
		resp := session.MakeRequest(t, req, NoExpectedStatus)
		if resp.Code != http.StatusOK {
			t.Logf("snapshot %s: %s answered %d", name, path, resp.Code)
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), resp.Body.Bytes(), 0o644))
	}
}

// TestAscReviewSnapshotStackPages writes the pages of a stacked change (two linked pull requests with a faked asc):
// the conversation, the files, and the confirmation of each stack operation.
func TestAscReviewSnapshotStackPages(t *testing.T) {
	dir := os.Getenv("ASC_REVIEW_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set ASC_REVIEW_SNAPSHOT_DIR to write page snapshots")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	defer tests.PrepareTestEnv(t)()
	newAscQueueEnv(t)

	session := loginUser(t, "user2")
	for name, path := range map[string]string{
		"stack-pull":    "/user2/repo1/pulls/5",
		"stack-files":   "/user2/repo1/pulls/5/files",
		"stack-queue":   "/user2/repo1/pulls/5/asc/confirm?op=queue",
		"stack-merge":   "/user2/repo1/pulls/5/asc/confirm?op=merge",
		"stack-restack": "/user2/repo1/pulls/5/asc/confirm?op=restack",
		"stack-bottom":  "/user2/repo1/pulls/2",
	} {
		resp := session.MakeRequest(t, NewRequest(t, "GET", path), NoExpectedStatus)
		if resp.Code != http.StatusOK {
			t.Logf("snapshot %s: %s answered %d", name, path, resp.Code)
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), resp.Body.Bytes(), 0o644))
	}
}
