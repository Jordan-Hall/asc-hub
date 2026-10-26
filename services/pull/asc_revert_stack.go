// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascbridge"
)

// "Revert landed stack" (SPEC section 20.8, docs/BUILDOUT.md section 6).
//
// RevertAscStack reverts a landed stack with Stacks.RevertLanded: asc finds each landing from the ref log (never
// by matching messages, SPEC section 8.9), reverts from the top of the landed stack down, and stores the
// reverts as a stack of NEW changes. It never writes trunk. The hub then opens one pull request per revert,
// stacked on the one below, so the revert is reviewed and merged like any other change. Rules:
//   - The stack is walked from the stored pull_request_asc_stack rows with a depth bound and a cycle check.
//     Every pull request from pr down to the bottom (or to Bottom) must be merged, in one repository and on
//     one base branch. A gap or a pull request that is not merged refuses the call before asc is called.
//   - The doer needs write access to the code of the repository (the new pull requests are theirs), and the
//     repository must not be archived. Merging a revert is still governed by branch protection.
//   - The answer is verified: each landed pull request has exactly one revert (or one revert for all with
//     Squash), no revert repeats, and an applied answer has ids and no conflicts.
//   - A conflict (REVERT_CONFLICT) lists the paths and stores nothing. A dry run reports the same without
//     storing anything.
//   - One revert of a stack runs at a time; a second request while one is running is refused (ErrAscRevertBusy).
//   - Errors never carry the server's message.

// Errors of the stack revert.
var (
	ErrAscRevertNotLanded  = errors.New("a pull request of the stack is not merged")
	ErrAscRevertNotAllowed = errors.New("not allowed to revert a pull request of the stack")
	ErrAscRevertDisabled   = errors.New("the repository is not stored in asc")
	ErrAscRevertBadAnswer  = errors.New("asc sent a revert answer that failed verification")
	ErrAscRevertBusy       = errors.New("a revert of this stack is already running")
	ErrAscRevertBottom     = errors.New("the bottom pull request is not part of the landed stack")
)

// AscRevertItem is one landed pull request.
type AscRevertItem struct {
	PR     *issues_model.PullRequest
	Change string
}

// AscRevertOptions are the options of RevertAscStack.
type AscRevertOptions struct {
	// DryRun reports the reverts and their conflicts and stores nothing.
	DryRun bool
	// Squash makes one revert change for the whole selection.
	Squash bool
	// Bottom, when set, is the lowest pull request to revert; the default is the bottom of the stack.
	Bottom *issues_model.PullRequest
}

// AscRevertPlan is one revert as asc answered it, bottom of the new stack first.
type AscRevertPlan struct {
	Original  []*issues_model.PullRequest // the landed pull requests this revert undoes
	Change    string                      // the new change; empty on a dry run
	Conflicts []string
}

// AscRevertStackResult reports a revert. Created are the new pull requests, bottom first. When the pull
// requests could not all be opened, Err says why and Created holds those that were: the reverts exist in asc
// under Ref and can be submitted from there.
type AscRevertStackResult struct {
	Selected []*AscRevertItem // newest first
	Plans    []AscRevertPlan
	Ref      string
	Applied  bool
	Created  []*issues_model.PullRequest
	Err      error
}

// HasConflicts reports whether any revert of a dry run conflicts.
func (r *AscRevertStackResult) HasConflicts() bool {
	for _, p := range r.Plans {
		if len(p.Conflicts) > 0 {
			return true
		}
	}
	return false
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

var ascRevertRunning sync.Map // top pull request id -> struct{}

// AscMayRevert reports whether doer may revert a landed stack in the repository of perm.
func AscMayRevert(doer *user_model.User, perm access_model.Permission) bool {
	return doer != nil && doer.IsActive && !doer.ProhibitLogin && perm.CanWrite(unit.TypeCode) && perm.CanReadIssuesOrPulls(true)
}

// RevertAscStack reverts pr and the landed pull requests below it. perm is the doer's permission on the base
// repository.
func RevertAscStack(ctx context.Context, doer *user_model.User, perm access_model.Permission, pr *issues_model.PullRequest, opt AscRevertOptions) (*AscRevertStackResult, error) {
	if !AscMayRevert(doer, perm) {
		return nil, ErrAscRevertNotAllowed
	}
	items, err := AscLandedStack(ctx, pr)
	if err != nil {
		return nil, err
	}
	first := items[0].PR
	if err := first.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	repo := first.BaseRepo
	if repo.IsArchived {
		return nil, ErrAscRevertNotAllowed
	}
	for _, it := range items {
		if it.PR.BaseRepoID != first.BaseRepoID || it.PR.BaseBranch != first.BaseBranch {
			return nil, fmt.Errorf("%w: #%d targets another branch", ErrAscStackNotMergable, it.PR.Index)
		}
	}
	if opt.Bottom != nil {
		cut := -1
		for i, it := range items {
			if it.PR.ID == opt.Bottom.ID {
				cut = i
			}
		}
		if cut < 0 {
			return nil, ErrAscRevertBottom
		}
		items = items[:cut+1]
	}
	if backed, err := ascStackBacked(repo); err != nil {
		return nil, err
	} else if !backed {
		return nil, ErrAscRevertDisabled
	}
	ops, err := ascbridge.StackClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscRevertDisabled, err)
	}

	if !opt.DryRun {
		if _, busy := ascRevertRunning.LoadOrStore(first.ID, struct{}{}); busy {
			return nil, ErrAscRevertBusy
		}
		defer ascRevertRunning.Delete(first.ID)
	}

	req := ascbridge.RevertLandedRequest{Repo: repo.FullName(), Change: items[0].Change, Squash: opt.Squash, DryRun: opt.DryRun}
	if opt.Bottom != nil {
		req.Bottom = items[len(items)-1].Change
	}
	ans, err := ops.RevertLanded(ctx, req)
	if err != nil {
		return nil, err
	}
	res := &AscRevertStackResult{Selected: items, Ref: ans.Ref, Applied: ans.Applied}
	if res.Plans, err = ascRevertPlans(items, ans, opt); err != nil {
		return nil, err
	}
	if opt.DryRun {
		return res, nil
	}
	if !ans.Applied {
		return nil, fmt.Errorf("%w: asc did not apply the revert", ErrAscRevertBadAnswer)
	}
	log.Info("asc audit: revert landed stack: repository %d, top pull request %d: user %d, %d reverts stored under %s",
		repo.ID, first.ID, doer.ID, len(ans.Reverts), ans.Ref)

	var parentID int64
	for i, rv := range ans.Reverts {
		np, err := ascOpenRevertPull(ctx, doer, repo, first.BaseBranch, res.Plans[i], rv, parentID)
		if err != nil {
			res.Err = fmt.Errorf("the reverts are stored in asc but the pull request for revert %d could not be opened: %w", i+1, err)
			return res, nil
		}
		res.Created = append(res.Created, np)
		parentID = np.ID
	}
	return res, nil
}

