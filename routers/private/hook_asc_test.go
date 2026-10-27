// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"context"
	"net/http"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/json"
	"forgejo.org/modules/private"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
	app_context "forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ascZeroID   = "0000000000000000000000000000000000000000"
	ascCommitID = "1111111111111111111111111111111111111111"
)

// fakeAscClient answers ImportGitPush only; any other call is a test failure (the nil embedded client panics).
type fakeAscClient struct {
	ascv1.RepoClient
	reqs []*ascv1.ImportGitPushRequest
	err  error
}

func (f *fakeAscClient) ImportGitPush(_ context.Context, in *ascv1.ImportGitPushRequest, _ ...grpc.CallOption) (*ascv1.ImportGitPushResponse, error) {
	f.reqs = append(f.reqs, in)
	if f.err != nil {
		return nil, f.err
	}
	resp := &ascv1.ImportGitPushResponse{}
	for _, u := range in.GetUpdates() {
		resp.Results = append(resp.Results, &ascv1.RefResult{Name: u.GetName()})
	}
	return resp, nil
}

// useAsc enables [asc] and installs a fake asc client for the test.
func useAsc(t *testing.T) *fakeAscClient {
	t.Helper()
	old := setting.Asc.Enabled
	setting.Asc.Enabled = true
	fake := &fakeAscClient{}
	restore := ascsync.UseClient(fake)
	t.Cleanup(func() {
		restore()
		setting.Asc.Enabled = old
	})
	return fake
}

// ascRepo loads repository 1 (user2/repo1) with the given storage backend, in memory.
func ascRepo(t *testing.T, backend string) *repo_model.Repository {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repo.StorageBackend = backend
	return repo
}

// importCall runs the pre-receive asc step for a push of refs and returns whether the push goes on and the
// response written when it does not.
func importCall(t *testing.T, repo *repo_model.Repository, opts *private.HookOptions) (bool, int, private.Response) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "/")
	ctx.Repo = &app_context.Repository{Repository: repo}
	ok := (&preReceiveContext{PrivateContext: ctx, opts: opts}).importIntoAsc()
	var out private.Response
	if resp.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return ok, resp.Code, out
}

func pushOpts(refs ...string) *private.HookOptions {
	opts := &private.HookOptions{UserID: 2, UserName: "user2"}
	for _, r := range refs {
		opts.RefFullNames = append(opts.RefFullNames, git.RefName(r))
		opts.OldCommitIDs = append(opts.OldCommitIDs, ascZeroID)
		opts.NewCommitIDs = append(opts.NewCommitIDs, ascCommitID)
	}
	return opts
}

func TestPreReceiveImportsIntoAsc(t *testing.T) {
	fake := useAsc(t)
	repo := ascRepo(t, "asc")

	ok, _, _ := importCall(t, repo, pushOpts("refs/heads/feature", "refs/for/master/topic"))
	require.True(t, ok)
	require.Len(t, fake.reqs, 1)
	req := fake.reqs[0]
	assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
	assert.Equal(t, repo.RepoPath(), req.GetGitDir())
	assert.Equal(t, int64(2), req.GetPusherId())
	require.Len(t, req.GetUpdates(), 1, "an AGit ref is not sent")
	assert.Equal(t, "refs/heads/feature", req.GetUpdates()[0].GetName())
}

func TestPreReceiveAscBranchIsSkippedWhenAscHasThePush(t *testing.T) {
	fake := useAsc(t)

	for name, tc := range map[string]struct {
		backend string
		mutate  func(*private.HookOptions)
	}{
		"a git repository":          {"git", func(*private.HookOptions) {}},
		"a dual repository":         {"dual", func(*private.HookOptions) {}},
		"a call made by asc itself": {"asc", func(o *private.HookOptions) { o.PushTrigger = ascsync.PushTriggerAsc }},
		"a wiki push":               {"asc", func(o *private.HookOptions) { o.IsWiki = true }},
		"only an AGit ref":          {"asc", func(o *private.HookOptions) { o.RefFullNames = []git.RefName{"refs/for/master"} }},
	} {
		t.Run(name, func(t *testing.T) {
			opts := pushOpts("refs/heads/feature")
			tc.mutate(opts)
			ok, _, _ := importCall(t, ascRepo(t, tc.backend), opts)
			assert.True(t, ok)
			assert.Empty(t, fake.reqs)
		})
	}
}

