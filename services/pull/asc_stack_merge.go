// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascsync"
)

// "Merge stack (N)" and the post-merge restack (SPEC section 20.5 flows 3 and 4, PLAN unit C38).
//
// MergeAscStack lands the prefix bottom..N of a stack in one atomic asc call (UpdateRefs LAND) and then asks asc to
// restack the open children with RestackChanges. Properties:
//   - Every pull request of the prefix is checked on its own: open, not a draft, same base repository and base
//     branch, the doer may merge it, and branch protection (approvals and status checks) passes. One failure
//     refuses the whole merge before asc is called. A database error refuses too (fail closed).
//   - The heads that were checked are the heads that land. The backend lands head(N) only when the heads form
//     a chain (each is an ancestor of the next), so a lower pull request cannot land content nobody approved.
//   - The land is all or nothing: asc lands the whole prefix atomically. Pull requests are marked merged only
//     after asc answered, with the changeset asc wrote for each.
//   - The restack uses the doer as committer and one timestamp per call, so a retry of the same request yields
//     the same changeset ids. A conflict leaves the child as it was; it then shows "needs restack" (C36
//     derives that from its landed parent) and the author runs `asc sync && asc submit`.
//   - Approvals survive a restack only for a change whose patch is unchanged (patch_unchanged). Every other
//     rewritten child keeps the ordinary stale-review behaviour.
//
// TODO(C22): RestackChanges is coded to its SPEC contract; AscStackBackend is the seam a real client fills.

// Errors of the stack merge. The first group is the caller's mistake and maps to 4xx.
var (
	ErrAscStackNotStacked  = errors.New("pull request is not part of a stack")
	ErrAscStackNotMergable = errors.New("a pull request of the stack cannot be merged")
	ErrAscStackNotAllowed  = errors.New("not allowed to merge a pull request of the stack")
	ErrAscStackHeadMoved   = errors.New("a pull request head moved while the stack merge was prepared")
	ErrAscStackDisabled    = errors.New("the repository is not stored in asc")
)

// AscStackItem is one pull request of the prefix, bottom first.
type AscStackItem struct {
	PR     *issues_model.PullRequest
	Change string // 32 letters k-z
	Head   string // git id of the head that was checked
}

// AscLandRequest asks the backend to land Items[len-1].Head onto Target. Heads are bottom to top.
type AscLandRequest struct {
	Repo   string // owner/name
	Target string // full ref name, refs/heads/<base>
	Heads  []string
}

// AscLandResult says what asc landed. Landed[i] is the git id the change of Heads[i] has on the target.
type AscLandResult struct {
	Landed []string
	Tip    string
}

// AscRestackItem is one open child to rewrite.
type AscRestackItem struct {
	Change     string
	UpdateRef  string // refs/pull/<index>/head
	ExpectHead string // git id the ref must still have
}

// AscRestackRequest asks asc to rebase the items, bottom to top, onto Onto.
type AscRestackRequest struct {
	Repo           string
	Onto           string
	Items          []AscRestackItem
	CommitterName  string
	CommitterEmail string
	CommitterTime  int64
}

// AscRestackOutcome is the result for one item.
type AscRestackOutcome struct {
	Change         string
	NewHead        string // empty when the item was not rewritten
	Conflict       bool
	PatchUnchanged bool
}

// AscStackBackend is the asc side of the stack merge.
type AscStackBackend interface {
	Land(ctx context.Context, req AscLandRequest) (*AscLandResult, error)
	Restack(ctx context.Context, req AscRestackRequest) ([]AscRestackOutcome, error)
}

var (
	ascStackBackend AscStackBackend

	// ascStackHead resolves the current head of a pull request. Tests replace it.
	ascStackHead = func(ctx context.Context, pr *issues_model.PullRequest) (string, error) {
		if err := pr.LoadBaseRepo(ctx); err != nil {
			return "", err
		}
		return git.GetFullCommitID(ctx, pr.BaseRepo.RepoPath(), pr.GetGitRefName())
	}
	ascStackNow = time.Now
)

// UseAscStackBackend sets the backend until the returned function is called. The router sets the real one
// at start; tests set a fake.
func UseAscStackBackend(b AscStackBackend) (restore func()) {
	prev := ascStackBackend
	ascStackBackend = b
	return func() { ascStackBackend = prev }
}