// ascRevertPlans checks asc's answer against the selection and pairs each revert with the landed pull
// requests it undoes.
func ascRevertPlans(items []*AscRevertItem, ans *ascbridge.RevertLandedResult, opt AscRevertOptions) ([]AscRevertPlan, error) {
	byChange := map[string]*issues_model.PullRequest{}
	for _, it := range items {
		byChange[it.Change] = it.PR
	}
	want := len(items)
	if opt.Squash {
		want = 1
	}
	if len(ans.Reverts) != want {
		return nil, fmt.Errorf("%w: %d reverts for %d pull requests", ErrAscRevertBadAnswer, len(ans.Reverts), len(items))
	}
	seen := map[string]bool{}
	seenNew := map[string]bool{}
	plans := make([]AscRevertPlan, len(ans.Reverts))
	for i, rv := range ans.Reverts {
		pr, ok := byChange[rv.Original]
		if !ok || seen[rv.Original] {
			return nil, fmt.Errorf("%w: a revert names a change outside the selection", ErrAscRevertBadAnswer)
		}
		seen[rv.Original] = true
		plans[i] = AscRevertPlan{Original: []*issues_model.PullRequest{pr}, Change: rv.RevertChange, Conflicts: rv.Conflicts}
		if opt.Squash {
			plans[i].Original = nil
			for j := len(items) - 1; j >= 0; j-- { // bottom first
				plans[i].Original = append(plans[i].Original, items[j].PR)
			}
		}
		if !opt.DryRun {
			if !issues_model.IsAscChange(rv.RevertChange) || seenNew[rv.RevertChange] || byChange[rv.RevertChange] != nil || (len(rv.RevertGit) != 40 && len(rv.RevertGit) != 64) {
				return nil, fmt.Errorf("%w: a revert has a bad change or commit id", ErrAscRevertBadAnswer)
			}
			seenNew[rv.RevertChange] = true
		}
	}
	return plans, nil
}

// ascRevertTitle is the title of the pull request of a revert: Revert "<title>" of the newest pull request,
// at most 255 characters.
func ascRevertTitle(ctx context.Context, originals []*issues_model.PullRequest) string {
	title := "Revert"
	if len(originals) > 0 {
		top := originals[len(originals)-1]
		if err := top.LoadIssue(ctx); err == nil {
			title = `Revert "` + top.Issue.Title + `"`
		}
	}
	if len(originals) > 1 {
		title += fmt.Sprintf(" (and %d more)", len(originals)-1)
	}
	if utf8.RuneCountInString(title) > 255 {
		title = string([]rune(title)[:254]) + "…"
	}
	return title
}

// ascOpenRevertPull opens the pull request of one revert. Tests replace it.
var ascOpenRevertPull = openAscRevertPull

// openAscRevertPull opens the pull request of one revert, stacked on parentID.
func openAscRevertPull(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, base string, plan AscRevertPlan, rv ascbridge.RevertedChange, parentID int64) (*issues_model.PullRequest, error) {
	var refs strings.Builder
	refs.WriteString("Reverts")
	for i, o := range plan.Original {
		if i > 0 {
			refs.WriteString(",")
		}
		fmt.Fprintf(&refs, " #%d", o.Index)
	}
	refs.WriteString(".")
	title := ascRevertTitle(ctx, plan.Original)
	issue := &issues_model.Issue{RepoID: repo.ID, Title: title, PosterID: doer.ID, Poster: doer, IsPull: true, Content: refs.String()}
	pr := &issues_model.PullRequest{
		HeadRepoID: repo.ID, BaseRepoID: repo.ID,
		HeadBranch:   strings.ToLower(doer.Name) + "/asc-" + rv.RevertChange,
		HeadCommitID: rv.RevertGit, BaseBranch: base,
		HeadRepo: repo, BaseRepo: repo,
		Type: issues_model.PullRequestGitea, Flow: issues_model.PullRequestFlowAGit,
	}
	if err := NewPullRequest(ctx, repo, issue, nil, nil, pr, nil); err != nil {
		return nil, err
	}
	if err := issues_model.SetPullRequestAscStack(ctx, pr, rv.RevertChange, parentID); err != nil {
		return nil, err
	}
	if err := RecomputeAscStackBase(ctx, pr); err != nil {
		return nil, err
	}
	return pr, nil
}
