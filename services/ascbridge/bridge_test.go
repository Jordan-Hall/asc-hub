// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forgejo.org/modules/repostore"
	ascwriter "forgejo.org/modules/repostore/asc"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingExporter records its calls.
type countingExporter struct {
	calls atomic.Int32
	err   error
	delay time.Duration
	names []string
	mu    sync.Mutex
}

func (e *countingExporter) Export(_ context.Context, name, dir string) error {
	e.calls.Add(1)
	e.mu.Lock()
	e.names = append(e.names, name+" "+filepath.Base(dir))
	e.mu.Unlock()
	time.Sleep(e.delay)
	return e.err
}

func TestPathIsTheRepositoryPath(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeEager)
	assert.Equal(t, fx.repo.RepoPath(), Path(fx.repo))
	assert.Equal(t, fx.repo.WikiPath(), WikiPath(fx.repo))
	assert.True(t, strings.HasSuffix(filepath.ToSlash(Path(fx.repo)), "/owner/repo.git"))
}

func TestEnsureForARepositoryThatIsNotInAsc(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeLazy)
	fx.repo.StorageBackend = "git"
	exp := &countingExporter{}
	t.Cleanup(UseExporter(exp))
	dir, err := Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, fx.repo.RepoPath(), dir)
	assert.Zero(t, exp.calls.Load())
	assert.NoDirExists(t, dir, "a git repository is not touched")

	fx.repo.StorageBackend = "mystery"
	_, err = Ensure(t.Context(), fx.repo, false)
	require.Error(t, err, "an unknown storage backend is never treated as plain git")
}

func TestEnsureRefusals(t *testing.T) {
	t.Run("asc is off", func(t *testing.T) {
		fx := newFixture(t, setting.AscBridgeLazy)
		setting.Asc.Enabled = false
		_, err := Ensure(t.Context(), fx.repo, false)
		require.ErrorIs(t, err, ErrDisabled)
	})
	t.Run("bridge off", func(t *testing.T) {
		fx := newFixture(t, setting.AscBridgeOff)
		_, err := Ensure(t.Context(), fx.repo, false)
		require.ErrorIs(t, err, ErrOff)
		assert.NoDirExists(t, fx.repo.RepoPath())
	})
}

func TestEnsureEagerOnlyMakesTheDirectory(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeEager)
	exp := &countingExporter{}
	t.Cleanup(UseExporter(exp))

	dir, err := Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	head, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	require.NoError(t, err)
	assert.Equal(t, "ref: refs/heads/main", strings.TrimSpace(string(head)))
	assert.Zero(t, exp.calls.Load(), "asc keeps an eager bridge current")
	assert.Zero(t, fx.fake.count("ListRefs"))

	// Asking again leaves the repository as it is.
	sentinel := filepath.Join(dir, "sentinel")
	require.NoError(t, os.WriteFile(sentinel, nil, 0o600))
	_, err = Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.FileExists(t, sentinel)
}

func TestEnsureLazyBuildsOnceAndAgainWhenTheRefsMove(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeLazy)
	exp := &countingExporter{}
	t.Cleanup(UseExporter(exp))
	fx.fake.set("refs/heads/main", strings.Repeat("11", 20))
	fx.fake.set("refs/users/7/heads/draft", strings.Repeat("99", 20))

	dir, err := Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, int32(1), exp.calls.Load())
	assert.Equal(t, []string{"owner/repo repo.git"}, exp.names)
	assert.FileExists(t, filepath.Join(dir, MarkerName))

	_, err = Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, int32(1), exp.calls.Load(), "the refs have not moved")

	fx.fake.set("refs/users/7/heads/draft", strings.Repeat("98", 20))
	_, err = Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, int32(1), exp.calls.Load(), "a user ref is not part of the bridge")

	fx.fake.set("refs/heads/main", strings.Repeat("22", 20))
	_, err = Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, int32(2), exp.calls.Load())

	fx.fake.set("refs/pull/3/head", strings.Repeat("33", 20))
	_, err = Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, int32(3), exp.calls.Load(), "pull request heads are part of the bridge")
}

