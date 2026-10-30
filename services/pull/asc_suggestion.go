// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"

	"github.com/google/uuid"
)

// "Apply as vN": a reviewer's suggested change (an asc thread comment with replacement text for its lines) becomes the
// next version of the change (SPEC 20.5, docs/asc/ASC-NATIVE-UI.md).
//
//  1. Review.PrepareSuggestion answers the file as it would be, or says the suggestion no longer fits. It is asked as the
//     person (the call is a write call).
//  2. Changes.AmendChange commits it as the next version of the change: the same parents, author and message, the committer
//     being the person. It is asked with the hub's own credential, because the person who applies a suggestion is the
//     author of the pull request or a maintainer, not necessarily the owner of the change in asc. The hub has checked
//     the right first (AscMayRestoreVersion). asc moves refs/pull/<n>/head only while it still points at the newest
//     version; a push that came first wins and the answer is stale. User draft refs never move.
//  3. The hub moves refs/pull/<n>/head to the new commit as a restore does (when asc's own post-receive has not done so
//     already) and runs the follow-up of a push: the stack base, the conflict check, stale reviews, the push comment.
//  4. Review.MarkSuggestionApplied records the version, as the person.
//
// Every step is asked again by the server that owns it; nothing here is trusted by asc.

// AscSuggestion is a suggested change of an asc thread that is still open.
type AscSuggestion struct {
	ThreadID, CommentID uint64
	Path                string
	StartLine, EndLine  uint32
	Author              string
	Suggestion          string
	Version             uint32 // the version the thread is on
	Stale               bool   // the thread is on an older version than the newest
	Body                string
}

// AscSuggestionStale says why a suggestion cannot be applied now. Nothing was written.
type AscSuggestionStale struct{ Reason string }

func (e *AscSuggestionStale) Error() string { return "the suggestion no longer fits: " + e.Reason }

// AscSuggestionResult says what an apply did.
type AscSuggestionResult struct {
	Version   uint32
	Changeset string // hex
	Commit    string // the git commit of the new version
	// Pending is true when the commit is not in the repository yet: the head follows when asc's bridge has it.
	Pending bool
	// MarkFailed is true when the version was made but asc did not record that the suggestion applied.
	MarkFailed bool
}

var (
	ascReviewMu  sync.Mutex
	ascReviewCli ascv1.ReviewClient

	// ascReviewFor returns the Review client. Tests replace it.
	ascReviewFor = func() (ascv1.ReviewClient, error) {
		ascReviewMu.Lock()
		defer ascReviewMu.Unlock()
		if ascReviewCli == nil {
			if setting.InternalToken == "" {
				return nil, errors.New("the internal token is not set")
			}
			c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: 30 * time.Second})
			if err != nil {
				return nil, fmt.Errorf("asc client: %w", err)
			}
			ascReviewCli = ascv1.NewReviewClient(c)
		}
		return ascReviewCli, nil
	}

	// ascSuggestionWait is how long an apply waits for the new commit to reach the repository, and how often it looks.
	ascSuggestionWait = 15 * time.Second
	ascSuggestionPoll = 500 * time.Millisecond
)

const ascMaxSuggestionThreads = 200

// AscSuggestions lists the suggested changes of the open threads of the change of pr, oldest thread first. It reads as the
// hub (the pull request page is for people who may read it). A suggestion that is applied already is left out.
func AscSuggestions(ctx context.Context, pr *issues_model.PullRequest) ([]AscSuggestion, error) {
	name, change, err := ascChangeOf(ctx, pr)
	if err != nil {
		return nil, err
	}
	versions, err := AscVersions(ctx, pr)
	if err != nil {
		return nil, err
	}
	api, err := ascReviewFor()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscUnavailable, err)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), 30*time.Second)
	defer cancel()
	resp, err := api.ListThreads(cctx, &ascv1.ListThreadsRequest{
		Repo: &ascv1.RepoRef{Name: name}, Change: change, Status: ascv1.ThreadStatus_THREAD_STATUS_OPEN, Limit: ascMaxSuggestionThreads,
	})
	if err != nil {
		return nil, ErrAscUnavailable
	}
	latest := versions.Latest()
	var out []AscSuggestion
	for _, t := range resp.GetThreads() {
		a := t.GetAnchor()
		if a == nil || len(a.GetPath()) == 0 || a.GetStartLine() == 0 {
			continue
		}
		for _, c := range t.GetComments() {
			if c.GetSuggestion() == "" || c.GetAppliedVersion() != 0 {
				continue
			}
			out = append(out, AscSuggestion{
				ThreadID: t.GetId(), CommentID: c.GetId(), Path: string(a.GetPath()),
				StartLine: a.GetStartLine(), EndLine: max(a.GetEndLine(), a.GetStartLine()),
				Author: c.GetAuthor(), Suggestion: c.GetSuggestion(), Body: c.GetBody(),
				Version: a.GetVersion(), Stale: a.GetVersion() != 0 && a.GetVersion() != latest,
			})
		}
	}
	return out, nil
}

