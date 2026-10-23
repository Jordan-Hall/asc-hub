// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	actions_model "forgejo.org/models/actions"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

const (
	ascTestHead      = "1111111111111111111111111111111111111111"
	ascTestBase      = "2222222222222222222222222222222222222222"
	ascTestMergeBase = "3333333333333333333333333333333333333333"
)

// ascTestChangeset is the changeset id the stub server gives to the commit whose git id is made of c.
func ascTestChangeset(c byte) []byte { return bytes.Repeat([]byte{c ^ 0xf0}, 32) }

// stubAsc answers GitMap only: it knows the three commits above and nothing else.
type stubAsc struct {
	ascv1.RepoClient
	calls int
}

func (s *stubAsc) GitMap(_ context.Context, req *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	s.calls++
	resp := &ascv1.GitMapResponse{}
	for _, id := range req.GetGit() {
		switch hex.EncodeToString(id) {
		case ascTestHead, ascTestBase, ascTestMergeBase:
			resp.GitToAsc = append(resp.GitToAsc, ascTestChangeset(id[0]))
		default:
			resp.GitToAsc = append(resp.GitToAsc, nil)
		}
	}
	return resp, nil
}

func TestGenerateTaskContextWithAsc(t *testing.T) {
	workflow := "name: w\non: pull_request\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"
	prPayload := func(mergeBase string) string {
		return fmt.Sprintf(`{"pull_request":{"merge_base":%q,"base":{"ref":"main","name":"main","sha":%q},"head":{"ref":"topic","sha":%q}}}`,
			mergeBase, ascTestBase, ascTestHead)
	}

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "testowner", "testrepo.git"), 0o755))
	t.Cleanup(test.MockVariableValue(&setting.RepoRootPath, root))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ServerURL, "https://asc.example.com"))

	stub := &stubAsc{}
	ascbackend.Configure(stub, ascbackend.Options{Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil }})
	t.Cleanup(ascbackend.Reset)

	newTask := func(storageBackend, event, payload, sha string) *actions_model.ActionTask {
		return &actions_model.ActionTask{
			ID: 47,
			Job: &actions_model.ActionRunJob{
				ID:    2,
				RunID: 1,
				Run: &actions_model.ActionRun{
					ID:                1,
					Index:             42,
					TriggerUser:       &user.User{ID: 1, Name: "testuser"},
					Repo:              &repo_model.Repository{ID: 1, OwnerName: "testowner", Name: "testrepo", StorageBackend: storageBackend},
					Event:             webhook_module.HookEventType(event),
					TriggerEvent:      "push",
					Ref:               "refs/heads/main",
					CommitSHA:         sha,
					WorkflowID:        "test-workflow.yaml",
					WorkflowDirectory: ".forgejo/workflows",
					EventPayload:      payload,
				},
				WorkflowPayload: []byte(workflow),
			},
		}
	}
	generate := func(task *actions_model.ActionTask) (map[string]any, error) {
		sc, err := generateTaskContextWithAsc(t.Context(), task, &repo_model.ActionsConfig{})
		if err != nil {
			return nil, err
		}
		return sc.AsMap(), nil
	}
	hexOf := func(c byte) string { return hex.EncodeToString(ascTestChangeset(c)) }

	t.Run("push on an asc repository adds the server and the changeset", func(t *testing.T) {
		stub.calls = 0
		got, err := generate(newTask("asc", "push", `{}`, ascTestHead))
		require.NoError(t, err)
		assert.Equal(t, "https://asc.example.com", got["asc_server_url"])
		assert.Equal(t, hexOf(0x11), got["asc_changeset"])
		assert.NotContains(t, got, "asc_base_merge_base")
		assert.Equal(t, ascTestHead, got["sha"], "github.sha stays the git SHA")
		assert.Equal(t, 1, stub.calls)
	})

	t.Run("pull request adds the merge base", func(t *testing.T) {
		task := newTask("asc", "pull_request", prPayload(ascTestMergeBase), ascTestHead)
		task.Job.Run.TriggerEvent = "pull_request"
		got, err := generate(task)
		require.NoError(t, err)
		assert.Equal(t, hexOf(0x11), got["asc_changeset"])
		assert.Equal(t, hexOf(0x33), got["asc_base_merge_base"])
		assert.Equal(t, ascTestHead, got["sha"])
	})

	t.Run("pull_request_target names the base commit that github.sha shows", func(t *testing.T) {
		task := newTask("asc", "pull_request", prPayload(ascTestMergeBase), ascTestHead)
		task.Job.Run.TriggerEvent = "pull_request_target"
		got, err := generate(task)
		require.NoError(t, err)
		assert.Equal(t, ascTestBase, got["sha"])
		assert.Equal(t, hexOf(0x22), got["asc_changeset"])
		assert.Equal(t, hexOf(0x33), got["asc_base_merge_base"])
	})

	t.Run("a pull request without a merge base gets no merge base key", func(t *testing.T) {
		task := newTask("asc", "pull_request", prPayload(""), ascTestHead)
		task.Job.Run.TriggerEvent = "pull_request"
		got, err := generate(task)
		require.NoError(t, err)
		assert.Contains(t, got, "asc_changeset")
		assert.NotContains(t, got, "asc_base_merge_base")
	})

	t.Run("a git or dual repository gets no asc keys and no call", func(t *testing.T) {
		for _, sb := range []string{"git", "dual", ""} {
			stub.calls = 0
			got, err := generate(newTask(sb, "push", `{}`, ascTestHead))
			require.NoError(t, err)
			assert.NotContains(t, got, "asc_server_url")
			assert.NotContains(t, got, "asc_changeset")
			assert.NotContains(t, got, "asc_base_merge_base")
			assert.Zero(t, stub.calls)
		}
	})

	t.Run("fails closed", func(t *testing.T) {
		failures := map[string]*actions_model.ActionTask{
			"commit unknown to asc":      newTask("asc", "push", `{}`, "4444444444444444444444444444444444444444"),
			"abbreviated sha":            newTask("asc", "push", `{}`, "1111111"),
			"empty sha":                  newTask("asc", "push", `{}`, ""),
			"merge base unknown to asc":  newTask("asc", "pull_request", prPayload("5555555555555555555555555555555555555555"), ascTestHead),
			"malformed merge base":       newTask("asc", "pull_request", prPayload("zz"), ascTestHead),
			"unknown storage backend":    newTask("svn", "push", `{}`, ascTestHead),
			"storage backend with a tab": newTask("asc\t", "push", `{}`, ascTestHead),
		}
		for name, task := range failures {
			t.Run(name, func(t *testing.T) {
				got, err := generate(task)
				require.Error(t, err)
				assert.Nil(t, got)
			})
		}

		t.Run("asc not enabled", func(t *testing.T) {
			defer test.MockVariableValue(&setting.Asc.Enabled, false)()
			_, err := generate(newTask("asc", "push", `{}`, ascTestHead))
			require.Error(t, err)
		})

		t.Run("no client configured", func(t *testing.T) {
			ascbackend.Reset()
			defer ascbackend.Configure(stub, ascbackend.Options{Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil }})
			_, err := generate(newTask("asc", "push", `{}`, ascTestHead))
			require.ErrorIs(t, err, ascbackend.ErrNotConfigured)
		})
	})
}
