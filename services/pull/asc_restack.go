// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"fmt"
	"sync"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/ascsync"
)

// Restack of a whole stack on the web (SPEC section 20.5, docs/BUILDOUT.md section 6).
//
// RestackAscStack asks asc to rebase the stack of a pull request onto the current target (Stacks.Restack) and
// then moves refs/pull/<n>/head of every pull request whose change asc rewrote. It is what the "Restack"
// button and the merge queue call when the parent of a change has landed or the target moved.
//
// Rules:
//   - The stack comes from the pull request's own asc link. asc restacks the whole stack the change belongs to,
//     so every pull request that asc would rewrite is checked first, on a dry run: it must be open, have an
//     open pull request in this repository, and doer must be allowed to update it (the poster, an owner or
//     admin, a writer when the pull request allows maintainer edits) or to merge it. One failure refuses the
//     whole call before anything is written.
//   - A conflict, a blocked change or a frozen change (one in a merge queue) refuses the call; nothing moves.
//     The dry run shows which change and which paths.
//   - A head is moved only for a pull request that was checked. Each move follows the path of a push: merge
//     base, conflict check, stale reviews, push comment, notification (ascRestoreFollowUp). An approval
//     survives a rewrite whose patch is unchanged (asc reports the change as unchanged but with a new head).
//   - doer must be allowed to rewrite the pull request the call is made on, before anything is asked of asc.
//   - The apply is bound to the preview that was authorised: it carries the head asc showed for each change it
//     may rewrite (ExpectedHeads), and when it answers, every change it rewrote must be one the preview listed,
//     with the same old head. Anything else is an error, and no head is moved.
//   - Restacks of one stack run one at a time in this process.
//   - Every restack is logged with the actor. Errors never carry the server's message.
//
// A head that asc rewrote but that could not be moved here is reported in the error and in Failed. A second
// restack finds the stack already on the target and does not move it again; move it by restoring its latest
// version.

// Errors of the stack restack.
var (
	ErrAscRestackNotAllowed = errors.New("not allowed to restack a pull request of the stack")
	ErrAscRestackNotOpen    = errors.New("the pull request is not open for changes")
	ErrAscRestackNoPull     = errors.New("a change of the stack has no open pull request")
	// ErrAscRestackNotPreviewed is returned when asc rewrote a change that the authorised preview did not allow.
	// asc has then changed the stack; no head is moved here.
	ErrAscRestackNotPreviewed = errors.New("asc rewrote a change that the authorised preview did not list")
	// ErrAscRestackNoPin is returned when a change to rewrite has no head to pin the apply to.
	ErrAscRestackNoPin = errors.New("a change to rewrite has no head to pin the restack to")
)

// ascRestackLocks serialises the restacks of one stack. The key is the repository and the bottom pull request
// of the stack; a waiter gives up with its context.
var ascRestackLocks = struct {
	sync.Mutex
	m map[string]*ascRestackLock
}{m: map[string]*ascRestackLock{}}

type ascRestackLock struct {
	held chan struct{}
	refs int
}

