// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/timeutil"
)

// "Revert landed stack" (SPEC section 20.8, PLAN unit C48).
//
// RevertAscStack reverts the landed pull requests from pr down to the bottom of its stack, newest first. Each
// one is found with FindLanding (never by walking the log or matching messages, SPEC section 8.9) and
// reverted with Apply(REVERT, update_ref = the base branch), so asc's UpdateRefs pipeline (branch protection,
// hooks, quotas) decides every write. Rules:
//   - The doer must be allowed to merge into the base branch of every pull request. A revert moves the same
//     branch a merge moves, so it needs the same permission.
//   - Every pull request must be merged, in one repository and on one base branch. The stack is walked from
//     the stored pull_request_asc_stack rows with a depth bound and a cycle check; a gap or a pull request
//     that is not merged refuses the whole call before asc is called (fail closed).
//   - The landing answers are verified before the first write: found, an asc id of the expected length, distinct
//     ids, and ref_log_seq not increasing from the newest pull request down. An answer out of order is
//     refused, so a revert never runs in the wrong order.
//   - Reverts run one at a time, newest first. The first conflict or error stops the run; the reverts
//     already applied stand and are reported. A retry of the same request carries the same operation ids.
//
// TODO(C22): coded to the Apply and FindLanding contracts; AscRevertBackend is the seam.

// Errors of the stack revert.
var (
	ErrAscRevertNotLanded  = errors.New("a pull request of the stack is not merged")
	ErrAscRevertNotAllowed = errors.New("not allowed to revert a pull request of the stack")
	ErrAscRevertDisabled   = errors.New("the repository is not stored in asc")
	ErrAscRevertBadAnswer  = errors.New("asc sent a landing answer that failed verification")
)

// AscLanding is a FindLanding answer.
type AscLanding struct {
	Found    bool
	LandedAs []byte // asc id, 32 bytes
	Seq      uint64 // ref_log_seq
}

// AscRevertRequest asks asc to revert one landed changeset on Ref.
type AscRevertRequest struct {
	Repo           string
	Ref            string // full ref name, onto and update_ref
	Changeset      []byte // the landed asc id
	CommitterName  string
	CommitterEmail string
	CommitterTime  int64
}

// AscRevertResult is asc's answer for one revert.
type AscRevertResult struct {
	Result   string // git id of the revert commit; empty on conflict
	Conflict bool
}

// AscRevertBackend is the asc side of the stack revert.
type AscRevertBackend interface {
	FindLanding(ctx context.Context, repo, ref, landedGit string) (*AscLanding, error)
	Revert(ctx context.Context, req AscRevertRequest) (*AscRevertResult, error)
}

var ascRevertBackend AscRevertBackend

// UseAscRevertBackend sets the backend until the returned function is called.
func UseAscRevertBackend(b AscRevertBackend) (restore func()) {
	prev := ascRevertBackend
	ascRevertBackend = b
	return func() { ascRevertBackend = prev }
}

// AscRevertItem is one landed pull request.
type AscRevertItem struct {
	PR     *issues_model.PullRequest
	Change string
	Landed []byte
	Seq    uint64
}

// AscRevertOutcome reports one revert.
type AscRevertOutcome struct {
	PR       *issues_model.PullRequest
	Result   string
	Conflict bool
}

// AscRevertStackResult reports a stack revert. Done lists the reverts that were applied, newest first.
// Stopped is set when a conflict or an error (Err) ended the run before every pull request was reverted.
type AscRevertStackResult struct {
	Done    []AscRevertOutcome
	Stopped *AscRevertOutcome
	Err     error
}

// AscLandedStack returns pr and the merged pull requests below it, newest (pr) first. It fails closed.
func AscLandedStack(ctx context.Context, pr *issues_model.PullRequest) ([]*AscRevertItem, error) {
	if pr == nil || pr.ID == 0 {
		return nil, ErrAscStackNotStacked
	}
	seen := map[int64]bool{}
	var out []*AscRevertItem
	cur := pr
	for {
		if seen[cur.ID] {
			return nil, fmt.Errorf("%w: cycle", ErrAscStackNotMergable)
		}
		seen[cur.ID] = true
		if len(out) >= ascStackMaxDepth {
			return nil, fmt.Errorf("%w: stack too deep", ErrAscStackNotMergable)
		}
		row, err := issues_model.GetPullRequestAscStack(ctx, cur.ID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, ErrAscStackNotStacked
		}
		if !cur.HasMerged || cur.MergedCommitID == "" {
			return nil, fmt.Errorf("%w: #%d", ErrAscRevertNotLanded, cur.Index)
		}
		out = append(out, &AscRevertItem{PR: cur, Change: row.AscChange})
		if row.StackParentID == 0 {
			return out, nil
		}
		parent, err := issues_model.GetPullRequestByID(ctx, row.StackParentID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				return nil, fmt.Errorf("%w: a parent pull request is gone", ErrAscRevertNotLanded)
			}
			return nil, err
		}
		cur = parent
	}
}

