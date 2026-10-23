// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build asc

package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	unit_model "forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/git/pushoptions"
	"forgejo.org/modules/private"
	"forgejo.org/modules/setting"
	"forgejo.org/services/agit"
	"forgejo.org/services/ascsync"
	pull_service "forgejo.org/services/pull"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// These tests run the writers of refs/pull/* against a repository with storage_backend=asc and a
// fake asc (PLAN unit K09, SPEC section 13.5). They prove that asc is asked first (an import, or the
// HOOK_MODE_SKIP_PRE update) and that the git bridge is written only by asc's bridge_promote: the fake plays it
// after a delay, and the writer must wait for it. The fake has no read methods (they are the nil interface and
// panic), so the tests stop before the code that goes on to read the pull request through asc; a real asc
// serves those reads.

const ascPromoteDelay = 200 * time.Millisecond

// ascFake is an in-process asc.v1.Repo client. Only the calls a pull request head write makes exist.
type ascFake struct {
	ascv1.RepoClient // nil: any other call panics

	t      *testing.T
	bridge string

	mu      sync.Mutex
	known   map[string][]byte // git hex -> changeset id
	back    map[string]string // changeset id -> git hex
	refs    map[string][]byte
	calls   []string
	imports []*ascv1.ImportGitPushRequest
	updates []*ascv1.UpdateRefsRequest

	importErr error // returned by the next ImportGitPush
	learn     bool  // the failing ImportGitPush still imports (asc imports before it judges the ref)
}

func (f *ascFake) learnLocked(hex string) []byte {
	if id, ok := f.known[hex]; ok {
		return id
	}
	id := make([]byte, 32)
	copy(id, "cs-"+hex)
	f.known[hex] = id
	f.back[string(id)] = hex
	return id
}

func (f *ascFake) call(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *ascFake) callNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *ascFake) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls, f.imports, f.updates = nil, nil, nil
}

// promote is bridge_promote: asc's value reaches the bridge after a delay.
func (f *ascFake) promote(ref, hex string) {
	time.AfterFunc(ascPromoteDelay, func() {
		args := []string{"update-ref", ref, hex}
		if hex == "" {
			args = []string{"update-ref", "-d", ref}
		}
		_, _, _ = git.NewCommand(context.Background()).AddArguments(git.ToTrustedCmdArgs(args)...).RunStdString(&git.RunOpts{Dir: f.bridge})
	})
}

func (f *ascFake) GitMap(_ context.Context, req *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	f.call("GitMap")
	f.mu.Lock()
	defer f.mu.Unlock()
	resp := &ascv1.GitMapResponse{}
	for _, g := range req.GetGit() {
		resp.GitToAsc = append(resp.GitToAsc, f.known[fmt.Sprintf("%x", g)])
	}
	for _, c := range req.GetAsc() {
		var raw []byte
		if h, ok := f.back[string(c)]; ok {
			raw = make([]byte, len(h)/2)
			for i := range raw {
				_, _ = fmt.Sscanf(h[2*i:2*i+2], "%02x", &raw[i])
			}
		}
		resp.AscToGit = append(resp.AscToGit, raw)
	}
	return resp, nil
}

