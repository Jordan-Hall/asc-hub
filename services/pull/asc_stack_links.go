// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascsync"
)

// The parent of a stacked pull request is the pull request whose head is the git parent of its head: a stack is a
// line of commits, each change on the one below. asc's own answer for the parent change (ResolveChanges) can be
// missing while the push hook runs, so the links are also made from git. Three rules keep that safe:
//   - a page only reads: it works the parent out in memory (ascViewParent) and writes nothing;
//   - a link is stored only for the pull requests of a push (LinkAscStackPush), after a merge was allowed to the
//     person (PersistAscStackLinks), or by the doctor command (RepairAscStackLinks), and only where no parent is
//     recorded yet;
//   - the parent is never a guess (AscGitParent): one open pull request on the commit, the same poster or asc's
//     confirmation, and a bounded look at the pull request refs.

const (
	// ascParentScan bounds how many pull request refs are looked at for one parent.
	ascParentScan = 16
	// ascRepairPage is the page of open stacked pull requests the doctor reads at a time.
	ascRepairPage = 200
)

// AscParentChange asks asc for the parent change of a change ("" for the bottom of a stack). The agit package sets
// it. Without it (nil) nothing is confirmed by asc.
var AscParentChange func(ctx context.Context, repo *repo_model.Repository, change string) (string, error)

// OpenAscBridge opens the git repository the hub reads the commits of a repository from: the git bridge of an asc
// repository, the repository itself otherwise. The caller closes it.
func OpenAscBridge(ctx context.Context, repo *repo_model.Repository) (*git.Repository, error) {
	return ascBridgeOpener(ctx, repo)
}

// ascBridgeOpener is OpenAscBridge. Tests replace it.
var ascBridgeOpener = func(ctx context.Context, repo *repo_model.Repository) (*git.Repository, error) {
	isAsc, err := ascsync.BackedBy(repo)
	if err != nil {
		return nil, fmt.Errorf("storage backend of repository %q: %w", repo.FullName(), err)
	}
	if !isAsc {
		return git.OpenRepository(ctx, repo.RepoPath())
	}
	return git.OpenRepositoryWithBackend(ctx, repo.RepoPath(), git.DefaultBackendName)
}

// AscGitParent finds the pull request that sits right below pr in git, or nil. It only reads, and it answers nil
// rather than guess:
//   - the parent is the open stacked pull request of the same base repository whose head is the first parent of head
//     (the pull requests of this push, in inPush, and the pull request refs that point at that commit; at most
//     ascParentScan refs are looked at);
//   - when more than one pull request has that head the answer is nil (it is ambiguous);
//   - a parent of the same poster is accepted; a parent of another person only when asc confirms it: the parent
//     change asc knows for the change of pr is the change of that pull request. Anyone who can open a pull request
//     could otherwise make their commit the parent of somebody else's change.
func AscGitParent(ctx context.Context, bridge *git.Repository, pr *issues_model.PullRequest, head string, inPush map[int64]string) (*issues_model.PullRequest, error) {
	id, err := git.NewIDFromString(head)
	if err != nil {
		return nil, nil
	}
	commit, err := bridge.GetCommit(id.String())
	if err != nil {
		return nil, nil
	}
	parentID, err := commit.ParentID(0)
	if err != nil {
		return nil, nil
	}
	want := parentID.String()

	cands := map[int64]*issues_model.PullRequest{}
	add := func(c *issues_model.PullRequest) error {
		if c == nil || c.ID == pr.ID || c.BaseRepoID != pr.BaseRepoID || c.HasMerged {
			return nil
		}
		if err := c.LoadIssue(ctx); err != nil {
			return err
		}
		if c.Issue.IsClosed {
			return nil
		}
		row, err := issues_model.GetPullRequestAscStack(ctx, c.ID)
		if err != nil || row == nil {
			return err
		}
		cands[c.ID] = c
		return nil
	}
	for cid, h := range inPush {
		if h != want || cid == pr.ID {
			continue
		}
		c, err := issues_model.GetPullRequestByID(ctx, cid)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := add(c); err != nil {
			return nil, err
		}
	}
	out, _, err := git.NewCommand(ctx, "for-each-ref", "--format=%(refname)").
		AddOptionFormat("--count=%d", ascParentScan+1).
		AddOptionFormat("--points-at=%s", want).
		AddDynamicArguments("refs/pull/").
		RunStdString(&git.RunOpts{Dir: bridge.Path})
	if err != nil {
		return nil, nil
	}
	refs := strings.Fields(out)
	if len(refs) > ascParentScan {
		return nil, nil // too many pull requests on one commit: ambiguous
	}
	for _, ref := range refs {
		idx, ok := strings.CutSuffix(strings.TrimPrefix(ref, "refs/pull/"), "/head")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(idx, 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		c, err := issues_model.GetPullRequestByIndex(ctx, pr.BaseRepoID, n)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				continue
			}
			return nil, err
		}
		if err := add(c); err != nil {
			return nil, err
		}
	}
	if len(cands) != 1 {
		return nil, nil
	}
	var parent *issues_model.PullRequest
	for _, c := range cands {
		parent = c
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return nil, err
	}
	if parent.Issue.PosterID == pr.Issue.PosterID {
		return parent, nil
	}
	return confirmedByAsc(ctx, pr, parent)
}