// AscStackPrefix returns the unmerged pull requests from the bottom of pr's stack up to and including pr,
// bottom first. It fails closed on a cycle, a missing link, a closed or foreign pull request.
func AscStackPrefix(ctx context.Context, pr *issues_model.PullRequest) ([]*AscStackItem, error) {
	if pr == nil || pr.ID == 0 {
		return nil, ErrAscStackNotStacked
	}
	seen := map[int64]bool{}
	var rev []*AscStackItem
	cur := pr
	for range ascStackMaxDepth {
		if seen[cur.ID] {
			return nil, fmt.Errorf("%w: cycle", ErrAscStackNotMergable)
		}
		seen[cur.ID] = true
		row, err := issues_model.GetPullRequestAscStack(ctx, cur.ID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, ErrAscStackNotStacked
		}
		rev = append(rev, &AscStackItem{PR: cur, Change: row.AscChange})
		if row.StackParentID == 0 {
			break
		}
		parent, err := issues_model.GetPullRequestByID(ctx, row.StackParentID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				break
			}
			return nil, err
		}
		if parent.HasMerged {
			break
		}
		cur = parent
		if len(rev) == ascStackMaxDepth {
			return nil, fmt.Errorf("%w: stack too deep", ErrAscStackNotMergable)
		}
	}
	slices.Reverse(rev)
	return rev, nil
}

func checkAscStackItem(ctx context.Context, doer *user_model.User, perm access_model.Permission, first *issues_model.PullRequest, it *AscStackItem) error {
	pr := it.PR
	if err := pr.LoadIssue(ctx); err != nil {
		return err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return err
	}
	switch {
	case pr.HasMerged:
		return fmt.Errorf("%w: #%d is merged already", ErrAscStackNotMergable, pr.Index)
	case pr.Issue.IsClosed:
		return fmt.Errorf("%w: #%d is closed", ErrAscStackNotMergable, pr.Index)
	case pr.IsWorkInProgress(ctx):
		return fmt.Errorf("%w: #%d is a draft", ErrAscStackNotMergable, pr.Index)
	case pr.BaseRepoID != first.BaseRepoID || pr.BaseBranch != first.BaseBranch:
		return fmt.Errorf("%w: #%d targets another branch", ErrAscStackNotMergable, pr.Index)
	}
	ok, err := IsUserAllowedToMerge(ctx, pr, perm, doer)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: #%d", ErrAscStackNotAllowed, pr.Index)
	}
	if _, err := CheckPullBranchProtections(ctx, pr, false); err != nil {
		return fmt.Errorf("%w: #%d: %w", ErrAscStackNotMergable, pr.Index, err)
	}
	head, err := ascStackHead(ctx, pr)
	if err != nil {
		return err
	}
	if pr.HeadCommitID != "" && pr.HeadCommitID != head {
		return fmt.Errorf("%w: #%d", ErrAscStackHeadMoved, pr.Index)
	}
	it.Head = head
	return nil
}

// AscStackMergeResult reports a stack merge.
type AscStackMergeResult struct {
	Merged   []*issues_model.PullRequest // bottom first
	Restack  []AscRestackOutcome
	Restacks error // set when the land succeeded and the restack call failed; the merge stands
}