// lockAscRestack waits for the lock of the stack and returns the function that frees it.
func lockAscRestack(ctx context.Context, key string) (func(), error) {
	ascRestackLocks.Lock()
	l := ascRestackLocks.m[key]
	if l == nil {
		l = &ascRestackLock{held: make(chan struct{}, 1)}
		ascRestackLocks.m[key] = l
	}
	l.refs++
	ascRestackLocks.Unlock()

	release := func() {
		ascRestackLocks.Lock()
		defer ascRestackLocks.Unlock()
		if l.refs--; l.refs == 0 {
			delete(ascRestackLocks.m, key)
		}
	}
	select {
	case l.held <- struct{}{}:
		return func() {
			<-l.held
			release()
		}, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}

// ascMaxStackDepth bounds the walk to the bottom of a stack.
const ascMaxStackDepth = 1000

// ascRestackKey names the stack of pr: its repository and the bottom pull request, found by following the
// parent links. A pull request that cannot be followed to a bottom is its own stack.
func ascRestackKey(ctx context.Context, pr *issues_model.PullRequest, row *issues_model.PullRequestAscStack) (string, error) {
	bottom := pr.ID
	for range ascMaxStackDepth {
		if row == nil || row.StackParentID == 0 {
			break
		}
		bottom = row.StackParentID
		var err error
		if row, err = issues_model.GetPullRequestAscStack(ctx, bottom); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%d/%d", pr.BaseRepoID, bottom), nil
}

// AscRestackOptions are the options of RestackAscStack.
type AscRestackOptions struct {
	// DryRun only reports what a restack would do.
	DryRun bool
}

// AscRestackStackItem is one change of the stack, bottom to top. PR is nil for a change without an open pull
// request in this repository (a landed one, or one that was never submitted).
type AscRestackStackItem struct {
	PR         *issues_model.PullRequest
	Change     string
	State      ascbridge.RestackState
	OldHead    string
	NewHead    string
	NewVersion uint32
	Conflicts  []string
}

// AscRestackStackResult reports a restack.
type AscRestackStackResult struct {
	Items []AscRestackStackItem
	Onto  string
	// Applied is true when asc rewrote the stack. UpToDate is true when there was nothing to rewrite.
	Applied  bool
	UpToDate bool
	// Moved are the pull requests whose head moved; Failed those whose head asc rewrote but this call did not move.
	Moved  []*issues_model.PullRequest
	Failed []*issues_model.PullRequest
}

// Needs reports whether any item would be (or was) rewritten.
func (r *AscRestackStackResult) Needs() bool {
	for _, it := range r.Items {
		if it.State == ascbridge.RestackRewritten {
			return true
		}
	}
	return false
}

// Blocker returns the error that stops an apply: the first conflict, blocked or frozen item. nil means none.
func (r *AscRestackStackResult) Blocker() error {
	for _, it := range r.Items {
		switch it.State {
		case ascbridge.RestackConflict, ascbridge.RestackBlocked:
			return &ascbridge.ConflictError{Err: ascbridge.ErrStackConflict, Paths: it.Conflicts}
		case ascbridge.RestackSkippedFrozen:
			return ascbridge.ErrStackFrozen
		}
	}
	return nil
}

var ascStackBacked = ascsync.BackedBy

// AscMayRewrite reports whether doer may rewrite the head of pr: the update authorisation, or the right to merge.
func AscMayRewrite(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (bool, error) {
	if ok, err := AscMayRestoreVersion(ctx, pr, doer); err != nil || ok {
		return ok, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return false, err
	}
	perm, err := access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer)
	if err != nil {
		return false, err
	}
	return IsUserAllowedToMerge(ctx, pr, perm, doer)
}

// RestackAscStack restacks the stack of pr as doer. With DryRun it writes nothing.
func RestackAscStack(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest, opt AscRestackOptions) (*AscRestackStackResult, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return nil, ErrAscRestackNotAllowed
	}
	if pr == nil || pr.ID == 0 {
		return nil, ErrAscStackNotStacked
	}
	row, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrAscStackNotStacked
	}
	open, err := AscPullIsOpen(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, ErrAscRestackNotOpen
	}
	// The call is made on pr: doer has to be allowed to rewrite it before asc is asked anything.
	if ok, err := AscMayRewrite(ctx, doer, pr); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("%w: #%d", ErrAscRestackNotAllowed, pr.Index)
	}
	if backed, err := ascStackBacked(pr.BaseRepo); err != nil {
		return nil, err
	} else if !backed {
		return nil, ErrAscStackDisabled
	}
	ops, err := ascbridge.StackClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscStackDisabled, err)
	}
	repo := pr.BaseRepo.FullName()

	if !opt.DryRun {
		key, err := ascRestackKey(ctx, pr, row)
		if err != nil {
			return nil, err
		}
		unlock, err := lockAscRestack(ctx, key)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}

	preview, err := ops.RestackStack(ctx, ascbridge.RestackStackRequest{Repo: repo, Change: row.AscChange, DryRun: true})
	if err != nil {
		return nil, err
	}
	res := &AscRestackStackResult{Onto: preview.Onto}
	checked := map[string]*issues_model.PullRequest{}
	previewed := map[string]ascbridge.RestackItem{}
	for _, it := range preview.Items {
		item := AscRestackStackItem{Change: it.Change, State: it.State, OldHead: it.OldHead, NewVersion: it.NewVersion, Conflicts: it.Conflicts}
		previewed[it.Change] = it
		if it.State != ascbridge.RestackSkippedLanded {
			p, err := issues_model.GetOpenPullRequestByAscChange(ctx, pr.BaseRepoID, it.Change)
			switch {
			case err == nil:
				ok, aerr := AscMayRewrite(ctx, doer, p)
				if aerr != nil {
					return nil, aerr
				}
				if ok {
					item.PR = p
					checked[it.Change] = p
				} else if it.State != ascbridge.RestackUnchanged {
					return nil, fmt.Errorf("%w: #%d", ErrAscRestackNotAllowed, p.Index)
				}
			case issues_model.IsErrPullRequestNotExist(err):
				if it.State != ascbridge.RestackUnchanged {
					return nil, fmt.Errorf("%w: change %.12s", ErrAscRestackNoPull, it.Change)
				}
			default:
				return nil, err
			}
		}
		res.Items = append(res.Items, item)
	}
	if opt.DryRun {
		return res, nil
	}
	if err := res.Blocker(); err != nil {
		return res, err
	}
	if !res.Needs() {
		res.UpToDate = true
		return res, nil
	}

	// The apply is pinned to what was authorised: every change that may be rewritten, with the head seen.
	var expected []ascbridge.ExpectedHead
	for _, it := range res.Items {
		if it.PR == nil || (it.State != ascbridge.RestackRewritten && it.State != ascbridge.RestackUnchanged) {
			continue
		}
		id := previewed[it.Change].OldHeadID
		if id == "" {
			if it.State == ascbridge.RestackRewritten {
				return res, fmt.Errorf("%w: change %.12s", ErrAscRestackNoPin, it.Change)
			}
			continue
		}
		expected = append(expected, ascbridge.ExpectedHead{Change: it.Change, OldHeadID: id})
	}
	applied, err := ops.RestackStack(ctx, ascbridge.RestackStackRequest{Repo: repo, Change: row.AscChange, ExpectedHeads: expected})
	if err != nil {
		return res, err
	}
	res.Applied = applied.Applied
	res.Onto = applied.Onto
	res.Items = res.Items[:0]
	var errs []error
	for _, it := range applied.Items {
		item := AscRestackStackItem{
			PR: checked[it.Change], Change: it.Change, State: it.State, OldHead: it.OldHead, NewHead: it.NewHead,
			NewVersion: it.NewVersion, Conflicts: it.Conflicts,
		}
		res.Items = append(res.Items, item)
	}
	// asc has written. Before any head is moved here, every change it rewrote must be one the authorised preview
	// listed, with the head the preview showed. If not, fail loudly and move nothing.
	if err := verifyAscRestackApplied(previewed, checked, applied.Items); err != nil {
		log.Error("asc audit: restack stack: repository %d, change %s: user %d: %v", pr.BaseRepoID, row.AscChange, doer.ID, err)
		return res, fmt.Errorf("asc restacked the stack, but %w; no pull request head was moved", err)
	}
	for _, item := range res.Items {
		// Only a pull request that was checked above is touched, whatever asc rewrote.
		if item.PR == nil || item.NewHead == "" {
			continue
		}
		moved, merr := ascMoveRestacked(ctx, doer, item)
		switch {
		case merr != nil:
			errs = append(errs, fmt.Errorf("#%d: %w", item.PR.Index, merr))
			res.Failed = append(res.Failed, item.PR)
		case moved:
			res.Moved = append(res.Moved, item.PR)
		}
	}
	log.Info("asc audit: restack stack: repository %d, change %s: user %d, %d rewritten, %d pull requests moved",
		pr.BaseRepoID, row.AscChange, doer.ID, countState(res.Items, ascbridge.RestackRewritten), len(res.Moved))
	if len(errs) > 0 {
		return res, fmt.Errorf("asc restacked the stack but a pull request head could not be moved: %w", errors.Join(errs...))
	}
	return res, nil
}