func (f *ascFake) ListRefs(_ context.Context, req *ascv1.ListRefsRequest, _ ...grpc.CallOption) (*ascv1.ListRefsResponse, error) {
	f.call("ListRefs")
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

func (f *ascFake) ImportGitPush(_ context.Context, req *ascv1.ImportGitPushRequest, _ ...grpc.CallOption) (*ascv1.ImportGitPushResponse, error) {
	f.call("ImportGitPush")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imports = append(f.imports, req)
	resp := &ascv1.ImportGitPushResponse{}
	err := f.importErr
	f.importErr = nil
	for _, u := range req.GetUpdates() {
		hex := fmt.Sprintf("%x", u.GetNewGit())
		if err == nil || f.learn {
			id := f.learnLocked(hex)
			if err == nil {
				f.refs[u.GetName()] = id
				f.promote(u.GetName(), hex)
			}
		}
		resp.Results = append(resp.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (f *ascFake) UpdateRefs(_ context.Context, req *ascv1.UpdateRefsRequest, _ ...grpc.CallOption) (*ascv1.UpdateRefsResponse, error) {
	f.call("UpdateRefs")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, req)
	resp := &ascv1.UpdateRefsResponse{}
	for _, u := range req.GetUpdates() {
		if u.GetDelete() {
			delete(f.refs, u.GetName())
			f.promote(u.GetName(), "")
		} else {
			id := u.GetNew().GetChangeset()
			f.refs[u.GetName()] = id
			f.promote(u.GetName(), f.back[string(id)])
		}
		resp.Results = append(resp.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	return resp, nil
}

// ascRepoFixture is a repository that was made as a plain git repository and then moved to storage_backend=asc.
type ascRepoFixture struct {
	repo   *repo_model.Repository
	bridge string
	fake   *ascFake
}

func newAscRepoFixture(t *testing.T) *ascRepoFixture {
	t.Helper()
	repo := forgery.CreateRepository(t, nil, &forgery.CreateRepositoryOptions{Files: forgery.FilesInit{}})
	forgery.EnableRepoUnits(t, repo, unit_model.TypePullRequests)
	bridge := repo.RepoPath()
	fx := &ascRepoFixture{repo: repo, bridge: bridge}
	fx.fake = &ascFake{t: t, bridge: bridge, known: map[string][]byte{}, back: map[string]string{}, refs: map[string][]byte{}}

	// The commits that exist before the move are known to asc, as an import at the Stage 2 flip leaves them.
	main := fx.git(t, nil, "", "rev-parse", "refs/heads/"+repo.DefaultBranch)
	fx.fake.learnLocked(main)

	oldEnabled, oldToken := setting.Asc.Enabled, setting.InternalToken
	setting.Asc.Enabled, setting.InternalToken = true, "internal-token-for-tests"
	ascbackend.Configure(fx.fake, ascbackend.Options{})
	restore := ascsync.UseClient(fx.fake)

	repo.StorageBackend = "asc"
	require.NoError(t, repo_model.UpdateRepositoryCols(t.Context(), repo, "storage_backend"))
	t.Cleanup(func() {
		// back to git before the repository is deleted
		repo.StorageBackend = "git"
		require.NoError(t, repo_model.UpdateRepositoryCols(context.Background(), repo, "storage_backend"))
		restore()
		ascbackend.Reset()
		setting.Asc.Enabled, setting.InternalToken = oldEnabled, oldToken
	})
	return fx
}

func (fx *ascRepoFixture) git(t *testing.T, env []string, stdin string, args ...string) string {
	t.Helper()
	opts := &git.RunOpts{Dir: fx.bridge}
	if env != nil {
		opts.Env = append(os.Environ(), env...)
	}
	if stdin != "" {
		opts.Stdin = strings.NewReader(stdin)
	}
	out, stderr, err := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...).RunStdString(opts)
	require.NoError(t, err, stderr)
	return strings.TrimSpace(out)
}

// commit adds a commit with one file on top of parent in the bridge and returns its id.
func (fx *ascRepoFixture) commit(t *testing.T, parent, msg string) string {
	t.Helper()
	blob := fx.git(t, nil, msg, "hash-object", "-w", "--stdin")
	tree := fx.git(t, nil, fmt.Sprintf("100644 blob %s\t%s.txt\n", blob, strings.ReplaceAll(msg, " ", "-")), "mktree")
	env := []string{
		"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_AUTHOR_DATE=2026-01-01T00:00:00 +0000",
		"GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com", "GIT_COMMITTER_DATE=2026-01-01T00:00:00 +0000",
	}
	return fx.git(t, env, "", "commit-tree", tree, "-p", parent, "-m", msg)
}

func (fx *ascRepoFixture) bridgeRef(t *testing.T, ref string) string {
	t.Helper()
	id, err := git.GetFullCommitID(t.Context(), fx.bridge, ref)
	if err != nil {
		return ""
	}
	return id
}

// newPR describes a pull request of the fixture repository from head into the default branch.
func newPR(fx *ascRepoFixture, t *testing.T, head string) (*issues_model.PullRequest, *issues_model.Issue) {
	t.Helper()
	main := fx.git(t, nil, "", "rev-parse", "refs/heads/"+fx.repo.DefaultBranch)
	issue := &issues_model.Issue{RepoID: fx.repo.ID, Title: "WIP: head ref", PosterID: fx.repo.OwnerID, Poster: fx.repo.Owner, IsPull: true}
	pr := &issues_model.PullRequest{
		HeadRepoID: fx.repo.ID, BaseRepoID: fx.repo.ID, HeadRepo: fx.repo, BaseRepo: fx.repo,
		HeadBranch: head, BaseBranch: fx.repo.DefaultBranch, MergeBase: main,
		Type: issues_model.PullRequestGitea, Flow: issues_model.PullRequestFlowGithub,
	}
	return pr, issue
}

func TestPullHeadRefAsc(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		fx := newAscRepoFixture(t)
		main := fx.git(t, nil, "", "rev-parse", "refs/heads/"+fx.repo.DefaultBranch)
		feature1 := fx.commit(t, main, "feature one")
		fx.git(t, nil, "", "update-ref", "refs/heads/feature", feature1)

		pr, issue := newPR(fx, t, "feature")
		require.NoError(t, issues_model.NewPullRequest(t.Context(), fx.repo, issue, nil, nil, pr))
		head := pr.GetGitRefName()
		require.Equal(t, "refs/pull/1/head", head)

		// The head branch is synced into the base repository: asc learns the commit and the ref, and the bridge
		// follows only through promote, so the writer waits for it.
		start := time.Now()
		require.NoError(t, pull_service.PushToBaseRepo(t.Context(), pr))
		assert.GreaterOrEqual(t, time.Since(start), ascPromoteDelay-50*time.Millisecond, "the writer waited for bridge_promote")
		assert.Equal(t, feature1, fx.bridgeRef(t, head))
		require.Len(t, fx.fake.imports, 1)
		imp := fx.fake.imports[0]
		assert.Equal(t, strings.ToLower(fx.repo.OwnerName)+"/"+strings.ToLower(fx.repo.Name), imp.GetRepo().GetName())
		assert.Equal(t, head, imp.GetUpdates()[0].GetName())
		assert.Equal(t, fx.bridge+"/objects", imp.GetObjectDir())
		assert.Empty(t, fx.fake.updates)

		// The head moves forward: a fast-forward import, naming the old head.
		feature2 := fx.commit(t, feature1, "feature two")
		fx.git(t, nil, "", "update-ref", "refs/heads/feature", feature2)
		fx.fake.reset()
		require.NoError(t, pull_service.PushToBaseRepo(t.Context(), pr))
		assert.Equal(t, feature2, fx.bridgeRef(t, head))
		require.Len(t, fx.fake.imports, 1)
		assert.Equal(t, feature1, fmt.Sprintf("%x", fx.fake.imports[0].GetUpdates()[0].GetOldGit()))

		// The branch is rewritten: asc refuses the fast-forward after importing, and the ref is forced.
		rewritten := fx.commit(t, main, "rewritten")
		fx.git(t, nil, "", "update-ref", "refs/heads/feature", rewritten)
		fx.fake.reset()
		fx.fake.importErr = status.Error(codes.FailedPrecondition, "! [rejected] "+head+" (fetch first)")
		fx.fake.learn = true
		require.NoError(t, pull_service.PushToBaseRepo(t.Context(), pr))
		assert.Equal(t, rewritten, fx.bridgeRef(t, head))
		require.Len(t, fx.fake.updates, 1)
		assert.Equal(t, ascv1.HookMode_HOOK_MODE_SKIP_PRE, fx.fake.updates[0].GetHooks())
		assert.Equal(t, ascv1.UpdateMode_UPDATE_MODE_FORCE, fx.fake.updates[0].GetMode())

		// UpdateRef (AGit flow): the commit is already known, so asc only moves the ref.
		fx.fake.reset()
		pr.HeadCommitID = feature2
		fx.fake.mu.Lock()
		fx.fake.learnLocked(feature2)
		fx.fake.mu.Unlock()
		require.NoError(t, pull_service.UpdateRef(t.Context(), pr))
		assert.Equal(t, feature2, fx.bridgeRef(t, head))
		assert.Empty(t, fx.fake.imports)
		assert.Equal(t, []string{"GitMap", "UpdateRefs"}, fx.fake.callNames())
	})
}

func TestPullHeadRefAscFailureLeavesNothingBehind(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		fx := newAscRepoFixture(t)
		main := fx.git(t, nil, "", "rev-parse", "refs/heads/"+fx.repo.DefaultBranch)
		feature := fx.commit(t, main, "feature one")
		fx.git(t, nil, "", "update-ref", "refs/heads/feature", feature)
		pr, issue := newPR(fx, t, "feature")

		// asc is down: the pull request is not created, the bridge has no pull ref, and the clean-up asks asc to
		// delete the ref it may have taken.
		fx.fake.importErr = status.Error(codes.Unavailable, "asc is down")
		require.Error(t, pull_service.NewPullRequest(t.Context(), fx.repo, issue, nil, nil, pr, nil))
		assert.Empty(t, fx.bridgeRef(t, "refs/pull/1/head"))
		unittest.AssertNotExistsBean(t, &issues_model.PullRequest{BaseRepoID: fx.repo.ID, HeadBranch: "feature"})
		require.NotEmpty(t, fx.fake.updates)
		last := fx.fake.updates[len(fx.fake.updates)-1]
		assert.True(t, last.GetUpdates()[0].GetDelete())
		assert.Equal(t, ascv1.HookMode_HOOK_MODE_SKIP_PRE, last.GetHooks())
	})
}