// MergeAscStack merges the prefix bottom..pr of a stack and restacks the open children. perm is the doer's
// permission on the base repository.
func MergeAscStack(ctx context.Context, doer *user_model.User, perm access_model.Permission, pr *issues_model.PullRequest) (*AscStackMergeResult, error) {
	if doer == nil {
		return nil, ErrAscStackNotAllowed
	}
	backend := ascStackBackend
	if backend == nil {
		return nil, fmt.Errorf("%w: no backend", ErrAscStackDisabled)
	}
	items, err := AscStackPrefix(ctx, pr)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if err := checkAscStackItem(ctx, doer, perm, items[0].PR, it); err != nil {
			return nil, err
		}
	}
	repo := items[0].PR.BaseRepo
	name := repo.FullName()

	heads := make([]string, len(items))
	for i, it := range items {
		heads[i] = it.Head
	}
	landed, err := backend.Land(ctx, AscLandRequest{Repo: name, Target: git.BranchPrefix + items[0].PR.BaseBranch, Heads: heads})
	if err != nil {
		return nil, err
	}
	if len(landed.Landed) != len(items) {
		return nil, fmt.Errorf("asc answered %d landed changesets for %d pull requests", len(landed.Landed), len(items))
	}

	res := &AscStackMergeResult{}
	merged := map[int64]bool{}
	for i, it := range items {
		it.PR.Merger = doer
		it.PR.MergerID = doer.ID
		it.PR.MergedCommitID = landed.Landed[i]
		it.PR.MergedUnix = timeutil.TimeStampNow()
		it.PR.Status = issues_model.PullRequestStatusMergeable
		if _, err := it.PR.SetMerged(ctx); err != nil {
			// asc has landed the stack; a retry finds the pull request merged or refused. Say so.
			return res, fmt.Errorf("the stack landed in asc but #%d could not be marked merged: %w", it.PR.Index, err)
		}
		merged[it.PR.ID] = true
		res.Merged = append(res.Merged, it.PR)
	}

	children, err := openAscStackChildren(ctx, items, merged)
	if err != nil {
		res.Restacks = err
		return res, nil
	}
	if len(children) == 0 {
		return res, nil
	}
	req := AscRestackRequest{
		Repo: name, Onto: landed.Tip,
		CommitterName: doer.DisplayName(), CommitterEmail: doer.GetEmail(), CommitterTime: ascStackNow().Unix(),
	}
	byChange := map[string]*issues_model.PullRequest{}
	for _, c := range children {
		req.Items = append(req.Items, AscRestackItem{Change: c.change, UpdateRef: c.pr.GetGitRefName(), ExpectHead: c.head})
		byChange[c.change] = c.pr
	}
	out, err := backend.Restack(ctx, req)
	if err != nil {
		res.Restacks = err
		return res, nil
	}
	res.Restack = out
	for _, o := range out {
		child := byChange[o.Change]
		if child == nil || o.NewHead == "" || !o.PatchUnchanged {
			continue
		}
		if err := keepAscApprovals(ctx, child, o.NewHead); err != nil {
			log.Error("asc stack merge: keeping approvals of pull request %d: %v", child.ID, err)
		}
	}
	return res, nil
}

type ascStackChild struct {
	pr     *issues_model.PullRequest
	change string
	head   string
}

// openAscStackChildren lists the open pull requests stacked on a merged one that are not merged themselves,
// breadth first from the bottom, so a parent is restacked before its child.
func openAscStackChildren(ctx context.Context, items []*AscStackItem, merged map[int64]bool) ([]ascStackChild, error) {
	var out []ascStackChild
	seen := map[int64]bool{}
	queue := make([]int64, 0, len(items))
	for _, it := range items {
		queue = append(queue, it.PR.ID)
	}
	for len(queue) > 0 && len(out) < 1<<10 {
		id := queue[0]
		queue = queue[1:]
		kids, err := issues_model.GetAscStackChildren(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			if merged[k.PullID] || seen[k.PullID] {
				continue
			}
			seen[k.PullID] = true
			kid, err := issues_model.GetPullRequestByID(ctx, k.PullID)
			if err != nil {
				if issues_model.IsErrPullRequestNotExist(err) {
					continue
				}
				return nil, err
			}
			if kid.HasMerged {
				continue
			}
			if err := kid.LoadIssue(ctx); err != nil {
				return nil, err
			}
			if kid.Issue.IsClosed {
				continue
			}
			head, err := ascStackHead(ctx, kid)
			if err != nil {
				return nil, err
			}
			out = append(out, ascStackChild{pr: kid, change: k.AscChange, head: head})
			queue = append(queue, kid.ID)
		}
	}
	return out, nil
}

// keepAscApprovals moves the live approvals of pr to its new head, so the stale-review pass of the push does not
// drop them. Only approvals that are not dismissed move.
func keepAscApprovals(ctx context.Context, pr *issues_model.PullRequest, newHead string) error {
	if strings.TrimSpace(newHead) == "" {
		return nil
	}
	_, err := db.GetEngine(ctx).Exec(
		"UPDATE `review` SET commit_id=?, stale=? WHERE issue_id=? AND type=? AND dismissed=?",
		newHead, false, pr.IssueID, issues_model.ReviewTypeApprove, false)
	return err
}