// AscApplySuggestion applies the suggestion of comment commentID in thread threadID as the next version of the change
// of pr, as doer. The permission is the one of a restore (the poster, an owner or administrator, or a writer when the pull
// request allows maintainer edits). A suggestion that no longer fits, or a pull request that moved, is an
// *AscSuggestionStale and nothing was written.
func AscApplySuggestion(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, threadID, commentID uint64) (*AscSuggestionResult, error) {
	if threadID == 0 || commentID == 0 {
		return nil, ErrAscBadVersion
	}
	allowed, err := AscMayRestoreVersion(ctx, pr, doer)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAscRestoreForbidden
	}
	open, err := AscPullIsOpen(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, ErrAscRestoreNotOpen
	}
	name, change, err := ascChangeOf(ctx, pr)
	if err != nil {
		return nil, err
	}
	if ascUserCall == nil {
		return nil, ErrAscUnavailable
	}
	userCtx, ok, err := ascUserCall(ctx, doer, pr.BaseRepo)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscUnavailable, err)
	}
	if !ok {
		return nil, ErrAscRestoreForbidden
	}
	review, err := ascReviewFor()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscUnavailable, err)
	}
	changes, err := ascChangesFor()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscUnavailable, err)
	}
	repo := &ascv1.RepoRef{Name: name}

	// The pull request must be at the newest version: a suggestion applies to the newest one, and a restored pull request
	// would lose the other versions' work.
	versions, err := AscVersions(ctx, pr)
	if err != nil {
		return nil, err
	}
	current, err := ascHeads.Head(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("head of pull request %d: %w", pr.ID, err)
	}
	if versions.ByGitCommit(current) != versions.Latest() {
		return nil, &AscSuggestionStale{Reason: "the pull request is not at the newest version"}
	}

	prepCtx, cancel := context.WithTimeout(context.WithoutCancel(userCtx), 30*time.Second)
	defer cancel()
	prep, err := review.PrepareSuggestion(prepCtx, &ascv1.PrepareSuggestionRequest{Repo: repo, Change: change, ThreadId: threadID, CommentId: commentID})
	if err != nil {
		return nil, ErrAscUnavailable
	}
	if prep.GetStale() {
		return nil, &AscSuggestionStale{Reason: prep.GetReason()}
	}
	if prep.GetVersion() != versions.Latest() {
		return nil, &AscSuggestionStale{Reason: "the thread is on an older version"}
	}

	opID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	amendCtx, cancelAmend := context.WithTimeout(ascclient.WithOpID(ascclient.WithSystem(ctx), opID), 60*time.Second)
	defer cancelAmend()
	amended, err := changes.AmendChange(amendCtx, &ascv1.AmendChangeRequest{
		Repo: repo, Change: change, BaseVersion: prep.GetVersion(),
		Files:         []*ascv1.AmendFile{{Path: prep.GetPath(), Content: prep.GetNewContent()}},
		UpdateRef:     fmt.Sprintf("refs/pull/%d/head", pr.Index),
		CommitterName: doer.Name, CommitterEmail: doer.GetPlaceholderEmail(),
	})
	if err != nil {
		return nil, ErrAscUnavailable
	}
	if amended.GetStale() {
		return nil, &AscSuggestionStale{Reason: amended.GetReason()}
	}
	res := &AscSuggestionResult{Version: amended.GetVersion(), Changeset: hex.EncodeToString(amended.GetChangeset()), Commit: hex.EncodeToString(amended.GetGit())}
	log.Info("asc audit: apply suggestion: pull request %d (repository %d, index %d): user %d applied comment %d of thread %d as version %d",
		pr.ID, pr.BaseRepoID, pr.Index, doer.ID, commentID, threadID, res.Version)

	// asc's own post-receive may have moved the head already; otherwise it is moved here, once the commit is in the repository.
	if res.Commit != "" && !ascWaitForCommit(ctx, pr, res.Commit) {
		res.Pending = true
	} else if res.Commit != "" {
		if head, err := ascHeads.Head(ctx, pr); err == nil && head != res.Commit {
			if err := ascHeads.Move(ctx, pr, res.Commit); err != nil {
				log.Error("asc apply suggestion: move head of pull request %d: %v", pr.ID, err)
				res.Pending = true
			} else {
				pr.HeadCommitID = res.Commit
				ascRestoreFollowUp(ctx, pr, doer, head, res.Commit)
			}
		}
	} else {
		res.Pending = true
	}

	markCtx, cancelMark := context.WithTimeout(context.WithoutCancel(userCtx), 30*time.Second)
	defer cancelMark()
	if _, err := review.MarkSuggestionApplied(markCtx, &ascv1.MarkSuggestionAppliedRequest{
		Repo: repo, Change: change, ThreadId: threadID, CommentId: commentID, Version: res.Version,
	}); err != nil {
		log.Warn("asc apply suggestion: MarkSuggestionApplied for pull request %d: %v", pr.ID, err)
		res.MarkFailed = true
	}
	return res, nil
}

// ascWaitForCommit waits until the repository holds the commit, at most ascSuggestionWait.
func ascWaitForCommit(ctx context.Context, pr *issues_model.PullRequest, commit string) bool {
	deadline := time.Now().Add(ascSuggestionWait)
	for {
		if has, err := ascHeads.HasCommit(ctx, pr, commit); err == nil && has {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(ascSuggestionPoll):
		}
	}
}

// Lines is the lines the suggestion replaces, as a label ("12" or "12-15").
func (s AscSuggestion) Lines() string {
	if s.EndLine > s.StartLine {
		return fmt.Sprintf("%d-%d", s.StartLine, s.EndLine)
	}
	return fmt.Sprintf("%d", s.StartLine)
}

// Preview is the suggestion cut to a few lines for the page.
func (s AscSuggestion) Preview() string {
	lines := strings.Split(strings.TrimRight(s.Suggestion, "\n"), "\n")
	if len(lines) > 20 {
		lines = append(lines[:20], "…")
	}
	return strings.Join(lines, "\n")
}
