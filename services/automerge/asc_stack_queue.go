// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
	pull_service "forgejo.org/services/pull"
)

// The stack queue: ordered "merge when ready" over the asc land (SPEC section 12.6 and section 20.5 flow 5,
// PLAN unit C39).
//
// "Merge when ready" on pull request N enqueues 1..N of its stack. One worker per target branch walks the
// entries in enqueue order and always lands the largest ready prefix, through pull_service.MergeAscStack (one
// atomic asc land). Properties:
//   - One land in flight per (repository, branch), across processes: the worker holds a lease row. There is
//     no global lock, no speculative CI and no batching.
//   - Readiness is decided by MergeAscStack on every try (open, not a draft, branch protection, the doer may
//     merge, heads unchanged since the check). A pull request that is not ready stays queued; it does not
//     block a later one of another stack, and the largest prefix that is ready lands.
//   - Permission is checked again at every land for the user who enqueued the top entry. A user who lost
//     the right, or who was deactivated, has the entry cancelled; the queue never lands on a stale grant.
//   - A paused branch lands nothing. Only a repository admin or a site admin pauses or resumes it.
//   - With [asc_stack_queue] REVALIDATE = on_content_merge (the default, and the reading of any other
//     value) the land carries no_content_merge. When the server answers LAND_NEEDS_CONTENT_MERGE the worker
//     restacks the stack as new versions and the entries wait (blocked) until a head changes, because the
//     content that was approved is no longer the content that would land. REVALIDATE = off omits the flag.
//   - A crash between claiming and finishing leaves entries in "landing". The next run marks the ones whose
//     pull request is merged as merged and puts the others back to queued; the land is idempotent.
//   - Revalidation is real: with the flag on, the content that would land is the content that was reviewed or
//     the land does not happen. The restack (pull_service.RestackAscStack, asc Stacks.Restack) writes the
//     rebased heads as new versions; the readiness check of the next try then asks for the checks and the
//     approvals of those exact heads (an approval survives where the patch is unchanged).

// Entry states.
const (
	AscQueueQueued    = "queued"
	AscQueueLanding   = "landing"
	AscQueueMerged    = "merged"
	AscQueueBlocked   = "blocked" // waits for a new head (restacked or pushed)
	AscQueueCancelled = "cancelled"
)

// Values of [asc_stack_queue] REVALIDATE.
const (
	AscRevalidateOnContentMerge = "on_content_merge"
	AscRevalidateOff            = "off"
)

// Errors of the queue. The first group is the caller's mistake.
var (
	ErrAscQueueDisabled     = errors.New("the repository is not stored in asc")
	ErrAscQueueNotAllowed   = errors.New("not allowed to merge a pull request of the stack")
	ErrAscQueueNotQueueable = errors.New("a pull request of the stack cannot be queued")
	ErrAscQueuePaused       = errors.New("the merge queue of the branch is paused")
	ErrAscQueueBusy         = errors.New("a land is in flight on the branch")
	ErrAscQueueNoPermission = errors.New("only an administrator may do this")

	// ErrAscLandNeedsContentMerge is what a lander returns when asc answered LAND_NEEDS_CONTENT_MERGE.
	ErrAscLandNeedsContentMerge = pull_service.ErrAscLandNeedsContentMerge
)

