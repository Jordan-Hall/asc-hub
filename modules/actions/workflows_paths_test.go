// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"forgejo.org/modules/git"
	_ "forgejo.org/modules/git/gitcli" // the backend the recording backend wraps
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/modules/test"

	"code.forgejo.org/forgejo/runner/v12/act/jobparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changedPathsCall is one ChangedPaths call that reached the backend.
type changedPathsCall struct {
	base, head string
	opt        git.DiffOpts
}

// recordingBackend wraps a backend and records ChangedPaths, the one call (Diff with NAMES on an asc
// repository) that the paths and paths-ignore filters make. A set err fails it.
type recordingBackend struct {
	git.Backend
	mu    sync.Mutex
	calls []changedPathsCall
	err   error
}

func (r *recordingBackend) ChangedPaths(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts) ([]git.PathChange, error) {
	r.mu.Lock()
	r.calls = append(r.calls, changedPathsCall{base: base.String(), head: head.String(), opt: opt})
	r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	return r.Backend.ChangedPaths(ctx, base, head, opt)
}

var (
	registerRecording sync.Once
	activeRecording   *recordingBackend
)

func openRecordingRepo(t *testing.T, path string) (*git.Repository, *recordingBackend) {
	t.Helper()
	registerRecording.Do(func() {
		git.RegisterBackend("k14-recording", func(repo *git.Repository) (git.Backend, error) {
			inner, err := git.NewBackend(git.DefaultBackendName, repo)
			if err != nil {
				return nil, err
			}
			activeRecording = &recordingBackend{Backend: inner}
			return activeRecording, nil
		})
	})
	repo, err := git.OpenRepositoryWithBackend(t.Context(), path, "k14-recording")
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })
	return repo, activeRecording
}

func TestPathsFiltersUseChangedPaths(t *testing.T) {
	t.Cleanup(test.MockVariableValue(&setting.Git.HomePath, t.TempDir()))
	require.NoError(t, git.InitSimple(t.Context()))

	home := t.TempDir()
	committer := git.Signature{Email: "jane@example.com", Name: "Jane", When: time.Now()}
	write := func(name string) {
		require.NoError(t, os.MkdirAll(filepath.Join(home, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte(name), 0o644))
	}
	require.NoError(t, git.InitRepository(t.Context(), home, false, git.Sha1ObjectFormat.Name()))
	write("README.md")
	require.NoError(t, git.AddChanges(home, true))
	require.NoError(t, git.CommitChanges(home, git.CommitChangesOptions{Message: "one", Committer: &committer}))
	write("docs/a.md")
	write("src/b.go")
	require.NoError(t, git.AddChanges(home, true))
	require.NoError(t, git.CommitChanges(home, git.CommitChangesOptions{Message: "two", Committer: &committer}))

	repo, rec := openRecordingRepo(t, home)
	headID, err := repo.GetRefCommitID("HEAD")
	require.NoError(t, err)
	head, err := repo.GetCommit(headID)
	require.NoError(t, err)
	baseObj, err := head.ParentID(0)
	require.NoError(t, err)
	baseID := baseObj.String()

	push := &api.PushPayload{Ref: "refs/heads/main", Before: baseID}
	pull := &api.PullRequestPayload{Action: api.HookIssueOpened, PullRequest: &api.PullRequest{MergeBase: baseID}}
	event := func(t *testing.T, yaml string) *jobparser.Event {
		evts, err := GetEventsFromContent([]byte(yaml))
		require.NoError(t, err)
		require.Len(t, evts, 1)
		return evts[0]
	}

	cases := []struct {
		name     string
		yaml     string
		expected bool
	}{
		{"paths matches a changed path", "paths:\n      - src/**", true},
		{"paths matches nothing changed", "paths:\n      - lib/**", false},
		{"paths-ignore leaves a changed path", "paths-ignore:\n      - docs/**", true},
		{"paths-ignore covers every changed path", "paths-ignore:\n      - docs/**\n      - src/**", false},
	}
	for _, tc := range cases {
		t.Run("push "+tc.name, func(t *testing.T) {
			rec.calls, rec.err = nil, nil
			assert.Equal(t, tc.expected, matchPushEvent(head, push, event(t, "on:\n  push:\n    "+tc.yaml)))
			require.Len(t, rec.calls, 1)
			assert.Equal(t, changedPathsCall{base: baseID, head: headID}, rec.calls[0], "push: Before to the pushed commit, no merge base")
		})
		t.Run("pull request "+tc.name, func(t *testing.T) {
			rec.calls, rec.err = nil, nil
			assert.Equal(t, tc.expected, matchPullRequestEvent(repo, head, pull, event(t, "on:\n  pull_request:\n    "+tc.yaml)))
			require.Len(t, rec.calls, 1)
			assert.Equal(t, changedPathsCall{base: baseID, head: headID}, rec.calls[0], "pull request: merge base to head")
		})
		t.Run("a failed lookup runs nothing, "+tc.name, func(t *testing.T) {
			rec.calls, rec.err = nil, errors.New("asc serve is down")
			assert.False(t, matchPushEvent(head, push, event(t, "on:\n  push:\n    "+tc.yaml)))
			assert.False(t, matchPullRequestEvent(repo, head, pull, event(t, "on:\n  pull_request:\n    "+tc.yaml)))
			rec.err = nil
		})
	}
}