// AscStackMergeReady reports whether the stack merge is available for pr: ascsync must be enabled for its repo.
func AscStackMergeReady(pr *issues_model.PullRequest) bool {
	return pr != nil && pr.BaseRepo != nil && ascStackBackend != nil && func() bool {
		ok, err := ascsync.BackedBy(pr.BaseRepo)
		return err == nil && ok
	}()
}

// ---------------------------------------------------------------------------------- the asc backend

// ascStackTimeout bounds one asc call of the stack merge.
const ascStackTimeout = 60 * time.Second

type ascClientBackend struct {
	repo ascv1.RepoClient
	chg  ascv1.ChangesClient
}

// NewAscStackBackend returns the backend that calls asc with the internal token (a system call: Forgejo has
// already authorised the doer). It is used by the router at start.
func NewAscStackBackend() (AscStackBackend, error) {
	if setting.InternalToken == "" {
		return nil, errors.New("the internal token is not set")
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: ascStackTimeout})
	if err != nil {
		return nil, err
	}
	return &ascClientBackend{repo: c.Repo(), chg: c.Changes()}, nil
}

func (b *ascClientBackend) system(ctx context.Context, op uuid.UUID) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), ascStackTimeout)
	return ascclient.WithOpID(ctx, op), cancel
}

// opID makes a retry of the same request carry the same operation id.
func opID(kind string, parts ...string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(kind+"|"+strings.Join(parts, "|")))
}

func (b *ascClientBackend) gitToAsc(ctx context.Context, repo string, ids ...string) ([][]byte, error) {
	raw := make([][]byte, len(ids))
	for i, id := range ids {
		v, err := hex.DecodeString(id)
		if err != nil || len(v) == 0 {
			return nil, fmt.Errorf("bad git id %q", id)
		}
		raw[i] = v
	}
	resp, err := b.repo.GitMap(ctx, &ascv1.GitMapRequest{Repo: &ascv1.RepoRef{Name: repo}, Git: raw})
	if err != nil {
		return nil, fmt.Errorf("GitMap: %w", err)
	}
	if len(resp.GetGitToAsc()) != len(ids) {
		return nil, fmt.Errorf("GitMap answered %d ids for %d", len(resp.GetGitToAsc()), len(ids))
	}
	for _, v := range resp.GetGitToAsc() {
		if len(v) == 0 {
			return nil, errors.New("a pull request head is not in asc")
		}
	}
	return resp.GetGitToAsc(), nil
}

func (b *ascClientBackend) ascToGit(ctx context.Context, repo string, ids ...[]byte) ([]string, error) {
	resp, err := b.repo.GitMap(ctx, &ascv1.GitMapRequest{Repo: &ascv1.RepoRef{Name: repo}, Asc: ids})
	if err != nil {
		return nil, fmt.Errorf("GitMap: %w", err)
	}
	if len(resp.GetAscToGit()) != len(ids) {
		return nil, fmt.Errorf("GitMap answered %d ids for %d", len(resp.GetAscToGit()), len(ids))
	}
	out := make([]string, len(ids))
	for i, v := range resp.GetAscToGit() {
		if len(v) == 0 {
			return nil, errors.New("a landed changeset has no git id")
		}
		out[i] = hex.EncodeToString(v)
	}
	return out, nil
}

