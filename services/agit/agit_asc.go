// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package agit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
	pull_service "forgejo.org/services/pull"
)

// Stacked PRs over AGit (SPEC section 20.5 flow 1, PLAN unit C34).
//
// `asc submit` pushes refs/for/<trunk>/asc-<change>, one ref per change, bottom to top. For a repository on the
// asc backend the pull request is found by (repo, change), not by (topic, user); only the author, an admin or
// owner, or a writer when the pull request allows maintainer edits may update it, so a forged `preds` cannot
// replace someone else's pull request head; and stack_parent_id is set from the parent change's pull request.

const ascTopicPrefix = "asc-"

// ascChangeOfTopic returns the change id of a topic of the form asc-<32 letters k-z>.
func ascChangeOfTopic(topic string) (string, bool) {
	change, ok := strings.CutPrefix(topic, ascTopicPrefix)
	if !ok || !issues_model.IsAscChange(change) {
		return "", false
	}
	return change, true
}

// ascChangeBytes decodes the text form of a change id: letter c is the hex digit 'z' - c, high digit first.
func ascChangeBytes(change string) ([]byte, error) {
	if !issues_model.IsAscChange(change) {
		return nil, errors.New("not an asc change id")
	}
	out := make([]byte, 16)
	for i := range out {
		out[i] = (('z' - change[2*i]) << 4) | ('z' - change[2*i+1])
	}
	return out, nil
}

// ascChangeText encodes a 16-byte change id to its text form.
func ascChangeText(b []byte) (string, error) {
	if len(b) != 16 {
		return "", errors.New("bad change id length")
	}
	out := make([]byte, 32)
	for i, v := range b {
		out[2*i] = 'z' - (v >> 4)
		out[2*i+1] = 'z' - (v & 0xf)
	}
	return string(out), nil
}

// shortChange is the 12 letter short form shown to users.
func shortChange(change string) string {
	if len(change) > 12 {
		return change[:12]
	}
	return change
}

// ascUpdateAllowed is the update authorisation of flow 1.
func ascUpdateAllowed(isAuthor, isAdminOrOwner, canWrite, allowMaintainerEdit bool) bool {
	return isAuthor || isAdminOrOwner || (canWrite && allowMaintainerEdit)
}

// ascMayUpdate decides whether pusher may update the pull request pr of a change.
func ascMayUpdate(ctx context.Context, repo *repo_model.Repository, pr *issues_model.PullRequest, pusher *user_model.User) (bool, error) {
	if pusher == nil {
		return false, nil
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return false, err
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, pusher)
	if err != nil {
		return false, err
	}
	return ascUpdateAllowed(
		pr.Issue.PosterID == pusher.ID,
		pusher.IsAdmin || perm.IsOwner() || perm.IsAdmin(),
		perm.CanWrite(unit.TypeCode),
		pr.AllowMaintainerEdit,
	), nil
}

var (
	ascChangesMu  sync.Mutex
	ascChangesAPI ascv1.ChangesClient

	// ascParentOf returns the text form of the parent change of change, or "" for the bottom of a stack.
	// Tests replace it.
	ascParentOf = func(ctx context.Context, repo *repo_model.Repository, change string) (string, error) {
		raw, err := ascChangeBytes(change)
		if err != nil {
			return "", err
		}
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo.RepoPath())
		if err != nil {
			return "", err
		}
		api, err := changesAPI()
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), 30*time.Second)
		defer cancel()
		resp, err := api.ResolveChanges(ctx, &ascv1.ResolveChangesRequest{Repo: &ascv1.RepoRef{Name: name}, Changes: [][]byte{raw}})
		if err != nil {
			return "", errors.New("asc ResolveChanges failed")
		}
		if len(resp.GetChanges()) != 1 {
			return "", errors.New("asc does not know the change")
		}
		p := resp.GetChanges()[0].GetParentChange()
		if len(p) == 0 {
			return "", nil
		}
		return ascChangeText(p)
	}
)

func changesAPI() (ascv1.ChangesClient, error) {
	ascChangesMu.Lock()
	defer ascChangesMu.Unlock()
	if ascChangesAPI == nil {
		if setting.InternalToken == "" {
			return nil, errors.New("the internal token is not set")
		}
		c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: 30 * time.Second})
		if err != nil {
			return nil, fmt.Errorf("asc client: %w", err)
		}
		ascChangesAPI = c.Changes()
	}
	return ascChangesAPI, nil
}

// ascIsStacked returns the change of topic when pushes to it take the asc stack path: the repository is on the
// asc backend and the topic is asc-<change>.
func ascIsStacked(repo *repo_model.Repository, topic string) (string, bool) {
	change, ok := ascChangeOfTopic(topic)
	if !ok || !setting.Asc.Enabled {
		return "", false
	}
	backed, err := ascsync.BackedBy(repo)
	if err != nil || !backed {
		return "", false
	}
	return change, true
}

// findAgitPullRequest finds the open pull request of a push. A stacked topic is found by (repo, change); any
// other by (head branch, base branch) as before.
func findAgitPullRequest(ctx context.Context, repo *repo_model.Repository, stackedChange, headBranch, baseBranch string) (*issues_model.PullRequest, error) {
	if stackedChange == "" {
		return issues_model.GetUnmergedPullRequest(ctx, repo.ID, repo.ID, headBranch, baseBranch, issues_model.PullRequestFlowAGit)
	}
	return issues_model.GetOpenPullRequestByAscChange(ctx, repo.ID, stackedChange)
}

