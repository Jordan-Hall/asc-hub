// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/repostore"
	ascwriter "forgejo.org/modules/repostore/asc"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mirrorFixture is an asc repository whose bridge has two branches and a tag, as a git fetch leaves it.
func mirrorFixture(t *testing.T) (fx *fixture, c1, c2 string) {
	t.Helper()
	fx = newFixture(t, setting.AscBridgeEager)
	dir := fx.repo.RepoPath()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	gitRun(t, dir, nil, "init", "-q", "--bare", "-b", "main")
	c1 = fx.commit(dir, "one")
	c2 = fx.commit(dir, "two", c1)
	gitRun(t, dir, nil, "update-ref", "refs/heads/main", c2)
	gitRun(t, dir, nil, "update-ref", "refs/heads/feature", c1)
	gitRun(t, dir, nil, "update-ref", "refs/tags/v1", c1)

	t.Cleanup(ascsync.UseClient(fx.fake))
	ascwriter.Configure(fx.fake, ascwriter.Options{})
	repostore.UseAscWriter(true)
	t.Cleanup(func() { repostore.UseAscWriter(false); ascwriter.Reset() })
	return fx, c1, c2
}

func hexOrZero(b []byte) string {
	if len(b) == 0 {
		return zeroID
	}
	return hex.EncodeToString(b)
}

func TestSyncFromBridgeImportsWhatTheFetchBrought(t *testing.T) {
	fx, c1, c2 := mirrorFixture(t)
	fx.fake.set("refs/heads/main", c1) // asc is one commit behind on main; feature and the tag are new

	require.NoError(t, SyncFromBridge(t.Context(), fx.repo, false))

	require.Len(t, fx.fake.imports, 1)
	in := fx.fake.imports[0]
	assert.Equal(t, "owner/repo", in.GetRepo().GetName())
	assert.Equal(t, fx.repo.RepoPath(), in.GetGitDir())
	assert.Equal(t, filepath.Join(fx.repo.RepoPath(), "objects"), in.GetObjectDir())
	got := map[string][2]string{}
	for _, u := range in.GetUpdates() {
		got[u.GetName()] = [2]string{hexOrZero(u.GetOldGit()), hexOrZero(u.GetNewGit())}
	}
	assert.Equal(t, map[string][2]string{
		"refs/heads/main":    {c1, c2},
		"refs/heads/feature": {zeroID, c1},
		"refs/tags/v1":       {zeroID, c1},
	}, got)
	assert.Equal(t, c2, fx.fake.refs["refs/heads/main"])

	// Nothing moved: nothing to import.
	fx.fake.imports = nil
	require.NoError(t, SyncFromBridge(t.Context(), fx.repo, true))
	assert.Empty(t, fx.fake.imports)
}

func TestSyncFromBridgeForcesARewrittenBranch(t *testing.T) {
	fx, c1, c2 := mirrorFixture(t)
	other := fx.commit(fx.repo.RepoPath(), "rewritten", c1)
	gitRun(t, fx.repo.RepoPath(), nil, "update-ref", "refs/heads/feature", other)
	fx.fake.set("refs/heads/main", c2)
	fx.fake.set("refs/heads/feature", c2) // asc has a commit the new feature does not descend from
	fx.fake.set("refs/tags/v1", c1)
	fx.fake.importErr["refs/heads/feature"] = status.Error(codes.FailedPrecondition, "not a fast-forward")

	require.NoError(t, SyncFromBridge(t.Context(), fx.repo, false))

	require.Len(t, fx.fake.updates, 1, "the one refused ref is forced")
	u := fx.fake.updates[0]
	assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FORCE, u.GetMode())
	assert.Equal(t, "refs/heads/feature", u.GetUpdates()[0].GetName())
	assert.Equal(t, other, fx.fake.refs["refs/heads/feature"])
}

func TestSyncFromBridgeFailsWhenAscIsUnreachable(t *testing.T) {
	fx, c1, _ := mirrorFixture(t)
	fx.fake.importErr["refs/heads/feature"] = status.Error(codes.Unavailable, "asc is down")
	fx.fake.set("refs/heads/main", c1)
	err := SyncFromBridge(t.Context(), fx.repo, false)
	require.Error(t, err)
	assert.Empty(t, fx.fake.updates, "an unreachable asc is not answered by forcing")
}

func TestSyncFromBridgePrunes(t *testing.T) {
	fx, c1, c2 := mirrorFixture(t)
	fx.fake.set("refs/heads/main", c2)
	fx.fake.set("refs/heads/feature", c1)
	fx.fake.set("refs/tags/v1", c1)
	fx.fake.set("refs/heads/deleted-upstream", c1)
	fx.fake.set("refs/pull/1/head", c1)

	require.NoError(t, SyncFromBridge(t.Context(), fx.repo, false))
	assert.Contains(t, fx.fake.refs, "refs/heads/deleted-upstream", "without prune the branch stays")

	require.NoError(t, SyncFromBridge(t.Context(), fx.repo, true))
	assert.NotContains(t, fx.fake.refs, "refs/heads/deleted-upstream")
	assert.Contains(t, fx.fake.refs, "refs/pull/1/head", "only branches and tags are pruned")
}

func TestSyncFromBridgeIgnoresAGitRepository(t *testing.T) {
	fx, _, _ := mirrorFixture(t)
	fx.repo.StorageBackend = "git"
	require.NoError(t, SyncFromBridge(t.Context(), fx.repo, true))
	assert.Empty(t, fx.fake.imports)
}
