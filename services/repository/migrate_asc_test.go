// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// fakeAscImport is an asc server that holds no refs and records the imports it is asked for.
type fakeAscImport struct {
	ascv1.UnimplementedRepoServer
	mu      sync.Mutex
	imports []*ascv1.ImportGitPushRequest
	trunks  []string
	refuse  error
}

// fakeSecretScan answers every scan with one finding, and records the notes it is given.
type fakeSecretScan struct {
	ascv1.UnimplementedSecretScanServer
	mu      sync.Mutex
	revs    []string
	noted   map[uint64]string
	findOne bool
}

func (f *fakeSecretScan) Scan(_ context.Context, req *ascv1.ScanSecretsRequest) (*ascv1.ScanSecretsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revs = append(f.revs, req.GetRev())
	if !f.findOne {
		return &ascv1.ScanSecretsResponse{}, nil
	}
	return &ascv1.ScanSecretsResponse{Findings: []*ascv1.SecretFinding{{Id: 7, State: ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN}}}, nil
}

func (f *fakeSecretScan) UpdateFinding(_ context.Context, req *ascv1.UpdateSecretFindingRequest) (*ascv1.UpdateSecretFindingResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.noted[req.GetId()] = req.GetNote()
	return &ascv1.UpdateSecretFindingResponse{}, nil
}

// RepoAdmin records the default branches asc is told.
func (f *fakeAscImport) RepoAdmin(_ context.Context, req *ascv1.RepoAdminRequest) (*ascv1.RepoAdminResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b := req.GetSetDefaultBranch(); b != "" {
		f.trunks = append(f.trunks, b)
	}
	return &ascv1.RepoAdminResponse{}, nil
}

func (f *fakeAscImport) ListRefs(context.Context, *ascv1.ListRefsRequest) (*ascv1.ListRefsResponse, error) {
	return &ascv1.ListRefsResponse{}, nil
}

func (f *fakeAscImport) ImportGitPush(_ context.Context, req *ascv1.ImportGitPushRequest) (*ascv1.ImportGitPushResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imports = append(f.imports, req)
	if f.refuse != nil {
		return nil, f.refuse
	}
	resp := &ascv1.ImportGitPushResponse{}
	for _, u := range req.GetUpdates() {
		resp.Results = append(resp.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	return resp, nil
}

func gitIn(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...)
	opts := &git.RunOpts{Dir: dir, Env: append(os.Environ(),
		"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com")}
	if stdin != "" {
		opts.Stdin = strings.NewReader(stdin)
	}
	out, stderr, err := cmd.RunStdString(opts)
	require.NoError(t, err, stderr)
	return strings.TrimSpace(out)
}

// ascMigrationFixture is a migrated asc repository as the clone leaves it: a bare directory with a branch, a
// tag and the pull request refs of a forge, below a repository root of its own.
type ascMigrationFixture struct {
	repo   *repo_model.Repository
	fake   *fakeAscImport
	secret *fakeSecretScan
	commit string
}

func newAscMigrationFixture(t *testing.T) *ascMigrationFixture {
	t.Helper()
	oldRoot, oldEnabled, oldToken := setting.RepoRootPath, setting.Asc.Enabled, setting.InternalToken
	setting.RepoRootPath, setting.Asc.Enabled, setting.InternalToken = t.TempDir(), true, "internal-token-for-tests"
	oldWait, oldPoll := ascImportVisibleWait, ascImportVisiblePoll
	ascImportVisibleWait, ascImportVisiblePoll = 300*time.Millisecond, 10*time.Millisecond
	oldEnsure := ensureAscRepo
	ensureAscRepo = func(context.Context, *repo_model.Repository, string) error { return nil }
	t.Cleanup(func() {
		ensureAscRepo = oldEnsure
		setting.RepoRootPath, setting.Asc.Enabled, setting.InternalToken = oldRoot, oldEnabled, oldToken
		ascImportVisibleWait, ascImportVisiblePoll = oldWait, oldPoll
	})

	repo := &repo_model.Repository{OwnerName: "Owner", Name: "Repo", StorageBackend: "asc", ObjectFormatName: "sha1"}
	bridge := repo.RepoPath()
	require.NoError(t, os.MkdirAll(bridge, 0o755))
	gitIn(t, bridge, "", "init", "-q", "--bare", "-b", "main")
	tree := gitIn(t, bridge, "", "hash-object", "-t", "tree", "-w", "--stdin")
	commit := gitIn(t, bridge, "", "commit-tree", tree, "-m", "one")
	for _, ref := range []string{"refs/heads/main", "refs/tags/v1", "refs/pull/1/head", "refs/merge-requests/2/head"} {
		gitIn(t, bridge, "", "update-ref", ref, commit)
	}

	fake := &fakeAscImport{}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, fake)
	secret := &fakeSecretScan{noted: map[uint64]string{}, findOne: true}
	ascv1.RegisterSecretScanServer(srv, secret) // before Serve: a service registered after it starts is a fatal error
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	conn, err := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	api := ascv1.NewRepoClient(conn)
	restoreSync, restoreSecret := ascsync.UseClient(api), ascsync.UseSecretClient(ascv1.NewSecretScanClient(conn))
	t.Cleanup(func() {
		restoreSync()
		restoreSecret()
		_ = conn.Close()
		srv.Stop()
	})
	return &ascMigrationFixture{repo: repo, fake: fake, secret: secret, commit: commit}
}

