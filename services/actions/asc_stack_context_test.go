// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/models/user"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscStackContext(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "testowner", "testrepo.git"), 0o755))
	t.Cleanup(test.MockVariableValue(&setting.RepoRootPath, root))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ServerURL, "https://asc.example.com"))
	ascbackend.Configure(&stubAsc{}, ascbackend.Options{Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil }})
	t.Cleanup(ascbackend.Reset)

	const (
		changeBottom = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
		changeTop    = "llllllllllllllllllllllllllllllll"
	)
	// Repository 1: pull request 1 (index 2) has merged; 2 (index 3) and 5 (index 5) are open.
	merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	bottom := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	top := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	require.True(t, merged.HasMerged)
	for _, pr := range []*issues_model.PullRequest{bottom, top} {
		_, err := db.GetEngine(ctx).ID(pr.ID).Cols("merge_base").Update(&issues_model.PullRequest{MergeBase: ascTestMergeBase})
		require.NoError(t, err)
	}
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, bottom, changeBottom, merged.ID))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, top, changeTop, bottom.ID))

	generate := func(storageBackend string, index int64) (map[string]any, error) {
		payload := fmt.Sprintf(`{"pull_request":{"number":%d,"merge_base":%q,"base":{"ref":"main","name":"main","sha":%q},"head":{"ref":"topic","sha":%q}}}`,
			index, ascTestMergeBase, ascTestBase, ascTestHead)
		task := &actions_model.ActionTask{
			ID: 47,
			Job: &actions_model.ActionRunJob{
				ID: 2, RunID: 1,
				Run: &actions_model.ActionRun{
					ID: 1, Index: 42,
					TriggerUser:       &user.User{ID: 1, Name: "testuser"},
					Repo:              &repo_model.Repository{ID: 1, OwnerName: "testowner", Name: "testrepo", StorageBackend: storageBackend},
					Event:             webhook_module.HookEventPullRequest,
					TriggerEvent:      "pull_request",
					Ref:               "refs/pull/1/head",
					CommitSHA:         ascTestHead,
					WorkflowID:        "w.yaml",
					WorkflowDirectory: ".forgejo/workflows",
					EventPayload:      payload,
				},
				WorkflowPayload: []byte("name: w\non: pull_request\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"),
			},
		}
		sc, err := generateTaskContextWithAsc(ctx, task, &repo_model.ActionsConfig{})
		if err != nil {
			return nil, err
		}
		return sc.AsMap(), nil
	}

	t.Run("the top of two", func(t *testing.T) {
		got, err := generate("asc", top.Index)
		require.NoError(t, err)
		assert.Equal(t, changeTop, got["asc_change"])
		assert.Equal(t, "2", got["asc_stack_position"])
		assert.Equal(t, "2", got["asc_stack_size"])
		assert.Equal(t, ascTestMergeBase, got["asc_stack_parent_sha"])
	})

	t.Run("the bottom counts from the first unmerged change", func(t *testing.T) {
		got, err := generate("asc", bottom.Index)
		require.NoError(t, err)
		assert.Equal(t, changeBottom, got["asc_change"])
		assert.Equal(t, "1", got["asc_stack_position"])
		assert.Equal(t, "2", got["asc_stack_size"])
	})

	t.Run("a pull request that is not stacked gets no stack keys", func(t *testing.T) {
		other := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
		got, err := generate("asc", other.Index)
		require.NoError(t, err)
		assert.NotContains(t, got, "asc_change")
		assert.NotContains(t, got, "asc_stack_size")
	})

	t.Run("the base comes from the database, not the payload", func(t *testing.T) {
		_, err := db.GetEngine(ctx).ID(top.ID).Cols("merge_base").Update(&issues_model.PullRequest{MergeBase: ascTestBase})
		require.NoError(t, err)
		got, err := generate("asc", top.Index)
		require.NoError(t, err)
		assert.Equal(t, ascTestBase, got["asc_stack_parent_sha"])
		assert.Equal(t, ascTestMergeBase, got["asc_base_merge_base"])
	})

	t.Run("a malformed stored merge base fails closed", func(t *testing.T) {
		_, err := db.GetEngine(ctx).ID(top.ID).Cols("merge_base").Update(&issues_model.PullRequest{MergeBase: "zz"})
		require.NoError(t, err)
		_, err = generate("asc", top.Index)
		require.Error(t, err)
	})

	t.Run("a stacked pull request with no stored merge base fails closed", func(t *testing.T) {
		_, err := db.GetEngine(ctx).ID(top.ID).Cols("merge_base").Update(&issues_model.PullRequest{MergeBase: ""})
		require.NoError(t, err)
		got, err := generate("asc", top.Index)
		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("a cycle fails closed", func(t *testing.T) {
		_, err := db.GetEngine(ctx).ID(top.ID).Cols("merge_base").Update(&issues_model.PullRequest{MergeBase: ascTestMergeBase})
		require.NoError(t, err)
		_, err = db.GetEngine(ctx).Table("pull_request_asc_stack").Where("pull_id = ?", bottom.ID).Update(map[string]any{"stack_parent_id": top.ID})
		require.NoError(t, err)
		_, err = generate("asc", top.Index)
		require.Error(t, err)
	})

	t.Run("a git repository gets no stack keys", func(t *testing.T) {
		got, err := generate("git", top.Index)
		require.NoError(t, err)
		assert.NotContains(t, got, "asc_change")
	})
}
