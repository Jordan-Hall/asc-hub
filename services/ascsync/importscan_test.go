// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascsync

import (
	"context"
	"net"
	"sync"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// adminAndScan serves RepoAdmin and SecretScan and records what it is asked.
type adminAndScan struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedSecretScanServer
	mu       sync.Mutex
	trunks   []string
	trunkErr error
	head     string
	revs     []string
	failRev  string
	findings map[string][]*ascv1.SecretFinding
	noted    map[uint64]string
}

func (s *adminAndScan) RepoAdmin(_ context.Context, req *ascv1.RepoAdminRequest) (*ascv1.RepoAdminResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trunkErr != nil {
		return nil, s.trunkErr
	}
	s.trunks = append(s.trunks, req.GetSetDefaultBranch())
	return &ascv1.RepoAdminResponse{}, nil
}

func (s *adminAndScan) ListRefs(context.Context, *ascv1.ListRefsRequest) (*ascv1.ListRefsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &ascv1.ListRefsResponse{Head: s.head}, nil
}

func (s *adminAndScan) Scan(_ context.Context, req *ascv1.ScanSecretsRequest) (*ascv1.ScanSecretsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revs = append(s.revs, req.GetRev())
	if req.GetRev() == s.failRev {
		return nil, status.Error(codes.Unavailable, "down")
	}
	return &ascv1.ScanSecretsResponse{Findings: s.findings[req.GetRev()]}, nil
}

func (s *adminAndScan) UpdateFinding(_ context.Context, req *ascv1.UpdateSecretFindingRequest) (*ascv1.UpdateSecretFindingResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noted[req.GetId()] = req.GetNote()
	return &ascv1.UpdateSecretFindingResponse{}, nil
}

func serveAdminAndScan(t *testing.T, s *adminAndScan) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, s)
	ascv1.RegisterSecretScanServer(srv, s)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	conn, err := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	r1, r2 := UseClient(ascv1.NewRepoClient(conn)), UseSecretClient(ascv1.NewSecretScanClient(conn))
	t.Cleanup(func() {
		r1()
		r2()
		_ = conn.Close()
		srv.Stop()
	})
}

func TestSetDefaultBranchTellsAsc(t *testing.T) {
	fx := newFixture(t)
	s := &adminAndScan{noted: map[uint64]string{}}
	serveAdminAndScan(t, s)

	require.NoError(t, SetDefaultBranch(t.Context(), fx.repo, "main"))
	assert.Equal(t, []string{"main"}, s.trunks)

	require.ErrorIs(t, SetDefaultBranch(t.Context(), fx.repo, ""), ErrBadRequest)
	other := *fx.repo
	other.StorageBackend = "git"
	require.ErrorIs(t, SetDefaultBranch(t.Context(), &other, "main"), ErrNotAsc)

	// A refusal of asc (an asc that does not know the call) is returned, so the caller can fail closed.
	s.trunkErr = status.Error(codes.InvalidArgument, "no operation")
	_, isReject := RejectMessage(SetDefaultBranch(t.Context(), fx.repo, "main"))
	assert.True(t, isReject)
}

func TestScanImportedHistoryScansTipsAndMarksFindings(t *testing.T) {
	fx := newFixture(t)
	open := ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN
	s := &adminAndScan{
		noted: map[uint64]string{},
		findings: map[string][]*ascv1.SecretFinding{
			"refs/heads/main": {{Id: 1, State: open}, {Id: 2, State: open, Note: "someone wrote this"}},
			"refs/heads/dev":  {{Id: 1, State: open}, {Id: 3, State: ascv1.SecretFindingState_SECRET_FINDING_STATE_REVOKED}},
		},
	}
	serveAdminAndScan(t, s)

	n, err := ScanImportedHistory(t.Context(), fx.repo, "main", []string{"dev", "main", "zeta"})
	require.NoError(t, err)
	assert.Equal(t, 3, n, "findings counted once each")
	assert.Equal(t, []string{"refs/heads/main", "refs/heads/dev", "refs/heads/zeta"}, s.revs, "the default branch first")
	assert.Equal(t, map[uint64]string{1: ImportedHistoryNote}, s.noted, "only open findings without a note are marked")

	// A scan that fails is reported, and the other tips are still scanned.
	s.revs, s.noted, s.failRev = nil, map[uint64]string{}, "refs/heads/main"
	_, err = ScanImportedHistory(t.Context(), fx.repo, "main", []string{"main", "dev"})
	require.Error(t, err)
	assert.Equal(t, []string{"refs/heads/main", "refs/heads/dev"}, s.revs)
}

func TestTrunkOfReadsTheBranchAscProtects(t *testing.T) {
	fx := newFixture(t)
	s := &adminAndScan{noted: map[uint64]string{}, head: "refs/heads/main"}
	serveAdminAndScan(t, s)

	got, err := TrunkOf(t.Context(), fx.repo)
	require.NoError(t, err)
	assert.Equal(t, "main", got)

	// An asc that reports none leaves the caller unable to tell, which is not an error.
	s.head = ""
	got, err = TrunkOf(t.Context(), fx.repo)
	require.NoError(t, err)
	assert.Empty(t, got)

	s.head = "refs/tags/v1"
	_, err = TrunkOf(t.Context(), fx.repo)
	require.ErrorIs(t, err, ErrBadAnswer)
}

// The first push to an empty repository is judged by the branch it creates: asc is told that branch whenever the trunk
// asc reports is another one (the hub's own value does not matter), and when asc refuses the push the trunk goes back.
func TestTakeTrunkComparesWithTheTrunkAscReportsAndRestoresIt(t *testing.T) {
	fx := newFixture(t)
	fx.repo.DefaultBranch = "main"
	s := &adminAndScan{noted: map[uint64]string{}, head: "refs/heads/" + ImportTrunkPlaceholder}
	serveAdminAndScan(t, s)

	// asc kept the placeholder while the hub says main, and the push creates main: asc is told, though hub and branch agree.
	restore, err := TakeTrunk(t.Context(), fx.repo, "main")
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, s.trunks)
	// The push is refused: the trunk does not go back to the placeholder (which would leave the repository ungated); it
	// stays the hub's default branch, which is the branch it was just made.
	restore()
	assert.Equal(t, []string{"main"}, s.trunks)

	// The trunk is the branch already: nothing is said, and nothing is restored.
	s.trunks, s.head = nil, "refs/heads/trunk"
	restore, err = TakeTrunk(t.Context(), fx.repo, "trunk")
	require.NoError(t, err)
	restore()
	assert.Empty(t, s.trunks)

	// A first push to another branch while asc protects main: the new branch is the trunk for this push, and a refusal puts main back.
	s.head = "refs/heads/main"
	restore, err = TakeTrunk(t.Context(), fx.repo, "other")
	require.NoError(t, err)
	assert.Equal(t, []string{"other"}, s.trunks)
	restore()
	assert.Equal(t, []string{"other", "main"}, s.trunks)

	// An asc that does not report a trunk is told in any case.
	s.trunks, s.head = nil, ""
	_, err = TakeTrunk(t.Context(), fx.repo, "main")
	require.NoError(t, err)
	assert.Equal(t, []string{"main"}, s.trunks)

	// asc does not take it: the caller refuses the push.
	s.trunkErr = status.Error(codes.Unavailable, "down")
	_, err = TakeTrunk(t.Context(), fx.repo, "zzz")
	require.Error(t, err)
}
