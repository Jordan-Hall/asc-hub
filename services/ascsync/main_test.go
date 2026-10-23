// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	_ "forgejo.org/modules/git/gitcli"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func testRun(m *testing.M) error {
	gitHomePath, err := os.MkdirTemp(os.TempDir(), "git-home")
	if err != nil {
		return fmt.Errorf("unable to create temp dir: %w", err)
	}
	defer util.RemoveAll(gitHomePath)
	setting.Git.HomePath = gitHomePath

	if err = git.InitFull(context.Background()); err != nil {
		return fmt.Errorf("failed to call Init: %w", err)
	}
	if exitCode := m.Run(); exitCode != 0 {
		return fmt.Errorf("run test failed, ExitCode=%d", exitCode)
	}
	return nil
}

func TestMain(m *testing.M) {
	if err := testRun(m); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Test failed: %v", err)
		os.Exit(1)
	}
}

// gitRun runs git in dir for test set-up and returns its trimmed stdout.
func gitRun(t *testing.T, dir string, env []string, stdin string, args ...string) string {
	t.Helper()
	cmd := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...)
	opts := &git.RunOpts{Dir: dir}
	if env != nil {
		opts.Env = append(os.Environ(), env...)
	}
	if stdin != "" {
		opts.Stdin = strings.NewReader(stdin)
	}
	stdout, stderr, err := cmd.RunStdString(opts)
	require.NoError(t, err, stderr)
	return strings.TrimSpace(stdout)
}

// fixture is a storage_backend=asc repository: a bare repository standing for the git bridge below a
// temporary repository root, and a fake asc server the package talks to.
type fixture struct {
	t      *testing.T
	repo   *repo_model.Repository
	bridge string
	fake   *fakeAsc
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	oldRoot, oldEnabled, oldToken, oldWait := setting.RepoRootPath, setting.Asc.Enabled, setting.InternalToken, bridgeWait
	setting.RepoRootPath, setting.Asc.Enabled, setting.InternalToken = root, true, "internal-token-for-tests"
	bridgeWait = 2 * time.Second
	t.Cleanup(func() {
		setting.RepoRootPath, setting.Asc.Enabled, setting.InternalToken, bridgeWait = oldRoot, oldEnabled, oldToken, oldWait
	})

	repo := &repo_model.Repository{OwnerName: "Owner", Name: "Repo", StorageBackend: "asc", ObjectFormatName: "sha1"}
	bridge := repo.RepoPath()
	require.NoError(t, os.MkdirAll(bridge, 0o755))
	gitRun(t, bridge, nil, "", "init", "-q", "--bare", "-b", "master")

	fake := &fakeAsc{
		t: t, bridge: bridge,
		known: map[string][]byte{}, back: map[string]string{}, refs: map[string][]byte{},
	}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, fake)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	conn, err := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	restore := UseClient(ascv1.NewRepoClient(conn))
	t.Cleanup(func() {
		restore()
		_ = conn.Close()
		srv.Stop()
	})
	return &fixture{t: t, repo: repo, bridge: bridge, fake: fake}
}

// commit makes a commit with no files in the bridge and returns its id.
func (fx *fixture) commit(msg string, parents ...string) string {
	fx.t.Helper()
	tree := gitRun(fx.t, fx.bridge, nil, "", "hash-object", "-t", "tree", "-w", "--stdin")
	env := []string{
		"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_AUTHOR_DATE=2026-01-01T00:00:00 +0000",
		"GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com", "GIT_COMMITTER_DATE=2026-01-01T00:00:00 +0000",
	}
	args := []string{"commit-tree", tree, "-m", msg}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	return gitRun(fx.t, fx.bridge, env, "", args...)
}

// bridgeHas reports the value of ref in the bridge, or "" when it is absent.
func (fx *fixture) bridgeRef(ref string) string {
	fx.t.Helper()
	out, err := bridgeRef(fx.t.Context(), fx.bridge, ref)
	require.NoError(fx.t, err)
	return out
}

// fakeAsc is an asc.v1.Repo server with scripted behaviour. It stands for `asc serve`: it keeps the git map and
// the refs, and its bridge_promote effect (promote) sets the bridge ref the way the real one does after an
// UpdateRefs.
type fakeAsc struct {
	ascv1.UnimplementedRepoServer
	t      *testing.T
	bridge string

	mu      sync.Mutex
	calls   []string
	known   map[string][]byte // git hex -> changeset id
	back    map[string]string // changeset id (string) -> git hex
	refs    map[string][]byte // ref -> changeset id
	imports []*ascv1.ImportGitPushRequest
	updates []*ascv1.UpdateRefsRequest
	gitMaps []*ascv1.GitMapRequest

	// importErr and updateErr, when set, are returned by the next call of that kind.
	importErr error
	updateErr error
	// importLearns makes ImportGitPush learn the commits it is asked about before it fails.
	importLearns bool
	// importShort makes ImportGitPush answer with no results at all.
	importShort bool
	// promoteDelay is how long the bridge takes to follow an UpdateRefs; a negative value never follows.
	promoteDelay time.Duration
}