func TestAgitAscReadsThePushedCommitFromTheBridge(t *testing.T) {
	onApplicationRun(t, func(t *testing.T, _ *url.URL) {
		fx := newAscRepoFixture(t)
		main := fx.git(t, nil, "", "rev-parse", "refs/heads/"+fx.repo.DefaultBranch)
		first := fx.commit(t, main, "agit one")

		// An AGit pull request that exists, with its head in the bridge.
		pr, issue := newPR(fx, t, strings.ToLower(fx.repo.OwnerName)+"/topic")
		pr.Flow = issues_model.PullRequestFlowAGit
		pr.HeadCommitID = first
		require.NoError(t, issues_model.NewPullRequest(t.Context(), fx.repo, issue, nil, nil, pr))
		fx.git(t, nil, "", "update-ref", pr.GetGitRefName(), first)

		gitRepo, err := git.OpenRepositoryWithBackend(t.Context(), fx.bridge, git.DefaultBackendName)
		require.NoError(t, err)
		defer gitRepo.Close()

		opts := func(old, new string) *private.HookOptions {
			return &private.HookOptions{
				UserID: fx.repo.OwnerID, UserName: fx.repo.OwnerName,
				OldCommitIDs: []string{old}, NewCommitIDs: []string{new},
				RefFullNames:   []git.RefName{git.RefName("refs/for/" + fx.repo.DefaultBranch + "/topic")},
				GitPushOptions: map[string]string{string(pushoptions.AgitTopic): "topic", string(pushoptions.AgitTitle): "WIP: agit"},
			}
		}

		// The pushed commit is in the bridge (git migrated it before proc-receive) and not in asc. The check for
		// a force push reads the bridge, so a diverged update is refused with the AGit message and not with an
		// error from asc, and nothing is asked of asc.
		diverged := fx.commit(t, main, "agit diverged")
		results, err := agit.ProcReceive(t.Context(), fx.repo, gitRepo, opts(first, diverged))
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Contains(t, results[0].Err, "force-push")
		assert.Empty(t, fx.fake.callNames())

		// A fast-forward of the head passes the check, and the new commit then goes to asc with the pull ref
		// (what follows reads the pull request through asc, which the fake does not serve).
		next := fx.commit(t, first, "agit two")
		func() {
			defer func() { _ = recover() }() // the fake has no reads; stop wherever the code first needs one
			_, _ = agit.ProcReceive(t.Context(), fx.repo, gitRepo, opts(first, next))
		}()
		assert.Equal(t, []string{"GitMap", "ListRefs", "ImportGitPush"}, fx.fake.callNames())
		assert.Equal(t, next, fx.bridgeRef(t, pr.GetGitRefName()))
	})
}