// verifyAscRestackApplied checks the answer of the apply against the authorised preview. A change counts as
// rewritten when asc says so or when it carries a head other than the old one (an unchanged patch is still a
// new commit). Each rewritten change must have been in the preview, have a checked pull request, and have the
// old head the preview showed.
func verifyAscRestackApplied(previewed map[string]ascbridge.RestackItem, checked map[string]*issues_model.PullRequest, items []ascbridge.RestackItem) error {
	var bad []error
	for _, it := range items {
		if it.State != ascbridge.RestackRewritten && (it.NewHead == "" || it.NewHead == it.OldHead) {
			continue
		}
		p, ok := previewed[it.Change]
		switch {
		case !ok:
			bad = append(bad, fmt.Errorf("change %.12s was not in the preview", it.Change))
		case checked[it.Change] == nil:
			bad = append(bad, fmt.Errorf("change %.12s has no checked pull request", it.Change))
		case p.State != ascbridge.RestackRewritten && p.State != ascbridge.RestackUnchanged:
			bad = append(bad, fmt.Errorf("change %.12s was %s in the preview", it.Change, p.State))
		case p.OldHeadID != "" && it.OldHeadID != "" && p.OldHeadID != it.OldHeadID,
			p.OldHead != "" && it.OldHead != "" && p.OldHead != it.OldHead:
			bad = append(bad, fmt.Errorf("change %.12s had another head than the preview showed", it.Change))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%w: %w", ErrAscRestackNotPreviewed, errors.Join(bad...))
	}
	return nil
}

func countState(items []AscRestackStackItem, s ascbridge.RestackState) int {
	n := 0
	for _, it := range items {
		if it.State == s {
			n++
		}
	}
	return n
}

// ascMoveRestacked points refs/pull/<n>/head of item.PR at the head asc wrote and runs the push follow-up.
func ascMoveRestacked(ctx context.Context, doer *user_model.User, item AscRestackStackItem) (bool, error) {
	pr := item.PR
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return false, err
	}
	current, err := ascHeads.Head(ctx, pr)
	if err != nil {
		return false, fmt.Errorf("head of pull request %d: %w", pr.ID, err)
	}
	if current == item.NewHead {
		return false, nil
	}
	if item.OldHead != "" && current != item.OldHead {
		// The pull request was pushed to since asc read it; moving its head would drop that push.
		return false, fmt.Errorf("head of pull request %d is not the head asc restacked", pr.ID)
	}
	if err := ascHeads.Move(ctx, pr, item.NewHead); err != nil {
		return false, fmt.Errorf("move head of pull request %d: %w", pr.ID, err)
	}
	pr.HeadCommitID = item.NewHead
	if item.State == ascbridge.RestackUnchanged {
		// The patch is the same: the live approvals of exactly the old head stand for the new one. They move
		// before the follow-up, whose stale pass then finds them on the new head.
		if err := keepAscApprovals(ctx, pr, current, item.NewHead); err != nil {
			log.Error("asc restack: keeping approvals of pull request %d: %v", pr.ID, err)
		}
	}
	ascRestoreFollowUp(ctx, pr, doer, current, item.NewHead)
	return true, nil
}