// RevertAscStack reverts pr and the landed pull requests below it, newest first. perm is the doer's
// permission on the base repository.
func RevertAscStack(ctx context.Context, doer *user_model.User, perm access_model.Permission, pr *issues_model.PullRequest) (*AscRevertStackResult, error) {
	if doer == nil {
		return nil, ErrAscRevertNotAllowed
	}
	backend := ascRevertBackend
	if backend == nil {
		return nil, fmt.Errorf("%w: no backend", ErrAscRevertDisabled)
	}
	items, err := AscLandedStack(ctx, pr)
	if err != nil {
		return nil, err
	}
	first := items[0].PR
	if err := first.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.PR.BaseRepoID != first.BaseRepoID || it.PR.BaseBranch != first.BaseBranch {
			return nil, fmt.Errorf("%w: #%d targets another branch", ErrAscStackNotMergable, it.PR.Index)
		}
		ok, err := IsUserAllowedToMerge(ctx, it.PR, perm, doer)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: #%d", ErrAscRevertNotAllowed, it.PR.Index)
		}
	}

	repo := first.BaseRepo.FullName()
	ref := git.BranchPrefix + first.BaseBranch
	seenLanded := map[string]bool{}
	for _, it := range items {
		l, err := backend.FindLanding(ctx, repo, ref, it.PR.MergedCommitID)
		if err != nil {
			return nil, err
		}
		if l == nil || !l.Found || len(l.LandedAs) != 32 {
			return nil, fmt.Errorf("%w: #%d", ErrAscRevertBadAnswer, it.PR.Index)
		}
		if seenLanded[string(l.LandedAs)] {
			return nil, fmt.Errorf("%w: two pull requests landed as one changeset", ErrAscRevertBadAnswer)
		}
		seenLanded[string(l.LandedAs)] = true
		it.Landed, it.Seq = l.LandedAs, l.Seq
	}
	// Newest first: the log position of a pull request is never below that of the one under it.
	for i := 0; i+1 < len(items); i++ {
		if items[i].Seq < items[i+1].Seq {
			return nil, fmt.Errorf("%w: landings are out of order", ErrAscRevertBadAnswer)
		}
	}

	res := &AscRevertStackResult{}
	now := timeutil.TimeStampNow().AsTime().Unix()
	for _, it := range items {
		out, err := backend.Revert(ctx, AscRevertRequest{
			Repo: repo, Ref: ref, Changeset: it.Landed,
			CommitterName: doer.DisplayName(), CommitterEmail: doer.GetEmail(), CommitterTime: now,
		})
		if err != nil {
			res.Err = err
			res.Stopped = &AscRevertOutcome{PR: it.PR}
			return res, nil
		}
		o := AscRevertOutcome{PR: it.PR, Result: out.Result, Conflict: out.Conflict}
		if out.Conflict || out.Result == "" {
			res.Stopped = &o
			return res, nil
		}
		res.Done = append(res.Done, o)
	}
	return res, nil
}

// FindLanding asks asc where a landed git commit went on ref.
func (b *ascClientBackend) FindLanding(ctx context.Context, repo, ref, landedGit string) (*AscLanding, error) {
	ctx, cancel := b.system(ctx, opID("find-landing", repo, ref, landedGit))
	defer cancel()
	asc, err := b.gitToAsc(ctx, repo, landedGit)
	if err != nil {
		return nil, err
	}
	resp, err := b.repo.FindLanding(ctx, &ascv1.FindLandingRequest{Repo: &ascv1.RepoRef{Name: repo}, Changeset: asc[0], Ref: ref})
	if err != nil {
		return nil, fmt.Errorf("FindLanding: %w", err)
	}
	return &AscLanding{Found: resp.GetFound(), LandedAs: resp.GetLandedAs(), Seq: resp.GetRefLogSeq()}, nil
}

// Revert applies Apply(REVERT) of one landed changeset onto ref and moves ref to the result.
func (b *ascClientBackend) Revert(ctx context.Context, req AscRevertRequest) (*AscRevertResult, error) {
	cs := hex.EncodeToString(req.Changeset)
	ctx, cancel := b.system(ctx, opID("revert", req.Repo, req.Ref, cs, req.CommitterName, req.CommitterEmail, fmt.Sprint(req.CommitterTime)))
	defer cancel()
	sig := &ascv1.Signature{Name: []byte(req.CommitterName), Email: []byte(req.CommitterEmail), Time: req.CommitterTime}
	resp, err := b.repo.Apply(ctx, &ascv1.ApplyRequest{
		Repo: &ascv1.RepoRef{Name: req.Repo}, Onto: req.Ref, Changeset: cs,
		Mode: ascv1.ApplyMode_APPLY_MODE_REVERT, Author: sig, Committer: sig, UpdateRef: req.Ref,
	})
	if err != nil {
		return nil, fmt.Errorf("Apply REVERT: %w", err)
	}
	if len(resp.GetConflicts()) > 0 {
		return &AscRevertResult{Conflict: true}, nil
	}
	g, err := b.ascToGit(ctx, req.Repo, resp.GetResult())
	if err != nil {
		return nil, err
	}
	return &AscRevertResult{Result: g[0]}, nil
}
