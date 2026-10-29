// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	pull_model "forgejo.org/models/pull"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	notify_service "forgejo.org/services/notify"
)

// SetReviewState notifier (SPEC section 20.5, PLAN unit C35).
//
// Forgejo owns the social state of a pull request (reviews, checks, queue); asc owns the forge-neutral change
// facts. After every event that changes a stacked pull request's summary (opened, synchronized, reviewed, a
// review dismissed, merged, closed or reopened, a check reported, a run finished) Forgejo pushes the summary
// back to asc with Changes.SetReviewState, so `asc stack`, the TUI and the plugins show it without calling a
// forge API.
//
// Properties:
//   - Only a pull request that has a pull_request_asc_stack row, in a base repository with storage_backend=asc,
//     while [asc] ENABLED is true, is ever reported. Nothing about any other pull request leaves Forgejo.
//   - The call is a system call (internal token); SetReviewState accepts no other credential. The change id
//     comes from Forgejo's own link row and is checked again before it is sent.
//   - The summary is read from the database when the call is made, never from the event, so a late or
//     repeated event sends current facts. It holds counts and fixed words, never review text or user names.
//   - A failure is logged and dropped: it never fails the user's action. asc treats the summary as a cache and
//     the next event repairs it.
//   - The call runs off the request path, bounded by CallTimeout.

// ReviewStateHost is the ReviewState.host value Forgejo sends.
const ReviewStateHost = "forgejo"

// maxApprovals clamps the approvals count; maxVersions clamps the reviewed version.
const (
	maxApprovals = 1 << 16
	maxVersions  = 1 << 16
)

// ReviewState words (SPEC section 8.8 json: state, checks, queue).
const (
	StateDraft  = "draft"
	StateOpen   = "open"
	StateMerged = "merged"
	StateClosed = "closed"

	ChecksNone    = "none"
	ChecksPending = "pending"
	ChecksSuccess = "success"
	ChecksFailure = "failure"

	QueueNone   = "none"
	QueueQueued = "queued"
)

var (
	changesMu  sync.Mutex
	changesVal ascv1.ChangesClient

	newChanges = func() (ascv1.ChangesClient, error) {
		if setting.InternalToken == "" {
			return nil, errors.New("ascsync: the internal token is not set")
		}
		c, err := ascclient.New(ascclient.Config{
			Target:        setting.Asc.ServerURL,
			InternalToken: setting.InternalToken,
			CallTimeout:   CallTimeout,
		})
		if err != nil {
			return nil, err
		}
		return c.Changes(), nil
	}

	// dispatch runs f off the caller's path. Tests make it synchronous.
	dispatch = func(f func()) { go f() }
	// loadFacts reads the summary inputs of pr. Tests replace it.
	loadFacts = loadPullFacts
)

// UseChangesClient replaces the Changes client until the returned function is called. It is for tests.
func UseChangesClient(api ascv1.ChangesClient) (restore func()) {
	changesMu.Lock()
	defer changesMu.Unlock()
	prev := changesVal
	changesVal = api
	return func() {
		changesMu.Lock()
		defer changesMu.Unlock()
		changesVal = prev
	}
}

func changesAPI() (ascv1.ChangesClient, error) {
	changesMu.Lock()
	defer changesMu.Unlock()
	if changesVal == nil {
		api, err := newChanges()
		if err != nil {
			return nil, fmt.Errorf("ascsync: asc client: %w", err)
		}
		changesVal = api
	}
	return changesVal, nil
}

// PullFacts is what the summary of one stacked pull request is built from.
type PullFacts struct {
	Repo      *repo_model.Repository // base repository
	Index     int64
	URL       string
	Change    string // 32 letters k-z
	Merged    bool
	Closed    bool
	Draft     bool
	Approvals int
	Checks    string // one of the Checks words
	Queued    bool
	// ReviewedVersion is the newest version of the change that a published review looked at, or 0.
	ReviewedVersion int
}