func TestEnsureLazyFailedBuildIsRetried(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeLazy)
	exp := &countingExporter{err: errors.New("asc export failed")}
	t.Cleanup(UseExporter(exp))
	fx.fake.set("refs/heads/main", strings.Repeat("11", 20))

	_, err := Ensure(t.Context(), fx.repo, false)
	require.ErrorContains(t, err, "asc export failed")
	assert.NoFileExists(t, filepath.Join(fx.repo.RepoPath(), MarkerName), "a failed build records nothing")

	exp.err = nil
	_, err = Ensure(t.Context(), fx.repo, false)
	require.NoError(t, err)
	assert.Equal(t, int32(2), exp.calls.Load())
}

func TestEnsureLazyConcurrentFirstRequestsShareOneBuild(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeLazy)
	exp := &countingExporter{delay: 50 * time.Millisecond}
	t.Cleanup(UseExporter(exp))
	fx.fake.set("refs/heads/main", strings.Repeat("11", 20))

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Ensure(t.Context(), fx.repo, false)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), exp.calls.Load())
}

func TestEnsureWikiBuildsTheWikiRepository(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeLazy)
	exp := &countingExporter{}
	t.Cleanup(UseExporter(exp))
	dir, err := Ensure(t.Context(), fx.repo, true)
	require.NoError(t, err)
	assert.Equal(t, fx.repo.WikiPath(), dir)
	assert.Equal(t, []string{"owner/repo.wiki repo.wiki.git"}, exp.names)
}

func TestAscName(t *testing.T) {
	root := t.TempDir()
	oldRoot := setting.RepoRootPath
	setting.RepoRootPath = root
	t.Cleanup(func() { setting.RepoRootPath = oldRoot })
	for path, want := range map[string]string{
		filepath.Join(root, "o", "r.git"):      "o/r",
		filepath.Join(root, "o", "r.wiki.git"): "o/r.wiki",
	} {
		got, err := ascName(path)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{
		root, filepath.Join(root, "o"), filepath.Join(root, "o", "r"), filepath.Join(root, "o", ".git"),
		filepath.Join(root, "o", "r.git", "x"), filepath.Join(root, "..", "r.git"), filepath.Join(filepath.Dir(root), "o", "r.git"),
	} {
		_, err := ascName(bad)
		require.Error(t, err, bad)
	}
}

func TestLifecycle(t *testing.T) {
	fx := newFixture(t, setting.AscBridgeEager)
	ref := fx.repo.StoreRef()
	ref2 := repostore.RepoRef{Owner: "Owner", Name: "Other", ObjectFormat: "sha1", Path: filepath.Join(setting.RepoRootPath, "owner", "other.git")}
	var lc Lifecycle

	require.NoError(t, lc.Prepare(t.Context(), ref, "trunk"))
	head, err := os.ReadFile(filepath.Join(ref.Path, "HEAD"))
	require.NoError(t, err)
	assert.Equal(t, "ref: refs/heads/trunk", strings.TrimSpace(string(head)))
	require.NoError(t, lc.Prepare(t.Context(), ref, "other"), "preparing twice leaves the bridge as it is")
	head, _ = os.ReadFile(filepath.Join(ref.Path, "HEAD"))
	assert.Equal(t, "ref: refs/heads/trunk", strings.TrimSpace(string(head)))

	require.NoError(t, lc.Rename(t.Context(), ref, ref2))
	assert.NoDirExists(t, ref.Path)
	assert.DirExists(t, ref2.Path)
	require.NoError(t, lc.Rename(t.Context(), ref, ref2), "a bridge that was never made is not an error")

	require.NoError(t, lc.Remove(t.Context(), ref2))
	assert.NoDirExists(t, ref2.Path)
	require.NoError(t, lc.Remove(t.Context(), ref2))

	for _, bad := range []string{"", "relative/repo.git", setting.RepoRootPath, filepath.Join(setting.RepoRootPath, "x.git"), filepath.Join(filepath.Dir(setting.RepoRootPath), "o", "r.git")} {
		assert.Error(t, lc.Remove(t.Context(), repostore.RepoRef{Path: bad}), bad)
		assert.Error(t, lc.Prepare(t.Context(), repostore.RepoRef{Path: bad}, ""), bad)
	}
}

func TestInitConnectsTheLifecycleOnlyWhenAscIsOn(t *testing.T) {
	oldAsc := setting.Asc
	t.Cleanup(func() { setting.Asc = oldAsc })
	setting.Asc.Enabled = false
	require.NoError(t, Init(t.Context()))
	setting.Asc.Enabled = true
	t.Cleanup(func() { ascwriter.SetBridge(nil) })
	require.NoError(t, Init(t.Context()))
}