// AscStackQueueEntry is one queued pull request.
type AscStackQueueEntry struct {
	ID          int64  `xorm:"pk autoincr"`
	RepoID      int64  `xorm:"INDEX(repo_branch) NOT NULL"`
	Branch      string `xorm:"VARCHAR(255) INDEX(repo_branch) NOT NULL"`
	PullID      int64  `xorm:"UNIQUE NOT NULL"`
	AscChange   string `xorm:"VARCHAR(32) NOT NULL"`
	DoerID      int64  `xorm:"NOT NULL"`
	State       string `xorm:"VARCHAR(16) INDEX NOT NULL"`
	Reason      string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	BlockedHead string `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedUnix int64  `xorm:"created NOT NULL"`
	UpdatedUnix int64  `xorm:"updated NOT NULL"`
}

// TableName gives the table its name.
func (*AscStackQueueEntry) TableName() string { return "asc_stack_queue" }

// AscStackQueueBranch holds the pause flag and the worker lease of one (repository, branch).
type AscStackQueueBranch struct {
	ID         int64  `xorm:"pk autoincr"`
	RepoID     int64  `xorm:"UNIQUE(repo_branch) NOT NULL"`
	Branch     string `xorm:"VARCHAR(255) UNIQUE(repo_branch) NOT NULL"`
	Paused     bool   `xorm:"NOT NULL DEFAULT false"`
	PausedByID int64  `xorm:"NOT NULL DEFAULT 0"`
	LockUntil  int64  `xorm:"NOT NULL DEFAULT 0"`
}

// TableName gives the table its name.
func (*AscStackQueueBranch) TableName() string { return "asc_stack_queue_branch" }

func init() {
	db.RegisterModel(new(AscStackQueueEntry))
	db.RegisterModel(new(AscStackQueueBranch))
}

// ---------------------------------------------------------------------------------- seams

// AscQueueLander is the asc side of the queue: the land of a prefix and the restack of a stack.
type AscQueueLander interface {
	// Land lands bottom..pr atomically. opt.NoContentMerge sets UpdateRefsRequest.no_content_merge, and opt.Auto
	// says the merged event is the automatic one. A land that would need a content merge returns an error for
	// which isAscNeedsContentMerge is true.
	Land(ctx context.Context, doer *user_model.User, perm access_model.Permission, pr *issues_model.PullRequest, opt pull_service.AscMergeOptions) (*pull_service.AscStackMergeResult, error)
	// Restack rewrites the stack of pr as new versions on top of the current target.
	Restack(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) error
}

type ascPullLander struct{}

func (ascPullLander) Land(ctx context.Context, doer *user_model.User, perm access_model.Permission, pr *issues_model.PullRequest, opt pull_service.AscMergeOptions) (*pull_service.AscStackMergeResult, error) {
	return pull_service.MergeAscStackWith(ctx, doer, perm, pr, opt)
}

// Restack rewrites the whole stack of pr onto the target through asc Stacks.Restack, as doer. A stack that
// needs nothing is an error here: the land asked for a restack, so a restack that changes nothing cannot
// make the next try succeed, and the entries must wait for a new head instead of looping.
func (ascPullLander) Restack(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) error {
	res, err := pull_service.RestackAscStack(ctx, doer, pr, pull_service.AscRestackOptions{})
	if err != nil {
		return err
	}
	if !res.Applied {
		return errors.New("the stack needs no restack")
	}
	return nil
}

var (
	ascQueueLander AscQueueLander = ascPullLander{}

	// ascQueueMayMerge decides whether doer may merge pr. Tests replace it.
	ascQueueMayMerge = pull_service.IsUserAllowedToMerge

	// ascQueueHead resolves the current head of a pull request. Tests replace it.
	ascQueueHead = func(ctx context.Context, pr *issues_model.PullRequest) (string, error) {
		if err := pr.LoadBaseRepo(ctx); err != nil {
			return "", err
		}
		return git.GetFullCommitID(ctx, pr.BaseRepo.RepoPath(), pr.GetGitRefName())
	}

	ascQueueNow = time.Now

	// ascQueueRevalidateSetting reads [asc_stack_queue] REVALIDATE. Tests replace it.
	ascQueueRevalidateSetting = func() string {
		if setting.CfgProvider == nil {
			return ""
		}
		return setting.CfgProvider.Section("asc_stack_queue").Key("REVALIDATE").String()
	}
)

// UseAscQueueLander sets the lander until the returned function is called.
func UseAscQueueLander(l AscQueueLander) (restore func()) {
	prev := ascQueueLander
	ascQueueLander = l
	return func() { ascQueueLander = prev }
}

// ascQueueNoContentMerge reports whether lands carry no_content_merge. Anything but "off" means on.
func ascQueueNoContentMerge() bool {
	return strings.TrimSpace(strings.ToLower(ascQueueRevalidateSetting())) != AscRevalidateOff
}

// AscQueueNoContentMerge reports whether lands carry no_content_merge: [asc_stack_queue] REVALIDATE is anything
// but "off".
func AscQueueNoContentMerge() bool { return ascQueueNoContentMerge() }

func isAscNeedsContentMerge(err error) bool {
	return err != nil && (errors.Is(err, ErrAscLandNeedsContentMerge) || strings.Contains(err.Error(), "LAND_NEEDS_CONTENT_MERGE"))
}

// ---------------------------------------------------------------------------------- branch row, pause, lease

const (
	ascQueueLease    = 10 * time.Minute
	ascQueueMaxLands = 64
)

func ascQueueBranchRow(ctx context.Context, repoID int64, branch string) (*AscStackQueueBranch, error) {
	e := db.GetEngine(ctx)
	row := &AscStackQueueBranch{}
	has, err := e.Where("repo_id = ? AND branch = ?", repoID, branch).Get(row)
	if err != nil {
		return nil, err
	}
	if has {
		return row, nil
	}
	row = &AscStackQueueBranch{RepoID: repoID, Branch: branch}
	if _, err := e.Insert(row); err != nil {
		// A concurrent insert won the unique key; read its row.
		row = &AscStackQueueBranch{}
		has, gerr := e.Where("repo_id = ? AND branch = ?", repoID, branch).Get(row)
		if gerr != nil || !has {
			return nil, err
		}
	}
	return row, nil
}

// AscQueuePaused reports whether the branch is paused.
func AscQueuePaused(ctx context.Context, repoID int64, branch string) (bool, error) {
	row := &AscStackQueueBranch{}
	has, err := db.GetEngine(ctx).Where("repo_id = ? AND branch = ?", repoID, branch).Get(row)
	return has && row.Paused, err
}

// mayAdminAscQueue: a site admin or an admin of the repository.
func mayAdminAscQueue(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (bool, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return false, nil
	}
	if doer.IsAdmin {
		return true, nil
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
	if err != nil {
		return false, err
	}
	return perm.IsAdmin(), nil
}

// AscQueueSetPaused pauses or resumes the queue of a branch. Only an administrator may.
func AscQueueSetPaused(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, branch string, paused bool) error {
	ok, err := mayAdminAscQueue(ctx, doer, repo)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAscQueueNoPermission
	}
	if _, err := ascQueueBranchRow(ctx, repo.ID, branch); err != nil {
		return err
	}
	by := int64(0)
	if paused {
		by = doer.ID
	}
	_, err = db.GetEngine(ctx).Where("repo_id = ? AND branch = ?", repo.ID, branch).Cols("paused", "paused_by_id").
		Update(&AscStackQueueBranch{Paused: paused, PausedByID: by})
	if err == nil {
		log.Info("asc audit: merge queue: repository %d, branch %q: user %d set paused=%v", repo.ID, branch, doer.ID, paused)
	}
	return err
}

// ascQueueLock takes the lease of the branch. ok is false when another worker holds it.
func ascQueueLock(ctx context.Context, repoID int64, branch string) (unlock func(), ok bool, err error) {
	if _, err := ascQueueBranchRow(ctx, repoID, branch); err != nil {
		return nil, false, err
	}
	now := ascQueueNow()
	res, err := db.GetEngine(ctx).Exec(
		"UPDATE `asc_stack_queue_branch` SET lock_until = ? WHERE repo_id = ? AND branch = ? AND lock_until < ?",
		now.Add(ascQueueLease).UnixNano(), repoID, branch, now.UnixNano())
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return nil, false, err
	}
	return func() {
		// Release even when the caller's context is done.
		if _, err := db.GetEngine(context.WithoutCancel(ctx)).Exec(
			"UPDATE `asc_stack_queue_branch` SET lock_until = 0 WHERE repo_id = ? AND branch = ?", repoID, branch); err != nil {
			log.Error("asc stack queue: releasing the lease of %d/%s: %v", repoID, branch, err)
		}
	}, true, nil
}

// ---------------------------------------------------------------------------------- enqueue and cancel

// AscQueueEnqueueResult says what an enqueue did.
type AscQueueEnqueueResult struct {
	Queued []*AscStackQueueEntry // entries of 1..N now queued, bottom first
}

// AscQueueEnqueue enables merge-when-ready for the prefix 1..pr. doer must be allowed to merge every pull
// request of the prefix. Pull requests that are already queued keep their place.
func AscQueueEnqueue(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscQueueEnqueueResult, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return nil, ErrAscQueueNotAllowed
	}
	items, err := pull_service.AscStackPrefix(ctx, pr)
	if err != nil {
		return nil, err
	}
	first := items[0].PR
	if err := first.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if ok, err := ascQueueBacked(first.BaseRepo); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrAscQueueDisabled
	}
	perm, err := access_model.GetUserRepoPermission(ctx, first.BaseRepo, doer)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		p := it.PR
		if err := p.LoadIssue(ctx); err != nil {
			return nil, err
		}
		switch {
		case p.HasMerged || p.Issue.IsClosed:
			return nil, fmt.Errorf("%w: #%d is closed or merged", ErrAscQueueNotQueueable, p.Index)
		case p.IsWorkInProgress(ctx):
			return nil, fmt.Errorf("%w: #%d is a draft", ErrAscQueueNotQueueable, p.Index)
		case p.BaseRepoID != first.BaseRepoID || p.BaseBranch != first.BaseBranch:
			return nil, fmt.Errorf("%w: #%d targets another branch", ErrAscQueueNotQueueable, p.Index)
		}
		ok, err := ascQueueMayMerge(ctx, p, perm, doer)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: #%d", ErrAscQueueNotAllowed, p.Index)
		}
	}

	res := &AscQueueEnqueueResult{}
	var fresh []int64
	err = db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		for _, it := range items {
			cur := &AscStackQueueEntry{}
			has, err := e.Where("pull_id = ?", it.PR.ID).Get(cur)
			if err != nil {
				return err
			}
			if has && (cur.State == AscQueueQueued || cur.State == AscQueueLanding || cur.State == AscQueueBlocked) {
				res.Queued = append(res.Queued, cur)
				continue
			}
			if has {
				// Merged or cancelled earlier: a new row, so the place is the new enqueue time.
				if _, err := e.ID(cur.ID).Delete(new(AscStackQueueEntry)); err != nil {
					return err
				}
			}
			ent := &AscStackQueueEntry{
				RepoID: first.BaseRepoID, Branch: first.BaseBranch, PullID: it.PR.ID, AscChange: it.Change,
				DoerID: doer.ID, State: AscQueueQueued,
			}
			if _, err := e.Insert(ent); err != nil {
				return err
			}
			res.Queued = append(res.Queued, ent)
			fresh = append(fresh, it.PR.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, id := range fresh {
		ascQueueNote(ctx, id, doer, true)
	}
	ascQueueMirrorState(ctx, AscQueueQueued, fresh)
	log.Info("asc audit: merge queue: repository %d, branch %q: user %d queued %d pull requests up to pull request %d",
		first.BaseRepoID, first.BaseBranch, doer.ID, len(res.Queued), pr.ID)
	AscQueueKick(first.BaseRepoID, first.BaseBranch)
	return res, nil
}

// ascQueueBacked reports whether the repository is stored in asc. Tests replace it.
var ascQueueBacked = func(repo *repo_model.Repository) (bool, error) {
	return ascsync.BackedBy(repo)
}

// UseAscQueueBacked replaces the test that a repository is stored in asc until the returned function is
// called. It is for tests of the callers of the queue.
func UseAscQueueBacked(f func(*repo_model.Repository) (bool, error)) (restore func()) {
	prev := ascQueueBacked
	ascQueueBacked = f
	return func() { ascQueueBacked = prev }
}

// AscQueueBackedBy reports whether the repository is stored in asc, so the queue applies to it.
func AscQueueBackedBy(repo *repo_model.Repository) (bool, error) { return ascQueueBacked(repo) }

// AscQueueCancelResult is what a cancel took out of the queue and what it left, so the answer names both.
type AscQueueCancelResult struct {
	// Cancelled are the entries that were taken out, in stack order.
	Cancelled []*AscStackQueueEntry
	// Skipped are queued entries of the stack that belong to someone else and stay queued: the ones below the
	// pull request, which queued their own prefix. A cancel never removes what the doer may not remove.
	Skipped []*AscStackQueueEntry
}

// AscQueueCancel removes the stack of pr from the queue; see AscQueueCancelEntries.
func AscQueueCancel(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) error {
	_, err := AscQueueCancelEntries(ctx, doer, pr)
	return err
}

// AscQueueCancelEntries removes pr from the queue together with the queued pull requests above it (they would
// land it) and answers with exactly what it removed. The entry of pr must be waiting (queued or blocked; a land
// in flight answers ErrAscQueueBusy). The user who enqueued an entry and an administrator may cancel it:
//   - the entry of pr: another user gets ErrAscQueueNotAllowed;
//   - an entry above pr that belongs to someone else: the cancel is refused with ErrAscQueueNotAllowed, because
//     leaving it would land pr anyway;
//   - an entry below pr that belongs to someone else: it stays queued and is returned in Skipped.
func AscQueueCancelEntries(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscQueueCancelResult, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return nil, ErrAscQueueNotAllowed
	}
	res := &AscQueueCancelResult{}
	ent := &AscStackQueueEntry{}
	has, err := db.GetEngine(ctx).Where("pull_id = ?", pr.ID).Get(ent)
	if err != nil || !has {
		return res, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	admin, err := mayAdminAscQueue(ctx, doer, pr.BaseRepo)
	if err != nil {
		return nil, err
	}
	if ent.DoerID != doer.ID && !admin {
		return nil, ErrAscQueueNotAllowed
	}
	switch ent.State {
	case AscQueueQueued, AscQueueBlocked:
	case AscQueueLanding:
		return nil, ErrAscQueueBusy
	default:
		return res, nil // cancelled, merged or failed already: nothing waits
	}
	err = db.WithTx(ctx, func(ctx context.Context) error {
		var err error
		res, err = ascQueueCancelPlan(ctx, doer, admin, ent, pr)
		if err != nil || len(res.Cancelled) == 0 {
			return err
		}
		ids := make([]int64, len(res.Cancelled))
		for i, c := range res.Cancelled {
			ids[i] = c.ID
		}
		if _, err := db.GetEngine(ctx).In("id", ids).Cols("state", "reason").
			Update(&AscStackQueueEntry{State: AscQueueCancelled, Reason: "cancelled by " + doer.Name}); err != nil {
			return err
		}
		log.Info("asc audit: merge queue: repository %d, branch %q: user %d removed %d entries (%d left to other users) for pull request %d",
			ent.RepoID, ent.Branch, doer.ID, len(ids), len(res.Skipped), pr.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, c := range res.Cancelled {
		ascQueueNote(ctx, c.PullID, doer, false)
		ascQueueTell(ctx, c, doer.ID)
	}
	return res, nil
}

// AscQueueCancelSet returns what AscQueueCancelEntries would do for doer on pr, in stack order, without writing:
// the entries it would cancel and the entries of others it would leave. The confirmation page lists exactly
// these. A cancel that is refused answers the same error.
func AscQueueCancelSet(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) (*AscQueueCancelResult, error) {
	res := &AscQueueCancelResult{}
	if doer == nil {
		return nil, ErrAscQueueNotAllowed
	}
	ent := &AscStackQueueEntry{}
	has, err := db.GetEngine(ctx).Where("pull_id = ?", pr.ID).Get(ent)
	if err != nil || !has {
		return res, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	admin, err := mayAdminAscQueue(ctx, doer, pr.BaseRepo)
	if err != nil {
		return nil, err
	}
	if ent.DoerID != doer.ID && !admin {
		return nil, ErrAscQueueNotAllowed
	}
	if ent.State != AscQueueQueued && ent.State != AscQueueBlocked {
		return res, nil
	}
	return ascQueueCancelPlan(ctx, doer, admin, ent, pr)
}

// ascQueueCancelPlan decides which waiting entries a cancel of pr by doer takes out and which it leaves.
func ascQueueCancelPlan(ctx context.Context, doer *user_model.User, admin bool, ent *AscStackQueueEntry, pr *issues_model.PullRequest) (*AscQueueCancelResult, error) {
	below, above, err := ascQueueCancelSet(ctx, ent, pr)
	if err != nil {
		return nil, err
	}
	res := &AscQueueCancelResult{}
	for _, c := range above {
		if c.DoerID != doer.ID && !admin {
			return nil, fmt.Errorf("%w: the entry of pull request %d above it was queued by someone else", ErrAscQueueNotAllowed, c.PullID)
		}
	}
	for _, c := range below {
		if c.DoerID == doer.ID || admin {
			res.Cancelled = append(res.Cancelled, c)
		} else {
			res.Skipped = append(res.Skipped, c)
		}
	}
	res.Cancelled = append(res.Cancelled, above...)
	slices.SortFunc(res.Cancelled, func(a, b *AscStackQueueEntry) int { return int(a.ID - b.ID) })
	return res, nil
}

// ascQueueCancelSet splits the waiting entries a cancel of pr concerns: below are the entries from the bottom of
// the stack up to and including pr (what "merge when ready" on pr queued), above are the entries of the pull
// requests above pr, which would land it.
func ascQueueCancelSet(ctx context.Context, ent *AscStackQueueEntry, pr *issues_model.PullRequest) (below, above []*AscStackQueueEntry, err error) {
	entries, err := ascQueueEntries(ctx, ent.RepoID, ent.Branch, AscQueueQueued, AscQueueBlocked)
	if err != nil {
		return nil, nil, err
	}
	inBelow := map[int64]bool{pr.ID: true}
	if prefix, err := pull_service.AscStackPrefix(ctx, pr); err == nil {
		for _, it := range prefix {
			inBelow[it.PR.ID] = true
		}
	}
	for _, c := range entries {
		if inBelow[c.PullID] {
			below = append(below, c)
			continue
		}
		cp, err := issues_model.GetPullRequestByID(ctx, c.PullID)
		if err != nil {
			continue
		}
		prefix, err := pull_service.AscStackPrefix(ctx, cp)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(prefix, func(it *pull_service.AscStackItem) bool { return it.PR.ID == pr.ID }) {
			above = append(above, c)
		}
	}
	return below, above, nil
}

// AscQueueList returns the entries of a branch in enqueue order.
func AscQueueList(ctx context.Context, repoID int64, branch string) ([]*AscStackQueueEntry, error) {
	return ascQueueEntries(ctx, repoID, branch)
}

func ascQueueEntries(ctx context.Context, repoID int64, branch string, states ...string) ([]*AscStackQueueEntry, error) {
	var out []*AscStackQueueEntry
	sess := db.GetEngine(ctx).Where("repo_id = ? AND branch = ?", repoID, branch)
	if len(states) > 0 {
		sess = sess.In("state", states)
	}
	return out, sess.OrderBy("id").Find(&out)
}

// ---------------------------------------------------------------------------------- the worker

func ascQueueSetState(ctx context.Context, state, reason, blockedHead string, pullIDs []int64, onlyFrom ...string) error {
	if len(pullIDs) == 0 {
		return nil
	}
	if len(reason) > 250 {
		reason = reason[:250]
	}
	var stopped []*AscStackQueueEntry
	if state == AscQueueCancelled || state == AscQueueBlocked {
		var err error
		if stopped, err = ascQueueEntriesToChange(ctx, state, pullIDs, onlyFrom); err != nil {
			return err
		}
	}
	sess := db.GetEngine(ctx).In("pull_id", pullIDs)
	if len(onlyFrom) > 0 {
		sess = sess.In("state", onlyFrom)
	}
	if _, err := sess.Cols("state", "reason", "blocked_head").Update(&AscStackQueueEntry{State: state, Reason: reason, BlockedHead: blockedHead}); err != nil {
		return err
	}
	ascQueueAnnounceStopped(ctx, stopped, state)
	ascQueueMirrorState(ctx, state, pullIDs)
	return nil
}

// ascQueueRecover finishes what a crashed worker left in "landing".
func ascQueueRecover(ctx context.Context, repoID int64, branch string) error {
	stale, err := ascQueueEntries(ctx, repoID, branch, AscQueueLanding)
	if err != nil {
		return err
	}
	for _, s := range stale {
		next := AscQueueQueued
		if p, err := issues_model.GetPullRequestByID(ctx, s.PullID); err == nil && p.HasMerged {
			next = AscQueueMerged
		}
		if err := ascQueueSetState(ctx, next, "", "", []int64{s.PullID}, AscQueueLanding); err != nil {
			return err
		}
	}
	return nil
}

// ascQueueUnblock puts a blocked entry back when its pull request has a new head.
func ascQueueUnblock(ctx context.Context, repoID int64, branch string) error {
	blocked, err := ascQueueEntries(ctx, repoID, branch, AscQueueBlocked)
	if err != nil {
		return err
	}
	for _, b := range blocked {
		p, err := issues_model.GetPullRequestByID(ctx, b.PullID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				if err := ascQueueSetState(ctx, AscQueueCancelled, "pull request is gone", "", []int64{b.PullID}); err != nil {
					return err
				}
				continue
			}
			return err
		}
		head, err := ascQueueHead(ctx, p)
		if err != nil {
			return err
		}
		if head != b.BlockedHead {
			if err := ascQueueSetState(ctx, AscQueueQueued, "", "", []int64{b.PullID}, AscQueueBlocked); err != nil {
				return err
			}
		}
	}
	return nil
}

type ascCandidate struct {
	entry  *AscStackQueueEntry
	pr     *issues_model.PullRequest
	prefix []*pull_service.AscStackItem
}

func (c *ascCandidate) pullIDs() []int64 {
	ids := make([]int64, len(c.prefix))
	for i, it := range c.prefix {
		ids[i] = it.PR.ID
	}
	return ids
}

type ascStepResult int

const (
	ascStepIdle     ascStepResult = iota // nothing changed
	ascStepProgress                      // something landed, was blocked or was cancelled
)

// ascQueueStep tries to land the largest ready prefix for the oldest entry that has one.
func ascQueueStep(ctx context.Context, repoID int64, branch string) (ascStepResult, int, error) {
	entries, err := ascQueueEntries(ctx, repoID, branch, AscQueueQueued)
	if err != nil {
		return ascStepIdle, 0, err
	}
	cands := make([]*ascCandidate, 0, len(entries))
	for _, ent := range entries {
		pr, err := issues_model.GetPullRequestByID(ctx, ent.PullID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				return ascStepProgress, 0, ascQueueSetState(ctx, AscQueueCancelled, "pull request is gone", "", []int64{ent.PullID})
			}
			return ascStepIdle, 0, err
		}
		prefix, err := pull_service.AscStackPrefix(ctx, pr)
		if err != nil {
			if errors.Is(err, pull_service.ErrAscStackNotStacked) {
				return ascStepProgress, 0, ascQueueSetState(ctx, AscQueueCancelled, "no longer part of a stack", "", []int64{ent.PullID})
			}
			// A cycle or a database error: not a candidate now.
			log.Warn("asc stack queue: prefix of pull request %d: %v", pr.ID, err)
			continue
		}
		cands = append(cands, &ascCandidate{entry: ent, pr: pr, prefix: prefix})
	}

	for _, oldest := range cands {
		// Candidates whose prefix holds the oldest entry, largest first; ties by enqueue order.
		var tops []*ascCandidate
		for _, c := range cands {
			if slices.ContainsFunc(c.prefix, func(it *pull_service.AscStackItem) bool { return it.PR.ID == oldest.pr.ID }) {
				tops = append(tops, c)
			}
		}
		slices.SortStableFunc(tops, func(a, b *ascCandidate) int { return len(b.prefix) - len(a.prefix) })
		for _, top := range tops {
			res, n, err := ascQueueTryLand(ctx, top)
			if err != nil {
				return ascStepIdle, 0, err
			}
			if res == ascStepProgress {
				return res, n, nil
			}
		}
	}
	return ascStepIdle, 0, nil
}

func ascQueueTryLand(ctx context.Context, c *ascCandidate) (ascStepResult, int, error) {
	doer, err := user_model.GetUserByID(ctx, c.entry.DoerID)
	if err != nil || !doer.IsActive || doer.ProhibitLogin {
		if err != nil && !user_model.IsErrUserNotExist(err) {
			return ascStepIdle, 0, err
		}
		return ascStepProgress, 0, ascQueueSetState(ctx, AscQueueCancelled, "the user who queued it is unavailable", "", []int64{c.entry.PullID})
	}
	if err := c.pr.LoadBaseRepo(ctx); err != nil {
		return ascStepIdle, 0, err
	}
	perm, err := access_model.GetUserRepoPermission(ctx, c.pr.BaseRepo, doer)
	if err != nil {
		return ascStepIdle, 0, err
	}

	ids := c.pullIDs()
	if err := ascQueueSetState(ctx, AscQueueLanding, "", "", ids, AscQueueQueued); err != nil {
		return ascStepIdle, 0, err
	}
	back := func(reason string) error {
		return ascQueueSetState(ctx, AscQueueQueued, reason, "", ids, AscQueueLanding)
	}

	res, err := ascQueueLander.Land(ctx, doer, perm, c.pr, pull_service.AscMergeOptions{NoContentMerge: ascQueueNoContentMerge(), Auto: true})
	switch {
	case err == nil:
	case errors.Is(err, pull_service.ErrAscStackNotAllowed):
		return ascStepProgress, 0, ascQueueSetState(ctx, AscQueueCancelled, "not allowed to merge: "+err.Error(), "", ids, AscQueueLanding)
	case errors.Is(err, pull_service.ErrAscStackNotStacked), errors.Is(err, pull_service.ErrAscStackDisabled):
		return ascStepProgress, 0, ascQueueSetState(ctx, AscQueueCancelled, err.Error(), "", ids, AscQueueLanding)
	case errors.Is(err, pull_service.ErrAscStackNotMergable), errors.Is(err, pull_service.ErrAscStackHeadMoved):
		// Not ready: stays queued, a smaller prefix may be.
		return ascStepIdle, 0, back(err.Error())
	case isAscNeedsContentMerge(err):
		return ascStepProgress, 0, ascQueueRevalidate(ctx, doer, c, ids)
	default:
		// A transient or unknown failure: put the entries back and stop the run.
		if berr := back("land failed"); berr != nil {
			log.Error("asc stack queue: %v", berr)
		}
		return ascStepIdle, 0, err
	}

	merged := make([]int64, 0, len(res.Merged))
	for _, p := range res.Merged {
		merged = append(merged, p.ID)
	}
	if err := ascQueueSetState(ctx, AscQueueMerged, "", "", merged); err != nil {
		return ascStepProgress, len(merged), err
	}
	return ascStepProgress, len(merged), nil
}

// ascQueueRevalidate handles LAND_NEEDS_CONTENT_MERGE: the content that was approved is not the content that
// would land. The stack is restacked as new versions and the entries go back to queued, where the branch
// protections hold them until the checks of the new versions pass. When the restack fails the entries are
// blocked on the heads they have now, and only a new head (the author restacks and pushes) frees them.
func ascQueueRevalidate(ctx context.Context, doer *user_model.User, c *ascCandidate, ids []int64) error {
	if restackErr := ascQueueLander.Restack(ctx, doer, c.pr); restackErr == nil {
		return ascQueueSetState(ctx, AscQueueQueued, "restacked: waiting for checks", "", ids, AscQueueLanding)
	} else {
		log.Warn("asc stack queue: restack of pull request %d: %v", c.pr.ID, restackErr)
	}
	for _, it := range c.prefix {
		head, err := ascQueueHead(ctx, it.PR)
		if err != nil {
			return err
		}
		if err := ascQueueSetState(ctx, AscQueueBlocked, "the land needs a content merge: restack the stack and push again", head,
			[]int64{it.PR.ID}, AscQueueLanding); err != nil {
			return err
		}
	}
	return nil
}

// RunAscStackQueue runs the worker of one branch until nothing more can land, and returns the number of pull
// requests that landed. It returns at once when another worker holds the branch or the branch is paused.
func RunAscStackQueue(ctx context.Context, repoID int64, branch string) (int, error) {
	unlock, ok, err := ascQueueLock(ctx, repoID, branch)
	if err != nil || !ok {
		return 0, err
	}
	defer unlock()
	return runAscStackQueueLocked(ctx, repoID, branch)
}

func runAscStackQueueLocked(ctx context.Context, repoID int64, branch string) (int, error) {
	if err := ascQueueRecover(ctx, repoID, branch); err != nil {
		return 0, err
	}
	landed := 0
	for range ascQueueMaxLands {
		if err := ctx.Err(); err != nil {
			return landed, err
		}
		if paused, err := AscQueuePaused(ctx, repoID, branch); err != nil || paused {
			return landed, err
		}
		if err := ascQueueUnblock(ctx, repoID, branch); err != nil {
			return landed, err
		}
		res, n, err := ascQueueStep(ctx, repoID, branch)
		landed += n
		if err != nil || res == ascStepIdle {
			return landed, err
		}
	}
	return landed, nil
}

// AscQueueLandNow lands bottom..pr at once when every pull request of the prefix is mergeable, under the
// branch lease so the one-land-in-flight rule holds. It refuses on a paused branch or a busy one. It returns
// the pull requests that landed.
func AscQueueLandNow(ctx context.Context, doer *user_model.User, pr *issues_model.PullRequest) ([]*issues_model.PullRequest, error) {
	if doer == nil || !doer.IsActive || doer.ProhibitLogin {
		return nil, ErrAscQueueNotAllowed
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if ok, err := ascQueueBacked(pr.BaseRepo); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrAscQueueDisabled
	}
	perm, err := access_model.GetUserRepoPermission(ctx, pr.BaseRepo, doer)
	if err != nil {
		return nil, err
	}
	// The doer must be allowed to merge every pull request of the prefix before the lander is asked, whatever the
	// lander checks itself.
	items, err := pull_service.AscStackPrefix(ctx, pr)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if ok, err := ascQueueMayMerge(ctx, it.PR, perm, doer); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("%w: #%d", ErrAscQueueNotAllowed, it.PR.Index)
		}
	}
	unlock, ok, err := ascQueueLock(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAscQueueBusy
	}
	defer unlock()
	if paused, err := AscQueuePaused(ctx, pr.BaseRepoID, pr.BaseBranch); err != nil {
		return nil, err
	} else if paused {
		return nil, ErrAscQueuePaused
	}
	res, err := ascQueueLander.Land(ctx, doer, perm, pr, pull_service.AscMergeOptions{NoContentMerge: ascQueueNoContentMerge()})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(res.Merged))
	for _, p := range res.Merged {
		ids = append(ids, p.ID)
	}
	log.Info("asc audit: merge queue: repository %d, branch %q: user %d landed %d pull requests up to pull request %d now",
		pr.BaseRepoID, pr.BaseBranch, doer.ID, len(ids), pr.ID)
	if err := ascQueueSetState(ctx, AscQueueMerged, "", "", ids); err != nil {
		return res.Merged, err
	}
	return res.Merged, nil
}

// ---------------------------------------------------------------------------------- scheduling

var (
	ascQueueKickMu  sync.Mutex
	ascQueueKicked  = map[string]bool{}
	ascQueueStarted sync.Once
)

// ascQueueKicker starts a run for a branch. Tests replace it.
var ascQueueKicker = startAscQueueRun

// UseAscQueueKicker replaces the function that schedules a run after an enqueue, until restore is called.
func UseAscQueueKicker(k func(repoID int64, branch string)) (restore func()) {
	prev := ascQueueKicker
	ascQueueKicker = k
	return func() { ascQueueKicker = prev }
}

// AscQueueKick asks for a run of the branch's worker soon. It never blocks on the run.
func AscQueueKick(repoID int64, branch string) { ascQueueKicker(repoID, branch) }

func startAscQueueRun(repoID int64, branch string) {
	ascQueueStarted.Do(func() { go startAscQueuePoller() })
	key := fmt.Sprintf("%d\x00%s", repoID, branch)
	ascQueueKickMu.Lock()
	already := ascQueueKicked[key]
	ascQueueKicked[key] = true
	ascQueueKickMu.Unlock()
	if already {
		return
	}
	go func() {
		defer func() {
			ascQueueKickMu.Lock()
			delete(ascQueueKicked, key)
			ascQueueKickMu.Unlock()
		}()
		ctx := graceful.GetManager().ShutdownContext()
		if _, err := RunAscStackQueue(ctx, repoID, branch); err != nil {
			log.Error("asc stack queue: run of %d/%s: %v", repoID, branch, err)
		}
	}()
}

// InitAscStackQueue starts the poller that re-runs every branch with queued or blocked entries, so a check
// that turned green or a new push moves the queue. automerge.Init calls it; the first enqueue starts it too.
func InitAscStackQueue() { ascQueueStarted.Do(func() { go startAscQueuePoller() }) }

const ascQueuePollEvery = 20 * time.Second

func startAscQueuePoller() {
	ctx := graceful.GetManager().ShutdownContext()
	t := time.NewTicker(ascQueuePollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := RunAllAscStackQueues(ctx); err != nil {
				log.Error("asc stack queue: poll: %v", err)
			}
		}
	}
}

// RunAllAscStackQueues runs the worker of every branch that has a queued or blocked entry.
func RunAllAscStackQueues(ctx context.Context) error {
	var keys []AscStackQueueEntry
	if err := db.GetEngine(ctx).Distinct("repo_id", "branch").In("state", AscQueueQueued, AscQueueBlocked, AscQueueLanding).Find(&keys); err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := RunAscStackQueue(ctx, k.RepoID, k.Branch); err != nil {
			log.Error("asc stack queue: run of %d/%s: %v", k.RepoID, k.Branch, err)
		}
	}
	return nil
}