// confirmedByAsc returns parent when asc says the parent change of the change of pr is the change of parent.
func confirmedByAsc(ctx context.Context, pr, parent *issues_model.PullRequest) (*issues_model.PullRequest, error) {
	if AscParentChange == nil {
		return nil, nil
	}
	own, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil || own == nil {
		return nil, err
	}
	below, err := issues_model.GetPullRequestAscStack(ctx, parent.ID)
	if err != nil || below == nil {
		return nil, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	ch, err := AscParentChange(ctx, pr.BaseRepo, own.AscChange)
	if err != nil || ch == "" || ch != below.AscChange {
		return nil, nil
	}
	return parent, nil
}

// ascViewParent is AscGitParent for a page: it opens the repository and reads the head from the pull request ref.
// It never writes. Any failure is "no parent".
func ascViewParent(ctx context.Context, pr *issues_model.PullRequest) (*issues_model.PullRequest, string) {
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, ""
	}
	bridge, err := OpenAscBridge(ctx, pr.BaseRepo)
	if err != nil {
		return nil, ""
	}
	defer bridge.Close()
	head, err := bridge.GetRefCommitID(pr.GetGitRefName())
	if err != nil {
		return nil, ""
	}
	parent, err := AscGitParent(ctx, bridge, pr, head, nil)
	if err != nil {
		return nil, ""
	}
	return parent, head
}

// setAscParentFromGit stores parent as the stack parent of pr when pr has none (it never replaces a parent that asc
// or an earlier push recorded) and fixes the merge base the rule gives.
func setAscParentFromGit(ctx context.Context, pr, parent *issues_model.PullRequest, head string) (bool, error) {
	row, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil || row == nil || row.StackParentID != 0 {
		return false, err
	}
	if err := issues_model.SetAscStackParent(ctx, pr.ID, parent.ID); err != nil {
		return false, err
	}
	if head != "" {
		pr.HeadCommitID = head // the merge base rule reads the head from the pull request
	}
	if err := RecomputeAscStackBase(ctx, pr); err != nil {
		log.Error("asc stack: base of pull request %d: %v", pr.ID, err)
	}
	return true, nil
}

// LinkAscStackPush fills the missing stack parent of each pull request in heads (the pull requests a push made or
// moved, and their new heads) from git (AscGitParent). It does nothing for a pull request that has a parent. It
// returns how many it linked.
func LinkAscStackPush(ctx context.Context, bridge *git.Repository, heads map[int64]string) (int, error) {
	linked := 0
	for id, head := range heads {
		pr, err := issues_model.GetPullRequestByID(ctx, id)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				continue
			}
			return linked, err
		}
		row, err := issues_model.GetPullRequestAscStack(ctx, id)
		if err != nil {
			return linked, err
		}
		if row == nil || row.StackParentID != 0 {
			continue
		}
		parent, err := AscGitParent(ctx, bridge, pr, head, heads)
		if err != nil {
			return linked, err
		}
		if parent == nil {
			continue
		}
		ok, err := setAscParentFromGit(ctx, pr, parent, head)
		if err != nil {
			return linked, err
		}
		if ok {
			linked++
		}
	}
	return linked, nil
}