func (f *fakeAsc) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeAsc) callNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// learn teaches the fake a commit and returns the changeset id it was given.
func (f *fakeAsc) learn(gitHex string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.learnLocked(gitHex)
}

func (f *fakeAsc) learnLocked(gitHex string) []byte {
	if id, ok := f.known[gitHex]; ok {
		return id
	}
	id := make([]byte, 32)
	copy(id, "cs-"+gitHex)
	f.known[gitHex] = id
	f.back[string(id)] = gitHex
	return id
}

func (f *fakeAsc) GitMap(_ context.Context, req *ascv1.GitMapRequest) (*ascv1.GitMapResponse, error) {
	f.record("GitMap")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gitMaps = append(f.gitMaps, req)
	resp := &ascv1.GitMapResponse{}
	for _, g := range req.GetGit() {
		resp.GitToAsc = append(resp.GitToAsc, f.known[fmt.Sprintf("%x", g)])
	}
	for _, c := range req.GetAsc() {
		var raw []byte
		if hexID, ok := f.back[string(c)]; ok {
			raw = mustHex(hexID)
		}
		resp.AscToGit = append(resp.AscToGit, raw)
	}
	return resp, nil
}

func (f *fakeAsc) ListRefs(_ context.Context, req *ascv1.ListRefsRequest) (*ascv1.ListRefsResponse, error) {
	f.record("ListRefs")
	f.mu.Lock()
	defer f.mu.Unlock()
	resp := &ascv1.ListRefsResponse{}
	for name, id := range f.refs {
		if strings.HasPrefix(name, req.GetPrefix()) {
			resp.Refs = append(resp.Refs, &ascv1.Ref{Name: name, Target: &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: id}}})
		}
	}
	return resp, nil
}

func (f *fakeAsc) ImportGitPush(_ context.Context, req *ascv1.ImportGitPushRequest) (*ascv1.ImportGitPushResponse, error) {
	f.record("ImportGitPush")
	f.mu.Lock()
	f.imports = append(f.imports, req)
	err := f.importErr
	f.importErr = nil
	if err != nil && f.importLearns {
		for _, u := range req.GetUpdates() {
			f.learnLocked(fmt.Sprintf("%x", u.GetNewGit()))
		}
	}
	if err != nil {
		f.mu.Unlock()
		return nil, err
	}
	resp := &ascv1.ImportGitPushResponse{}
	for _, u := range req.GetUpdates() {
		newHex := fmt.Sprintf("%x", u.GetNewGit())
		if strings.Trim(newHex, "0") != "" {
			id := f.learnLocked(newHex)
			f.refs[u.GetName()] = id
			f.mu.Unlock()
			f.promote(u.GetName(), newHex)
			f.mu.Lock()
		}
		resp.Results = append(resp.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	if f.importShort {
		resp.Results = nil
	}
	f.mu.Unlock()
	return resp, nil
}

func (f *fakeAsc) UpdateRefs(_ context.Context, req *ascv1.UpdateRefsRequest) (*ascv1.UpdateRefsResponse, error) {
	f.record("UpdateRefs")
	f.mu.Lock()
	f.updates = append(f.updates, req)
	err := f.updateErr
	f.updateErr = nil
	if err != nil {
		f.mu.Unlock()
		return nil, err
	}
	resp := &ascv1.UpdateRefsResponse{}
	for _, u := range req.GetUpdates() {
		if u.GetDelete() {
			delete(f.refs, u.GetName())
			f.mu.Unlock()
			f.promote(u.GetName(), "")
			f.mu.Lock()
		} else {
			id := u.GetNew().GetChangeset()
			f.refs[u.GetName()] = id
			target := f.back[string(id)]
			f.mu.Unlock()
			f.promote(u.GetName(), target)
			f.mu.Lock()
		}
		resp.Results = append(resp.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	f.mu.Unlock()
	return resp, nil
}

// promote is the stand-in for bridge_promote: it sets the bridge ref to asc's value after promoteDelay.
func (f *fakeAsc) promote(ref, gitHex string) {
	if f.promoteDelay < 0 {
		return
	}
	run := func() {
		if gitHex == "" {
			_ = gitCmd(f.bridge, "update-ref", "-d", ref)
			return
		}
		_ = gitCmd(f.bridge, "update-ref", ref, gitHex)
	}
	if f.promoteDelay == 0 {
		run()
		return
	}
	time.AfterFunc(f.promoteDelay, run)
}

func gitCmd(dir string, args ...string) error {
	_, _, err := git.NewCommand(context.Background()).AddArguments(git.ToTrustedCmdArgs(args)...).RunStdString(&git.RunOpts{Dir: dir})
	return err
}

func mustHex(s string) []byte {
	b := make([]byte, len(s)/2)
	for i := range b {
		var v byte
		_, _ = fmt.Sscanf(s[2*i:2*i+2], "%02x", &v)
		b[i] = v
	}
	return b
}