func TestPreReceiveAscRefusalRejectsThePush(t *testing.T) {
	fake := useAsc(t)
	repo := ascRepo(t, "asc")

	fake.err = status.Error(codes.FailedPrecondition, "! [rejected] refs/heads/feature (fetch first)")
	ok, code, out := importCall(t, repo, pushOpts("refs/heads/feature"))
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "! [rejected] refs/heads/feature (fetch first)", out.UserMsg)
}

func TestPreReceiveAscFailureRejectsWithoutDetails(t *testing.T) {
	fake := useAsc(t)
	repo := ascRepo(t, "asc")

	fake.err = status.Error(codes.Unavailable, "dial tcp 10.1.2.3:7890: secret detail")
	ok, code, out := importCall(t, repo, pushOpts("refs/heads/feature"))
	assert.False(t, ok)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.NotEmpty(t, out.UserMsg)
	assert.NotContains(t, out.UserMsg+out.Err, "10.1.2.3")
	assert.NotContains(t, out.UserMsg+out.Err, "secret")
}

func TestPreReceiveAscRepositoryIsNeverWrittenThroughGitWhenAscIsOff(t *testing.T) {
	fake := useAsc(t)
	setting.Asc.Enabled = false
	ok, code, _ := importCall(t, ascRepo(t, "asc"), pushOpts("refs/heads/feature"))
	assert.False(t, ok)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Empty(t, fake.reqs)
}

func TestPreReceiveUnknownStorageBackendRejects(t *testing.T) {
	fake := useAsc(t)
	ok, code, _ := importCall(t, ascRepo(t, "ASC"), pushOpts("refs/heads/feature"))
	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Empty(t, fake.reqs)
}

// ---------------------------------------------------------------------------------- post-receive

// postCall runs the post-receive asc step and returns whether it answered, with the status and body.
func postCall(t *testing.T, opts *private.HookOptions) (bool, int, string) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "/")
	answered := ascOwnsFanOut(ctx, opts, "user2", "repo1")
	return answered, resp.Code, resp.Body.String()
}

func setBackend(t *testing.T, backend string) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repo.StorageBackend = backend
	require.NoError(t, repo_model.UpdateRepositoryCols(db.DefaultContext, repo, "storage_backend"))
}

func postOpts() *private.HookOptions {
	opts := pushOpts("refs/heads/master")
	opts.OldCommitIDs[0] = ascCommitID
	return opts
}

func TestPostReceiveIgnoresGitOriginCallsForAscRepositories(t *testing.T) {
	useAsc(t)
	setBackend(t, "asc")

	for _, trigger := range []repo_module.PushTrigger{"", repo_module.PushTriggerPRUpdateWithBase} {
		opts := postOpts()
		opts.PushTrigger = trigger
		answered, code, body := postCall(t, opts)
		assert.True(t, answered, trigger)
		assert.Equal(t, http.StatusOK, code, trigger)
		var res private.HookPostReceiveResult
		require.NoError(t, json.Unmarshal([]byte(body), &res))
		assert.Empty(t, res.Results)
		assert.Empty(t, res.Err)
	}
}

func TestPostReceiveProcessesAscCallsAsAlways(t *testing.T) {
	useAsc(t)
	setBackend(t, "asc")
	opts := postOpts()
	opts.PushTrigger = ascsync.PushTriggerAsc
	answered, _, body := postCall(t, opts)
	assert.False(t, answered)
	assert.Empty(t, body)
}

func TestPostReceiveLeavesOtherRepositoriesAlone(t *testing.T) {
	useAsc(t)
	for _, backend := range []string{"git", "dual"} {
		setBackend(t, backend)
		answered, _, body := postCall(t, postOpts())
		assert.False(t, answered, backend)
		assert.Empty(t, body, backend)
	}

	setBackend(t, "asc")
	wiki := postOpts()
	wiki.IsWiki = true
	answered, _, _ := postCall(t, wiki)
	assert.False(t, answered, "a wiki is plain git")

	setting.Asc.Enabled = false
	answered, _, _ = postCall(t, postOpts())
	assert.False(t, answered, "nothing is looked up while [asc] is off")
}