// PersistAscStackLinks stores the parents that AscStackPrefix derived from git for items (Derived), so that the next
// walk finds them. The caller has checked that the doer may merge every item: a page never calls this.
func PersistAscStackLinks(ctx context.Context, items []*AscStackItem) error {
	for i, it := range items {
		if !it.Derived || i == 0 {
			continue
		}
		if _, err := setAscParentFromGit(ctx, it.PR, items[i-1].PR, it.Head); err != nil {
			return err
		}
	}
	return nil
}

// RepairAscStackLinks fills the missing stack parents of the open stacked pull requests of repo from git. It is for the
// doctor command, which an administrator runs: it reads the repository a page at a time. It returns how many it
// linked.
func RepairAscStackLinks(ctx context.Context, repo *repo_model.Repository) (int, error) {
	bridge, err := OpenAscBridge(ctx, repo)
	if err != nil {
		return 0, err
	}
	defer bridge.Close()
	linked := 0
	var after int64
	for {
		prs, err := issues_model.ListOpenAscStackPulls(ctx, repo.ID, after, ascRepairPage)
		if err != nil || len(prs) == 0 {
			return linked, err
		}
		after = prs[len(prs)-1].ID
		heads := map[int64]string{}
		for _, pr := range prs {
			if head, err := bridge.GetRefCommitID(pr.GetGitRefName()); err == nil {
				heads[pr.ID] = head
			}
		}
		n, err := LinkAscStackPush(ctx, bridge, heads)
		linked += n
		if err != nil {
			return linked, err
		}
		if len(prs) < ascRepairPage {
			return linked, nil
		}
	}
}

// MarkLandedAscStackMerged marks the open stacked pull requests of repo whose head commit is reachable from their base
// branch as merged: asc landed them (a land of the top of a stack carries every change below it), but the hub did
// not hear about each one. Nobody is named as the merger (the ghost user): the hub does not know who landed. No
// notification is sent for a land that happened before. A rewritten land (asc rebased the change onto a moved
// trunk) is not found by this, because its head is not in the trunk. It is for the doctor command, which reads a
// page at a time; it returns how many it marked.
func MarkLandedAscStackMerged(ctx context.Context, repo *repo_model.Repository) (int, error) {
	bridge, err := OpenAscBridge(ctx, repo)
	if err != nil {
		return 0, err
	}
	defer bridge.Close()
	marked := 0
	var after int64
	for {
		prs, err := issues_model.ListOpenAscStackPulls(ctx, repo.ID, after, ascRepairPage)
		if err != nil || len(prs) == 0 {
			return marked, err
		}
		after = prs[len(prs)-1].ID
		for _, pr := range prs {
			head, err := bridge.GetRefCommitID(pr.GetGitRefName())
			if err != nil || head == "" {
				continue
			}
			in, err := bridge.IsCommitInBranch(head, pr.BaseBranch)
			if err != nil || !in {
				continue
			}
			pr.Merger = user_model.NewGhostUser()
			pr.MergerID = pr.Merger.ID
			pr.MergedCommitID = head
			pr.MergedUnix = timeutil.TimeStampNow()
			pr.Status = issues_model.PullRequestStatusMergeable
			if _, err := pr.SetMerged(ctx); err != nil {
				log.Error("asc stack: marking pull request %d merged: %v", pr.ID, err)
				continue
			}
			cache.Remove(repo.GetCommitsCountCacheKey(pr.BaseBranch, true))
			marked++
		}
		if len(prs) < ascRepairPage {
			return marked, nil
		}
	}
}
