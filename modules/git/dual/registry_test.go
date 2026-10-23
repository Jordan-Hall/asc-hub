// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package dual_test

import (
	"path/filepath"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/git/dual"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dualRepo is a repository model that has storage_backend = dual.
type dualRepo struct {
	gitrepo.NamedRepository
}

func (dualRepo) GetStorageBackend() string { return "dual" }

// brokenAsc is an asc client whose every call panics: the worst shadow there is.
type brokenAsc struct{ ascv1.RepoClient }

// placeRepo copies the fixture repository to where gitrepo looks for owner/repo.
func placeRepo(t *testing.T, fx *fixture) dualRepo {
	t.Helper()
	root := filepath.Join(fx.root, "repos")
	gitRun(t, fx.root, nil, "clone", "-q", "--bare", fx.path, filepath.Join(root, "owner", "repo.git"))
	gitRun(t, filepath.Join(root, "owner", "repo.git"), nil, "symbolic-ref", "HEAD", "refs/heads/master")
	t.Cleanup(test.MockVariableValue(&setting.RepoRootPath, root))
	return dualRepo{gitrepo.NamedRepository{Owner: "owner", Name: "repo"}}
}

// A dual repository opened the way Forgejo opens it is served from git, and the shadow follows the
// state of the asc client.
func TestOpenedThroughGitrepo(t *testing.T) {
	fx := newFixture(t)
	r := placeRepo(t, fx)
	defer test.MockVariableValue(&setting.Asc.ShadowSamplePercent, 100)()
	dual.Configure(dual.Options{SamplePercent: 100, Sample: always})
	t.Cleanup(dual.Reset)

	t.Run("needs asc enabled", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		_, err := gitrepo.OpenRepository(t.Context(), r)
		require.ErrorIs(t, err, gitrepo.ErrAscDisabled)
	})

	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	t.Run("without an asc client the shadow is off and git still serves", func(t *testing.T) {
		ascbackend.Reset()
		repo, err := gitrepo.OpenRepository(t.Context(), r)
		require.NoError(t, err)
		t.Cleanup(func() { _ = repo.Close() })
		be, err := repo.Backend()
		require.NoError(t, err)
		d, ok := be.(*dual.Backend)
		require.True(t, ok, "%T", be)
		assert.False(t, d.ShadowEnabled())
		commit, err := repo.GetCommit("master")
		require.NoError(t, err)
		assert.Equal(t, fx.id("c3").String(), commit.ID.String())
	})

	t.Run("an asc client that panics changes nothing", func(t *testing.T) {
		ascbackend.Configure(brokenAsc{}, ascbackend.Options{})
		t.Cleanup(ascbackend.Reset)
		before := dual.Snapshot()
		repo, err := gitrepo.OpenRepository(t.Context(), r)
		require.NoError(t, err)
		be, err := repo.Backend()
		require.NoError(t, err)
		d, ok := be.(*dual.Backend)
		require.True(t, ok, "%T", be)
		require.True(t, d.ShadowEnabled())

		commit, err := repo.GetCommit("master")
		require.NoError(t, err)
		assert.Equal(t, fx.id("c3").String(), commit.ID.String())
		assert.Equal(t, "rename a", commit.Summary())
		flush(t, d)
		require.NoError(t, repo.Close())

		after := dual.Snapshot()
		for method, n := range after.Mismatched {
			assert.Zero(t, since(before.Mismatched, after.Mismatched, method), "%s (%d): a client that panics is not a mismatch", method, n)
		}
		var skipped uint64
		for _, reason := range []string{"shadow_error", "unsupported", "closed", "busy"} {
			skipped += since(before.Skipped, after.Skipped, reason)
		}
		assert.Positive(t, skipped)
	})
}

// The names of the metrics are fixed (SPEC section 16): forgejo_asc_shadow_mismatch_total{method}.
func TestMetricsAreRegistered(t *testing.T) {
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	seen := map[string]int{}
	for _, f := range families {
		if strings.HasPrefix(f.GetName(), "forgejo_asc_shadow_") {
			seen[f.GetName()] = len(f.GetMetric())
			for _, m := range f.GetMetric() {
				for _, l := range m.GetLabel() {
					assert.Contains(t, []string{"method", "reason"}, l.GetName())
				}
			}
		}
	}
	assert.Equal(t, 20, seen["forgejo_asc_shadow_mismatch_total"], "one series per method, from the start")
	assert.Equal(t, 20, seen["forgejo_asc_shadow_compared_total"])
	assert.Equal(t, 8, seen["forgejo_asc_shadow_skipped_total"])
}

func TestNewBackendNeedsARegisteredName(t *testing.T) {
	fx := newFixture(t)
	repo, err := git.OpenRepository(t.Context(), fx.path)
	require.NoError(t, err)
	defer repo.Close()
	_, err = git.NewBackend("nope", repo)
	require.ErrorIs(t, err, git.ErrNoBackend)
	be, err := git.NewBackend(git.DefaultBackendName, repo)
	require.NoError(t, err)
	require.NoError(t, be.Close())
}
