// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	_ "forgejo.org/modules/git/gitcli"
	_ "forgejo.org/modules/repostore/asc"
	_ "forgejo.org/modules/repostore/gitcli"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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
func gitRun(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...)
	opts := &git.RunOpts{Dir: dir}
	if env != nil {
		opts.Env = append(os.Environ(), env...)
	}
	stdout, stderr, err := cmd.RunStdString(opts)
	require.NoError(t, err, stderr)
	return strings.TrimSpace(stdout)
}

// fixture is a storage_backend=asc repository below a temporary repository root.
type fixture struct {
	t    *testing.T
	repo *repo_model.Repository
	fake *fakeAsc
}

// newFixture sets [asc] up for one test and returns a repository Owner/Repo stored in asc. mode is [asc] BRIDGE.
func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	oldRoot, oldAsc, oldToken := setting.RepoRootPath, setting.Asc, setting.InternalToken
	setting.RepoRootPath, setting.InternalToken = t.TempDir(), "internal-token-for-tests"
	setting.Asc.Enabled, setting.Asc.Bridge = true, mode
	t.Cleanup(func() { setting.RepoRootPath, setting.Asc, setting.InternalToken = oldRoot, oldAsc, oldToken })

	fake := newFake()
	t.Cleanup(UseClient(fake))
	return &fixture{
		t:    t,
		repo: &repo_model.Repository{OwnerName: "Owner", Name: "Repo", DefaultBranch: "main", StorageBackend: "asc", ObjectFormatName: "sha1"},
		fake: fake,
	}
}

// commit makes a commit with no files in the bridge, optionally on top of parents, and returns its id.
func (fx *fixture) commit(dir, msg string, parents ...string) string {
	fx.t.Helper()
	tree := gitRun(fx.t, dir, nil, "mktree")
	env := []string{
		"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_AUTHOR_DATE=2026-01-01T00:00:00 +0000",
		"GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com", "GIT_COMMITTER_DATE=2026-01-01T00:00:00 +0000",
	}
	args := []string{"commit-tree", tree, "-m", msg}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	return gitRun(fx.t, dir, env, args...)
}

// fakeAsc is the part of asc.v1.Repo that this package uses, scripted by the tests.
type fakeAsc struct {
	ascv1.RepoClient

	mu      sync.Mutex
	refs    map[string]string // ref -> git id (hex) in asc
	calls   []string
	imports []*ascv1.ImportGitPushRequest
	updates []*ascv1.UpdateRefsRequest
	// importErr is returned by ImportGitPush for a request that names the ref.
	importErr map[string]error
}

func newFake() *fakeAsc {
	return &fakeAsc{refs: map[string]string{}, importErr: map[string]error{}}
}

func (f *fakeAsc) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeAsc) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

// set moves a ref of the fake asc.
func (f *fakeAsc) set(ref, gitHex string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if gitHex == "" {
		delete(f.refs, ref)
		return
	}
	f.refs[ref] = gitHex
}

// ascOf and gitOf are the fake's id mapping: a changeset id is the 20 git bytes and 12 bytes of 0xaa.
func ascOf(gitHex string) []byte {
	raw, err := hex.DecodeString(gitHex)
	if err != nil || len(raw) != 20 {
		panic("ascOf: not a git id: " + gitHex)
	}
	return append(raw, bytes.Repeat([]byte{0xaa}, 12)...)
}

func (f *fakeAsc) ListRefs(_ context.Context, in *ascv1.ListRefsRequest, _ ...grpc.CallOption) (*ascv1.ListRefsResponse, error) {
	f.record("ListRefs")
	f.mu.Lock()
	defer f.mu.Unlock()
	resp := &ascv1.ListRefsResponse{}
	for name, id := range f.refs {
		if strings.HasPrefix(name, in.GetPrefix()) {
			resp.Refs = append(resp.Refs, &ascv1.Ref{Name: name, Target: &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: ascOf(id)}}})
		}
	}
	return resp, nil
}

func (f *fakeAsc) GitMap(_ context.Context, in *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	f.record("GitMap")
	out := &ascv1.GitMapResponse{}
	for _, g := range in.GetGit() {
		out.GitToAsc = append(out.GitToAsc, append(bytes.Clone(g), bytes.Repeat([]byte{0xaa}, 12)...))
	}
	for _, c := range in.GetAsc() {
		out.AscToGit = append(out.AscToGit, bytes.Clone(c[:20]))
	}
	return out, nil
}

func (f *fakeAsc) ImportGitPush(_ context.Context, in *ascv1.ImportGitPushRequest, _ ...grpc.CallOption) (*ascv1.ImportGitPushResponse, error) {
	f.record("ImportGitPush")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imports = append(f.imports, in)
	out := &ascv1.ImportGitPushResponse{}
	for _, u := range in.GetUpdates() {
		if err := f.importErr[u.GetName()]; err != nil {
			return nil, err
		}
		f.refs[u.GetName()] = fmt.Sprintf("%x", u.GetNewGit())
		out.Results = append(out.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	return out, nil
}

func (f *fakeAsc) Resolve(_ context.Context, in *ascv1.ResolveRequest, _ ...grpc.CallOption) (*ascv1.ResolveResponse, error) {
	f.record("Resolve")
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ascv1.ResolveResponse{}
	for _, rev := range in.GetRevspecs() {
		if id, ok := f.refs[rev]; ok {
			out.Ids = append(out.Ids, ascOf(id)[:20])
		} else {
			out.Ids = append(out.Ids, nil)
		}
	}
	return out, nil
}

func (f *fakeAsc) UpdateRefs(_ context.Context, in *ascv1.UpdateRefsRequest, _ ...grpc.CallOption) (*ascv1.UpdateRefsResponse, error) {
	f.record("UpdateRefs")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, in)
	out := &ascv1.UpdateRefsResponse{}
	for _, u := range in.GetUpdates() {
		if u.GetDelete() {
			delete(f.refs, u.GetName())
		} else {
			f.refs[u.GetName()] = fmt.Sprintf("%x", u.GetNew().GetChangeset()[:20])
		}
		out.Results = append(out.Results, &ascv1.RefResult{Name: u.GetName(), New: u.GetNew()})
	}
	return out, nil
}