// BuildReviewState turns facts into the message sent to asc. The change id is checked here too.
func BuildReviewState(f *PullFacts, now time.Time) (change []byte, st *ascv1.ReviewState, err error) {
	if f == nil || f.Repo == nil || f.Index <= 0 {
		return nil, nil, fmt.Errorf("%w: review state facts", ErrBadRequest)
	}
	change, err = changeBytes(f.Change)
	if err != nil {
		return nil, nil, err
	}
	state := StateOpen
	switch {
	case f.Merged:
		state = StateMerged
	case f.Closed:
		state = StateClosed
	case f.Draft:
		state = StateDraft
	}
	checks := f.Checks
	switch checks {
	case ChecksNone, ChecksPending, ChecksSuccess, ChecksFailure:
	default:
		checks = ChecksNone
	}
	live := state != StateMerged && state != StateClosed
	queue := QueueNone
	if f.Queued && live {
		queue = QueueQueued
	}
	approvals := min(max(f.Approvals, 0), maxApprovals)
	return change, &ascv1.ReviewState{
		Host:            ReviewStateHost,
		Number:          uint64(f.Index),
		Url:             clean(f.URL, 2048),
		State:           state,
		Draft:           f.Draft && live,
		Approvals:       uint32(approvals),
		Checks:          checks,
		Queue:           queue,
		ReviewedVersion: uint32(min(max(f.ReviewedVersion, 0), maxVersions)),
		Updated:         now.Unix(),
	}, nil
}

// changeBytes decodes the text form of a change id: letter c is the hex digit 'z' - c, high digit first.
func changeBytes(change string) ([]byte, error) {
	if !issues_model.IsAscChange(change) {
		return nil, fmt.Errorf("%w: change id", ErrBadRequest)
	}
	out := make([]byte, 16)
	for i := range out {
		out[i] = (('z' - change[2*i]) << 4) | ('z' - change[2*i+1])
	}
	return out, nil
}

// SendReviewState reports f to asc. It returns ErrNotAsc, ErrDisabled or ErrBadRequest, without a call, when
// the repository or the facts do not qualify.
func SendReviewState(ctx context.Context, f *PullFacts) error {
	if f == nil || f.Repo == nil {
		return fmt.Errorf("%w: no repository", ErrBadRequest)
	}
	ok, err := BackedBy(f.Repo)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotAsc
	}
	if !setting.Asc.Enabled {
		return ErrDisabled
	}
	change, st, err := BuildReviewState(f, time.Now())
	if err != nil {
		return err
	}
	name, err := repoName(setting.RepoRootPath, f.Repo.RepoPath())
	if err != nil {
		return err
	}
	api, err := changesAPI()
	if err != nil {
		return err
	}
	cctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	_, err = api.SetReviewState(cctx, &ascv1.SetReviewStateRequest{
		Repo:    &ascv1.RepoRef{Name: name},
		Changes: [][]byte{change},
		States:  []*ascv1.ReviewState{st},
	})
	return callError("SetReviewState", err)
}