func TestScrubBridgeConfig(t *testing.T) {
	fx := newAscMigrationFixture(t)
	path := fx.repo.RepoPath()
	gitIn(t, path, "", "config", "--local", "http.followRedirects", "false")
	gitIn(t, path, "", "config", "--local", "http.sslVerify", "false")
	gitIn(t, path, "", "config", "--local", "remote.origin.url", "https://example.org/x.git")
	require.NoError(t, scrubBridgeConfig(t.Context(), path))
	conf := gitIn(t, path, "", "config", "--local", "--list")
	assert.NotContains(t, conf, "http.")
	assert.Contains(t, conf, "remote.origin.url=", "what git clone --mirror writes stays")
	// Nothing to remove is no error.
	require.NoError(t, scrubBridgeConfig(t.Context(), path))
}

func TestPruneNonBranchRefs(t *testing.T) {
	fx := newAscMigrationFixture(t)
	path := fx.repo.RepoPath()
	require.NoError(t, pruneNonBranchRefs(t.Context(), path))
	refs := gitIn(t, path, "", "for-each-ref", "--format=%(refname)")
	assert.Equal(t, "refs/heads/main\nrefs/tags/v1", refs)
	// A second run has nothing left to do.
	require.NoError(t, pruneNonBranchRefs(t.Context(), path))
}

func TestImportMigratedIntoAscSendsBranchesAndTagsForTheImporter(t *testing.T) {
	fx := newAscMigrationFixture(t)
	restore := stubAscHolds(func(context.Context, *repo_model.Repository, string, string) error { return nil })
	defer restore()

	gitIn(t, fx.repo.RepoPath(), "", "config", "--local", "http.followRedirects", "false")
	ctx := ascsync.WithImporter(t.Context(), 42)
	require.NoError(t, importMigratedIntoAsc(ctx, fx.repo, "main"))
	assert.NotContains(t, gitIn(t, fx.repo.RepoPath(), "", "config", "--local", "--list"), "http.", "asc refuses a bridge with an [http] section")

	require.Len(t, fx.fake.imports, 1)
	req := fx.fake.imports[0]
	assert.Equal(t, int64(42), req.GetPusherId(), "the import is attributed to the importing user")
	assert.Equal(t, fx.repo.RepoPath(), req.GetGitDir())
	names := []string{}
	for _, u := range req.GetUpdates() {
		names = append(names, u.GetName())
	}
	assert.Equal(t, []string{"refs/heads/main", "refs/tags/v1"}, names, "forge namespaces are not imported")
	assert.Empty(t, gitIn(t, fx.repo.RepoPath(), "", "for-each-ref", "refs/pull", "refs/merge-requests"))

	// The history is looked at after the import, the findings say where they come from, and the default branch
	// becomes the trunk asc protects only at the end.
	assert.Equal(t, []string{"refs/heads/main"}, fx.secret.revs)
	assert.Equal(t, map[uint64]string{7: ascsync.ImportedHistoryNote}, fx.secret.noted)
	assert.Equal(t, []string{"main"}, fx.fake.trunks)
}

func TestImportMigratedIntoAscSurfacesTheRefusal(t *testing.T) {
	fx := newAscMigrationFixture(t)
	fx.fake.refuse = status.Error(codes.InvalidArgument, "path policy: .env is not allowed")
	restore := stubAscHolds(func(context.Context, *repo_model.Repository, string, string) error {
		t.Error("nothing may be waited for after a refusal")
		return nil
	})
	defer restore()

	err := importMigratedIntoAsc(t.Context(), fx.repo, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "asc refused the imported history")
	assert.Contains(t, err.Error(), "path policy: .env is not allowed")
	assert.Empty(t, fx.secret.revs, "nothing is scanned after a refusal")
	assert.Empty(t, fx.fake.trunks, "the default branch is not made the trunk after a refusal")
}

