// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascbyo/ascbyotest"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The repositories page of the admin says where each repository keeps its objects and warns when that disagrees with
// where they are.
func TestAdminReposStorageColumn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	f := ascbyotest.Start(t)
	enabled := ascv1.StorageTargetState_STORAGE_TARGET_STATE_ENABLED
	f.Placements = map[string]*ascv1.RepoPlacement{
		"user2/repo1":  {TargetId: "st_AAAAAAAAAAAAAAAAAAAA", TargetName: "garage-eu", TargetState: enabled},
		"user2/repo15": {HasFallback: true},
		"user2/repo16": {TargetId: "st_BBBBBBBBBBBBBBBBBBBB", TargetName: "garage-us", TargetState: enabled, HasFallback: true, Migrating: true},
		"user2/repo20": {TargetId: "st_CCCCCCCCCCCCCCCCCCCC", TargetName: "old", TargetState: ascv1.StorageTargetState_STORAGE_TARGET_STATE_DISABLED},
	}

	session := loginUser(t, "user1")
	rows := func(q string) map[string]*goquery.Selection {
		doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/repos?q="+q), http.StatusOK).Body)
		assert.Contains(t, doc.Find(".ds-table thead").Text(), "Storage")
		assert.NotContains(t, doc.Find(".ds-table thead").Text(), "Issues")
		out := map[string]*goquery.Selection{}
		doc.Find(".ds-table tbody tr").Each(func(_ int, row *goquery.Selection) {
			out[strings.TrimSpace(row.Find(".adm-who-name").Text())] = row.Find(`[data-testid="repo-storage"]`)
		})
		return out
	}
	text := func(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

	got := rows("repo1")
	assert.Equal(t, "garage-eu", text(got["user2/repo1"]))
	assert.Equal(t, "Server default Some objects are still in an earlier place.", text(got["user2/repo15"]))
	assert.Equal(t, "garage-us Moving to this storage", text(got["user2/repo16"]))
	assert.Equal(t, 1, got["user2/repo15"].Find(`[data-testid="repo-storage-warning"]`).Length())
	assert.Zero(t, got["user2/repo16"].Find(`[data-testid="repo-storage-warning"]`).Length(), "a migration moving the objects is no mismatch")
	assert.Equal(t, "old This storage is not enabled.", text(rows("repo20")["user2/repo20"]))

	f.SetFail("/asc.v1.Storage/ListRepoPlacements", status.Error(codes.Unavailable, "down"))
	assert.Equal(t, "Not known", text(rows("repo1")["user2/repo1"]), "asc not answering is not shown as the default")
}

// Deleting a repository from the admin list needs its full name typed.
func TestAdminRepoDeleteNeedsName(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerName: "user2", LowerName: "repo2"})
	session := loginUser(t, "user1")

	doc := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/admin/repos?q=repo2"), http.StatusOK).Body)
	assert.Equal(t, 1, doc.Find(`#delete-repo [data-testid="repo-delete-name"]`).Length())

	for _, typed := range []string{"", "user2/repo20", "repo2"} {
		req := NewRequestWithValues(t, "POST", "/admin/repos/delete", map[string]string{"id": strconv.FormatInt(repo.ID, 10), "confirm_name": typed})
		resp := session.MakeRequest(t, req, http.StatusBadRequest)
		assert.Contains(t, resp.Body.String(), "does not match", typed)
		unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repo.ID})
	}

	req := NewRequestWithValues(t, "POST", "/admin/repos/delete", map[string]string{"id": strconv.FormatInt(repo.ID, 10), "confirm_name": "User2/Repo2"})
	session.MakeRequest(t, req, http.StatusOK)
	unittest.AssertNotExistsBean(t, &repo_model.Repository{ID: repo.ID})
}