// loadPullFacts reads the summary inputs from the database and git. It returns nil for a pull request that
// is not a stacked pull request of an asc repository.
func loadPullFacts(ctx context.Context, pr *issues_model.PullRequest) (*PullFacts, error) {
	if pr == nil || pr.ID == 0 {
		return nil, nil
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil || link == nil {
		return nil, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if ok, err := BackedBy(pr.BaseRepo); err != nil || !ok {
		return nil, err
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return nil, err
	}
	if pr.Issue.Repo == nil {
		pr.Issue.Repo = pr.BaseRepo
	}
	f := &PullFacts{
		Repo:   pr.BaseRepo,
		Index:  pr.Issue.Index,
		URL:    pr.Issue.HTMLURL(),
		Change: link.AscChange,
		Merged: pr.HasMerged,
		Closed: pr.Issue.IsClosed,
		Draft:  pr.IsWorkInProgress(ctx),
		Checks: ChecksNone,
	}
	reviews, err := issues_model.GetReviewsByIssueID(ctx, pr.IssueID)
	if err != nil {
		return nil, err
	}
	for _, r := range reviews {
		if r.Type == issues_model.ReviewTypeApprove && !r.Stale && !r.Dismissed {
			f.Approvals++
		}
	}
	// When the versions cannot be read the summary is not sent: a wrong 0 would replace a right number in asc.
	if src := versionCommitSource(); src != nil {
		commits, err := src(ctx, pr)
		if err != nil {
			return nil, fmt.Errorf("versions of pull request %d: %w", pr.ID, err)
		}
		f.ReviewedVersion = reviewedVersionOf(reviews, commits)
	}
	if sha := headCommit(ctx, pr); sha != "" {
		statuses, _, err := git_model.GetLatestCommitStatus(ctx, pr.BaseRepoID, sha, db.ListOptions{ListAll: true})
		if err != nil {
			return nil, err
		}
		f.Checks = checksWord(git_model.CalcCommitStatus(statuses))
	}
	if exists, _, err := pull_model.GetScheduledMergeByPullID(ctx, pr.ID); err == nil && exists {
		f.Queued = true
	}
	return f, nil
}

// VersionCommitsFunc lists the git commits of the versions of the change of pr, oldest first: version N is
// element N-1. Versions belong to asc and services/pull reads and verifies them (AscVersions); that package
// imports this one, so it registers the function here (RegisterVersionCommits) instead of being imported.
type VersionCommitsFunc func(ctx context.Context, pr *issues_model.PullRequest) ([]string, error)

var (
	versionCommitsMu  sync.Mutex
	versionCommitsFor VersionCommitsFunc
)

// RegisterVersionCommits sets the source of version commits and returns a function that restores the previous
// one. Without a source the reviewed version is reported as 0.
func RegisterVersionCommits(f VersionCommitsFunc) (restore func()) {
	versionCommitsMu.Lock()
	defer versionCommitsMu.Unlock()
	prev := versionCommitsFor
	versionCommitsFor = f
	return func() {
		versionCommitsMu.Lock()
		defer versionCommitsMu.Unlock()
		versionCommitsFor = prev
	}
}

func versionCommitSource() VersionCommitsFunc {
	versionCommitsMu.Lock()
	defer versionCommitsMu.Unlock()
	return versionCommitsFor
}

// reviewedVersionOf is the newest version whose git commit a published review (approve, comment or reject, not
// dismissed, stale or not) was made on; commits[i] is the git commit of version i+1. A review of a commit that
// is no version counts for nothing. A commit that two versions share is the later version.
func reviewedVersionOf(reviews []*issues_model.Review, commits []string) int {
	best := 0
	for _, r := range reviews {
		if r == nil || r.Dismissed {
			continue
		}
		switch r.Type {
		case issues_model.ReviewTypeApprove, issues_model.ReviewTypeComment, issues_model.ReviewTypeReject:
		default:
			continue
		}
		if len(r.CommitID) != 40 {
			continue
		}
		for i := len(commits) - 1; i >= best; i-- {
			if strings.EqualFold(commits[i], r.CommitID) {
				best = i + 1
				break
			}
		}
	}
	return best
}

func headCommit(ctx context.Context, pr *issues_model.PullRequest) string {
	if pr.HeadCommitID != "" {
		return pr.HeadCommitID
	}
	id, err := git.GetFullCommitID(ctx, pr.BaseRepo.RepoPath(), pr.GetGitRefName())
	if err != nil {
		return ""
	}
	return id
}

func checksWord(s *git_model.CommitStatus) string {
	if s == nil {
		return ChecksNone
	}
	switch {
	case s.State.IsSuccess(), s.State.IsSkipped(), s.State.IsWarning():
		return ChecksSuccess
	case s.State.IsPending():
		return ChecksPending
	default:
		return ChecksFailure
	}
}

// NotifyPullRequest reports the current summary of pr to asc, off the caller's path. It is a no-op when [asc]
// is disabled or pr is not a stacked pull request of an asc repository.
func NotifyPullRequest(ctx context.Context, pr *issues_model.PullRequest) {
	if !setting.Asc.Enabled || pr == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	dispatch(func() { reportPull(ctx, pr) })
}

func reportPull(ctx context.Context, pr *issues_model.PullRequest) {
	f, err := loadFacts(ctx, pr)
	if err != nil {
		log.Error("ascsync: review state of pull request %d: %v", pr.ID, err)
		return
	}
	if f == nil {
		return
	}
	if err := SendReviewState(ctx, f); err != nil {
		log.Error("ascsync: SetReviewState for pull request %d: %v", pr.ID, err)
	}
}

// NotifyCommitStatus reports the pull requests whose head is sha in repo, after a check changed. It is a
// no-op for a repository that is not stored in asc.
func NotifyCommitStatus(ctx context.Context, repo *repo_model.Repository, sha string) {
	if !setting.Asc.Enabled || repo == nil || sha == "" {
		return
	}
	if ok, err := BackedBy(repo); err != nil || !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	dispatch(func() {
		gitRepo, err := gitrepo.OpenRepository(ctx, repo)
		if err != nil {
			log.Error("ascsync: open repository for check event: %v", err)
			return
		}
		refs, err := gitRepo.GetRefsBySha(sha, git.PullPrefix)
		gitRepo.Close()
		if err != nil {
			log.Error("ascsync: pull refs of check commit: %v", err)
			return
		}
		for _, ref := range refs {
			idx, ok := pullIndexOfRef(ref)
			if !ok {
				continue
			}
			pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, idx)
			if err != nil {
				continue
			}
			reportPull(ctx, pr)
		}
	})
}

// pullIndexOfRef parses refs/pull/<n>/head.
func pullIndexOfRef(ref string) (int64, bool) {
	rest, ok := strings.CutPrefix(ref, git.PullPrefix)
	if !ok {
		return 0, false
	}
	num, tail, ok := strings.Cut(rest, "/")
	if !ok || tail != "head" || num == "" || len(num) > 18 {
		return 0, false
	}
	var n int64
	for _, c := range num {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, n > 0
}

// reviewStateNotifier is registered with the notify service.
type reviewStateNotifier struct {
	notify_service.NullNotifier
}

var _ notify_service.Notifier = (*reviewStateNotifier)(nil)

func init() {
	notify_service.RegisterNotifier(&reviewStateNotifier{})
}

func (*reviewStateNotifier) NewPullRequest(ctx context.Context, pr *issues_model.PullRequest, _ []*user_model.User) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) PullRequestReview(ctx context.Context, pr *issues_model.PullRequest, _ *issues_model.Review, _ *issues_model.Comment, _ []*user_model.User) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) PullReviewDismiss(ctx context.Context, _ *user_model.User, review *issues_model.Review, _ *issues_model.Comment) {
	if !setting.Asc.Enabled || review == nil {
		return
	}
	if err := review.LoadIssue(ctx); err != nil {
		return
	}
	if err := review.Issue.LoadPullRequest(ctx); err != nil {
		return
	}
	NotifyPullRequest(ctx, review.Issue.PullRequest)
}