func TestImportMigratedIntoAscFailsWhenAscIsOff(t *testing.T) {
	fx := newAscMigrationFixture(t)
	setting.Asc.Enabled = false
	err := importMigratedIntoAsc(t.Context(), fx.repo, "main")
	require.ErrorIs(t, err, ascsync.ErrDisabled)
}

func TestImportMigratedIntoAscWaitsUntilTheHubReadsTheBranch(t *testing.T) {
	fx := newAscMigrationFixture(t)
	calls := 0
	restore := stubAscHolds(func(_ context.Context, _ *repo_model.Repository, branch, want string) error {
		calls++
		assert.Equal(t, "main", branch)
		assert.Equal(t, fx.commit, want)
		if calls < 3 {
			return assert.AnError
		}
		return nil
	})
	defer restore()

	require.NoError(t, importMigratedIntoAsc(t.Context(), fx.repo, "main"))
	assert.Equal(t, 3, calls)
}

func TestImportMigratedIntoAscFailsWhenTheHubNeverReadsTheBranch(t *testing.T) {
	fx := newAscMigrationFixture(t)
	restore := stubAscHolds(func(context.Context, *repo_model.Repository, string, string) error { return assert.AnError })
	defer restore()

	err := importMigratedIntoAsc(t.Context(), fx.repo, "main")
	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), `asc does not hold the imported branch "main"`)
}

// However a migration into asc ends, the repository must not keep the placeholder as the branch asc protects: its
// real default branch (or the instance's) becomes the trunk, so a secret pushed afterwards is judged by asc's gate.
func TestAscMigrationNeverLeavesThePlaceholderTrunk(t *testing.T) {
	oldDefault := setting.Repository.DefaultBranch
	setting.Repository.DefaultBranch = "main"
	t.Cleanup(func() { setting.Repository.DefaultBranch = oldDefault })

	t.Run("empty source", func(t *testing.T) {
		fx := newAscMigrationFixture(t)
		fx.repo.DefaultBranch = ascImportTrunk
		require.NoError(t, settleEmptyAscMigration(t.Context(), fx.repo))
		assert.Equal(t, "main", fx.repo.DefaultBranch, "the placeholder is not a default branch")
		assert.Equal(t, []string{"main"}, fx.fake.trunks)

		fx = newAscMigrationFixture(t)
		fx.repo.DefaultBranch = "trunk"
		require.NoError(t, settleEmptyAscMigration(t.Context(), fx.repo))
		assert.Equal(t, []string{"trunk"}, fx.fake.trunks, "the branch the source names")
	})

	t.Run("a refused import", func(t *testing.T) {
		fx := newAscMigrationFixture(t)
		fx.fake.refuse = status.Error(codes.InvalidArgument, "path policy: .env is not allowed")
		fx.repo.DefaultBranch = "trunk"
		ctx, tracker := withTrunkTracker(t.Context())
		require.Error(t, importMigratedIntoAsc(ctx, fx.repo, "main"))
		assert.False(t, tracker.set)
		settleAscTrunk(ctx, fx.repo, tracker)
		assert.Equal(t, []string{"trunk"}, fx.fake.trunks)
		assert.True(t, tracker.set)
	})

	t.Run("a cancelled migration", func(t *testing.T) {
		fx := newAscMigrationFixture(t)
		fx.repo.DefaultBranch = ascImportTrunk
		ctx, cancel := context.WithCancel(t.Context())
		ctx, tracker := withTrunkTracker(ctx)
		cancel()
		settleAscTrunk(ctx, fx.repo, tracker)
		assert.Equal(t, []string{"main"}, fx.fake.trunks, "the repair does not stop with the migration")
	})

	t.Run("a finished migration is left alone", func(t *testing.T) {
		fx := newAscMigrationFixture(t)
		stubHolds := stubAscHolds(func(context.Context, *repo_model.Repository, string, string) error { return nil })
		defer stubHolds()
		ctx, tracker := withTrunkTracker(t.Context())
		require.NoError(t, importMigratedIntoAsc(ctx, fx.repo, "main"))
		assert.True(t, tracker.set)
		before := len(fx.fake.trunks)
		settleAscTrunk(ctx, fx.repo, tracker)
		assert.Len(t, fx.fake.trunks, before, "the real trunk was set once")
		assert.Equal(t, []string{"main"}, fx.fake.trunks)
	})
}