func (b *ascClientBackend) Land(ctx context.Context, req AscLandRequest) (*AscLandResult, error) {
	op := opID("land", req.Repo, req.Target, strings.Join(req.Heads, ","))
	ctx, cancel := b.system(ctx, op)
	defer cancel()
	asc, err := b.gitToAsc(ctx, req.Repo, req.Heads...)
	if err != nil {
		return nil, err
	}
	ref := &ascv1.RepoRef{Name: req.Repo}
	// The heads must form a chain: each one an ancestor of the next. Only then does head(N) carry exactly
	// the changes that were checked.
	for i := 0; i+1 < len(asc); i++ {
		mb, err := b.repo.MergeBase(ctx, &ascv1.MergeBaseRequest{Repo: ref, A: asc[i], B: [][]byte{asc[i+1]}})
		if err != nil {
			return nil, fmt.Errorf("MergeBase: %w", err)
		}
		if len(mb.GetBases()) != 1 || !bytes.Equal(mb.GetBases()[0], asc[i]) {
			return nil, fmt.Errorf("%w: the heads of the stack are not a chain", ErrAscStackNotMergable)
		}
	}
	resp, err := b.repo.UpdateRefs(ctx, &ascv1.UpdateRefsRequest{
		Repo: ref, Mode: ascv1.UpdateMode_UPDATE_MODE_LAND,
		Updates: []*ascv1.RefUpdate{{
			Name: req.Target, Expect: &ascv1.RefUpdate_Any{Any: true},
			New: &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: asc[len(asc)-1]}},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("UpdateRefs LAND: %w", err)
	}
	rewritten := map[string][]byte{}
	for _, rw := range resp.GetRewritten() {
		rewritten[string(rw.GetOld())] = rw.GetNew()
	}
	landed := make([][]byte, len(asc))
	for i, id := range asc {
		if n, ok := rewritten[string(id)]; ok {
			id = n
		}
		landed[i] = id
	}
	git, err := b.ascToGit(ctx, req.Repo, landed...)
	if err != nil {
		return nil, err
	}
	return &AscLandResult{Landed: git, Tip: git[len(git)-1]}, nil
}

func (b *ascClientBackend) Restack(ctx context.Context, req AscRestackRequest) ([]AscRestackOutcome, error) {
	parts := []string{req.Repo, req.Onto, req.CommitterName, req.CommitterEmail, fmt.Sprint(req.CommitterTime)}
	items := make([]*ascv1.RestackItem, len(req.Items))
	heads := make([]string, len(req.Items))
	changes := make([][]byte, len(req.Items))
	for i, it := range req.Items {
		c, err := ascChangeBytes(it.Change)
		if err != nil {
			return nil, err
		}
		changes[i], heads[i] = c, it.ExpectHead
		parts = append(parts, it.Change, it.ExpectHead)
	}
	ctx, cancel := b.system(ctx, opID("restack", parts...))
	defer cancel()
	asc, err := b.gitToAsc(ctx, req.Repo, append([]string{req.Onto}, heads...)...)
	if err != nil {
		return nil, err
	}
	for i, it := range req.Items {
		items[i] = &ascv1.RestackItem{
			Change: changes[i], UpdateRef: it.UpdateRef,
			Expect: &ascv1.RefTarget{T: &ascv1.RefTarget_Changeset{Changeset: asc[i+1]}},
		}
	}
	resp, err := b.chg.RestackChanges(ctx, &ascv1.RestackChangesRequest{
		Repo: &ascv1.RepoRef{Name: req.Repo}, Onto: hex.EncodeToString(asc[0]), Items: items, Atomic: true,
		CommitterName: req.CommitterName, CommitterEmail: req.CommitterEmail, CommitterTime: req.CommitterTime,
	})
	if err != nil {
		return nil, fmt.Errorf("RestackChanges: %w", err)
	}
	out := make([]AscRestackOutcome, len(req.Items))
	newOf := map[string][]byte{}
	for _, rw := range resp.GetRewritten() {
		newOf[string(rw.GetOld())] = rw.GetNew()
	}
	for i, it := range req.Items {
		out[i] = AscRestackOutcome{Change: it.Change}
		for _, c := range resp.GetConflicts() {
			if bytes.Equal(c.GetChangeset(), asc[i+1]) {
				out[i].Conflict = true
			}
		}
		n, ok := newOf[string(asc[i+1])]
		if !ok || out[i].Conflict {
			continue
		}
		g, err := b.ascToGit(ctx, req.Repo, n)
		if err != nil {
			return nil, err
		}
		out[i].NewHead = g[0]
		for _, p := range resp.GetPatchUnchanged() {
			if bytes.Equal(p, changes[i]) || bytes.Equal(p, n) {
				out[i].PatchUnchanged = true
			}
		}
	}
	return out, nil
}

// ascChangeBytes decodes the text form of a change id: letter c is the hex digit 'z' - c, high digit first.
func ascChangeBytes(change string) ([]byte, error) {
	if !issues_model.IsAscChange(change) {
		return nil, fmt.Errorf("bad change id")
	}
	out := make([]byte, 16)
	for i := range out {
		out[i] = (('z' - change[2*i]) << 4) | ('z' - change[2*i+1])
	}
	return out, nil
}