func (*reviewStateNotifier) MergePullRequest(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) AutoMergePullRequest(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) PullRequestSynchronized(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) PullRequestPushCommits(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest, _ *issues_model.Comment) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) PullRequestChangeTargetBranch(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest, _ string) {
	NotifyPullRequest(ctx, pr)
}

func (*reviewStateNotifier) IssueChangeTitle(ctx context.Context, _ *user_model.User, issue *issues_model.Issue, _ string) {
	notifyIssue(ctx, issue)
}

func (*reviewStateNotifier) IssueChangeStatus(ctx context.Context, _ *user_model.User, _ string, issue *issues_model.Issue, _ *issues_model.Comment, _ bool) {
	notifyIssue(ctx, issue)
}

func notifyIssue(ctx context.Context, issue *issues_model.Issue) {
	if !setting.Asc.Enabled || issue == nil || !issue.IsPull {
		return
	}
	if err := issue.LoadPullRequest(ctx); err != nil {
		return
	}
	NotifyPullRequest(ctx, issue.PullRequest)
}

// ActionRunNowDone is the check event for Actions: the run's commit may be the head of stacked pull requests.
// Statuses posted by anything else (the API, a commit status poster) reach NotifyCommitStatus from
// services/repository/commitstatus.CreateCommitStatus.
func (*reviewStateNotifier) ActionRunNowDone(ctx context.Context, run *actions_model.ActionRun, _ actions_model.Status, _ *actions_model.ActionRun) {
	if !setting.Asc.Enabled || run == nil || run.CommitSHA == "" {
		return
	}
	if err := run.LoadRepo(ctx); err != nil {
		return
	}
	NotifyCommitStatus(ctx, run.Repo, run.CommitSHA)
}