// recordAscStack links pr to change and sets stack_parent_id from the parent change's pull request, then
// applies the merge_base rule.
//
// The asc server records the versions of a push only after the push was accepted, so the parent it answers
// while the hook runs can be empty for a change that has one. The git parent of the head commit is the head of
// the parent change (a change is committed on its parent), so a change without a parent from the server is
// linked to the open pull request whose head is the git parent of its own head.
func recordAscStack(ctx context.Context, repo *repo_model.Repository, pushed *git.Repository, pr *issues_model.PullRequest, change string) error {
	parent, err := ascParentOf(ctx, repo, change)
	if err != nil {
		return err
	}
	var parentID int64
	if parent == "" && pushed != nil {
		parentID = ascParentByGit(ctx, repo, pushed, pr)
	}
	if parent != "" {
		pp, err := issues_model.GetOpenPullRequestByAscChange(ctx, repo.ID, parent)
		switch {
		case err == nil:
			parentID = pp.ID
		case issues_model.IsErrPullRequestNotExist(err):
			// The parent was never submitted, or is merged: the change is the bottom of the open stack.
		default:
			return err
		}
	}
	if err := issues_model.SetPullRequestAscStack(ctx, pr, change, parentID); err != nil {
		return err
	}
	if err := adoptAscChildren(ctx, repo, pr, change); err != nil {
		log.Error("asc stack: adopting the children of change %s: %v", shortChange(change), err)
	}
	return pull_service.RecomputeAscStackBase(ctx, pr)
}

// ascParentByGit returns the id of the open stacked pull request whose head is the first parent of the head of pr,
// or 0.
func ascParentByGit(ctx context.Context, repo *repo_model.Repository, pushed *git.Repository, pr *issues_model.PullRequest) int64 {
	if pr.HeadCommitID == "" {
		return 0
	}
	commit, err := pushed.GetCommit(pr.HeadCommitID)
	if err != nil || commit.ParentCount() != 1 {
		return 0
	}
	parentSHA, err := commit.ParentID(0)
	if err != nil {
		return 0
	}
	open, err := issues_model.ListOpenAscPullRequests(ctx, repo.ID, ascParentScan)
	if err != nil {
		return 0
	}
	for _, p := range open {
		if p.ID == pr.ID {
			continue
		}
		if head, err := pushed.GetRefCommitID(p.GetGitRefName()); err == nil && head == parentSHA.String() {
			return p.ID
		}
	}
	return 0
}

// ascParentScan bounds the open stacked pull requests compared by head when the server names no parent.
const ascParentScan = 300

// ascAdoptLimit bounds how many unlinked changes one look asks asc about.
const ascAdoptLimit = 20

// adoptAscChildren links the open pull requests of the changes that sit on change, whose parent was not linked
// because their pull requests were made before this one (a stack is pushed as one batch and the order of the
// refs is not the order of the stack). A row whose parent is not this change is left alone.
func adoptAscChildren(ctx context.Context, repo *repo_model.Repository, pr *issues_model.PullRequest, change string) error {
	rows, err := issues_model.ListAscStackBottoms(ctx, repo.ID)
	if err != nil {
		return err
	}
	rows = rows[:min(len(rows), ascAdoptLimit)]
	for _, row := range rows {
		if row.PullID == pr.ID || row.AscChange == change {
			continue
		}
		child, err := issues_model.GetPullRequestByID(ctx, row.PullID)
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				continue
			}
			return err
		}
		if err := child.LoadIssue(ctx); err != nil || child.Issue.IsClosed || child.HasMerged {
			continue
		}
		parent, err := ascParentOf(ctx, repo, row.AscChange)
		if err != nil || parent != change {
			continue
		}
		if err := issues_model.SetAscStackParent(ctx, child.ID, pr.ID); err != nil {
			return err
		}
		if err := pull_service.RecomputeAscStackBase(ctx, child); err != nil {
			return err
		}
	}
	return nil
}

// ascDeniedMessage is what the pusher sees when the update authorisation fails.
func ascDeniedMessage(change string) string {
	return "not allowed to update change " + shortChange(change)
}

func logAscStack(err error, change string) {
	log.Error("asc stack for change %s: %v", shortChange(change), err)
}

// linkAscStackedPushes links the parents of the stacked pull requests a push made or moved from git: the parent of a
// change is the open stacked pull request whose head is the git parent of its head. asc's own answer
// (ascParentOf) is missing while the hook runs for a batch, and the refs of a batch come in any order, so this runs
// once after every ref of the push has its pull request. A failure is logged: the pull requests exist, and the
// stack page and the merge look again.
func linkAscStackedPushes(ctx context.Context, repo *repo_model.Repository, bridge *git.Repository, heads map[int64]string) {
	if len(heads) == 0 {
		return
	}
	if _, err := pull_service.LinkAscStackPush(ctx, bridge, heads); err != nil {
		log.Error("asc stack: linking the pull requests of a push to %s: %v", repo.FullName(), err)
	}
}

func init() {
	// asc's answer for the parent change confirms a link that git suggests between pull requests of two people
	pull_service.AscParentChange = func(ctx context.Context, repo *repo_model.Repository, change string) (string, error) {
		return ascParentOf(ctx, repo, change)
	}
}