func TestPostReceiveFailsOnAnUnusableStorageBackend(t *testing.T) {
	useAsc(t)
	setBackend(t, "ASC")
	answered, code, _ := postCall(t, postOpts())
	assert.True(t, answered)
	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestPostReceiveMarksAMergedPullRequestOfACgRepository(t *testing.T) {
	useAsc(t)
	setBackend(t, "asc")
	pr, err := issues_model.GetUnmergedPullRequest(db.DefaultContext, 1, 1, "branch2", "master", issues_model.PullRequestFlowGithub)
	require.NoError(t, err)
	require.NoError(t, pr.LoadBaseRepo(db.DefaultContext))
	user1 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	require.NoError(t, pull_model.ScheduleAutoMerge(db.DefaultContext, user1, pr.ID, repo_model.MergeStyleSquash, "squash merge a pr", false))

	opts := postOpts()
	opts.PushTrigger = repo_module.PushTriggerPRMergeToBase
	opts.PullRequestID = pr.ID
	opts.NewCommitIDs[0] = "01234567"
	answered, code, _ := postCall(t, opts)
	assert.True(t, answered)
	assert.Equal(t, http.StatusOK, code)

	pr, err = issues_model.GetPullRequestByID(db.DefaultContext, pr.ID)
	require.NoError(t, err)
	assert.True(t, pr.HasMerged)
	assert.Equal(t, "01234567", pr.MergedCommitID)
}

// ---------------------------------------------------------------------------------- post-receive fan-out of asc

func settleOpts(trigger repo_module.PushTrigger, userID int64) *private.HookOptions {
	return &private.HookOptions{
		UserID:       userID,
		PushTrigger:  trigger,
		RefFullNames: []git.RefName{"refs/heads/qa-removed"},
		OldCommitIDs: []string{ascCommitID},
		NewCommitIDs: []string{ascZeroID},
	}
}

func settle(t *testing.T, opts *private.HookOptions) {
	t.Helper()
	ctx, _ := contexttest.MockPrivateContext(t, "/")
	settleAscFanOut(ctx, opts, "user2", "repo1")
}

func TestPostReceiveOfAscGetsTheRememberedPusherBack(t *testing.T) {
	useAsc(t)
	setBackend(t, "asc")
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	deletion := ascsync.PushRef{Name: "refs/heads/qa-removed", Old: ascCommitID, New: ascZeroID}
	remember := func() ascsync.PushToken {
		tok, err := ascsync.RememberPusher(repo, 2, []ascsync.PushRef{deletion})
		require.NoError(t, err)
		return tok
	}
	remember()

	// asc names itself as the pusher, so the call arrives without a user.
	opts := settleOpts(ascsync.PushTriggerAsc, 0)
	settle(t, opts)
	assert.Equal(t, int64(2), opts.UserID)
	assert.Equal(t, "user2", opts.UserName)

	// The push is attributed once: a second call for the same ref change gets nobody.
	opts = settleOpts(ascsync.PushTriggerAsc, 0)
	old := ascsync.SetFanOutWaitForTest(50 * time.Millisecond)
	defer old()
	settle(t, opts)
	assert.Zero(t, opts.UserID, "a remembered push is used once")

	// Only a call without a user is given one: a user that asc did name, and the Actions user, are kept.
	remember()
	for _, id := range []int64{5, user_model.ActionsUserID} {
		opts = settleOpts(ascsync.PushTriggerAsc, id)
		settle(t, opts)
		assert.Equal(t, id, opts.UserID)
	}

	// A git-origin call is not touched, and neither is a push nobody remembers.
	opts = settleOpts("", 0)
	settle(t, opts)
	assert.Zero(t, opts.UserID)
	opts = settleOpts(ascsync.PushTriggerAsc, 0)
	opts.NewCommitIDs[0] = ascCommitID
	opts.OldCommitIDs[0] = ascZeroID
	opts.RefFullNames[0] = "refs/heads/qa-unknown"
	settle(t, opts)
	assert.Zero(t, opts.UserID)

	// The remembered deletion is still there, and it is not claimed through another deletion of the same ref.
	opts = settleOpts(ascsync.PushTriggerAsc, 0)
	opts.OldCommitIDs[0] = "2222222222222222222222222222222222222222"
	settle(t, opts)
	assert.Zero(t, opts.UserID, "a deletion from another commit is another push")
	opts = settleOpts(ascsync.PushTriggerAsc, 0)
	settle(t, opts)
	assert.Equal(t, int64(2), opts.UserID)
}

func TestPostReceiveOfAscAttributesOnlyACallThatIsOnePush(t *testing.T) {
	useAsc(t)
	setBackend(t, "asc")
	defer ascsync.SetFanOutWaitForTest(50 * time.Millisecond)()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	a := ascsync.PushRef{Name: "refs/heads/qa-a", Old: ascZeroID, New: ascCommitID}
	b := ascsync.PushRef{Name: "refs/heads/qa-b", Old: ascZeroID, New: ascCommitID}
	_, err := ascsync.RememberPusher(repo, 2, []ascsync.PushRef{a})
	require.NoError(t, err)
	_, err = ascsync.RememberPusher(repo, 4, []ascsync.PushRef{b})
	require.NoError(t, err)

	opts := &private.HookOptions{
		PushTrigger:  ascsync.PushTriggerAsc,
		RefFullNames: []git.RefName{"refs/heads/qa-a", "refs/heads/qa-b"},
		OldCommitIDs: []string{ascZeroID, ascZeroID},
		NewCommitIDs: []string{ascCommitID, ascCommitID},
	}
	settle(t, opts)
	assert.Zero(t, opts.UserID, "ref changes of two users in one call are attributed to nobody")

	// A call whose lists disagree is not attributed either.
	opts = &private.HookOptions{
		PushTrigger:  ascsync.PushTriggerAsc,
		RefFullNames: []git.RefName{"refs/heads/qa-a", "refs/heads/qa-b"},
		OldCommitIDs: []string{ascZeroID},
		NewCommitIDs: []string{ascCommitID},
	}
	settle(t, opts)
	assert.Zero(t, opts.UserID)

	// Each push on its own is still attributed: nothing was used up by the refusals.
	for ref, want := range map[string]int64{"refs/heads/qa-a": 2, "refs/heads/qa-b": 4} {
		opts = &private.HookOptions{
			PushTrigger:  ascsync.PushTriggerAsc,
			RefFullNames: []git.RefName{git.RefName(ref)},
			OldCommitIDs: []string{ascZeroID},
			NewCommitIDs: []string{ascCommitID},
		}
		settle(t, opts)
		assert.Equal(t, want, opts.UserID, ref)
	}
}

// A push that asc refused or could not take must not leave its pusher behind for a later call to claim.
func TestPreReceiveForgetsThePusherUnlessTheImportSucceeds(t *testing.T) {
	fake := useAsc(t)
	repo := ascRepo(t, "asc")
	claim := func() (int64, bool) {
		return ascsync.ClaimPusher(repo, []ascsync.PushRef{{Name: "refs/heads/feature", Old: ascZeroID, New: ascCommitID}})
	}

	for { // earlier tests leave the pushes they did not claim
		if _, found := claim(); !found {
			break
		}
	}
	for name, err := range map[string]error{
		"a refusal":          status.Error(codes.FailedPrecondition, "! [rejected] refs/heads/feature (fetch first)"),
		"an unavailable asc": status.Error(codes.Unavailable, "down"),
	} {
		fake.err = err
		ok, _, _ := importCall(t, repo, pushOpts("refs/heads/feature"))
		require.False(t, ok, name)
		_, found := claim()
		assert.False(t, found, name+": the pusher of a push that did not happen is forgotten")
	}

	fake.err = nil
	ok, _, _ := importCall(t, repo, pushOpts("refs/heads/feature"))
	require.True(t, ok)
	id, found := claim()
	assert.True(t, found, "an accepted push is remembered for the outbox call")
	assert.Equal(t, int64(2), id)
	_, found = claim()
	assert.False(t, found, "and used once")
}
