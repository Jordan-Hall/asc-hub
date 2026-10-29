// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"
	"fmt"
	"regexp"

	"forgejo.org/models/db"
)

// A stacked pull request (SPEC section 20.5, PLAN unit C34) is the pull request of one asc change. The facts
// Forgejo keeps about that link live here, in a table of their own beside pull_request: asc_change is the
// change id (32 letters k-z), and StackParentID is the pull request of the parent change, a cache for listing
// (asc's Changes.Stack is the truth).

// AscChangePattern is the text form of an asc change id: 32 letters from k to z.
const AscChangePattern = `[k-z]{32}`

var ascChangeRe = regexp.MustCompile(`^` + AscChangePattern + `$`)

// IsAscChange reports whether s is the text form of an asc change id.
func IsAscChange(s string) bool { return ascChangeRe.MatchString(s) }

// PullRequestAscStack links a pull request to its asc change and to the pull request of the parent change.
type PullRequestAscStack struct {
	ID            int64  `xorm:"pk autoincr"`
	PullID        int64  `xorm:"UNIQUE NOT NULL"`
	RepoID        int64  `xorm:"INDEX(repo_change) NOT NULL"`
	AscChange     string `xorm:"VARCHAR(32) INDEX(repo_change) NOT NULL"`
	StackParentID int64  `xorm:"INDEX NOT NULL DEFAULT 0"` // pull_request.id of the parent change's PR; 0 for the bottom
}

// TableName gives the table its name.
func (*PullRequestAscStack) TableName() string { return "pull_request_asc_stack" }

func init() {
	db.RegisterModel(new(PullRequestAscStack))
}

// GetPullRequestAscStack returns the stack row of the pull request with the given id, or nil when the pull
// request is not a stacked one.
func GetPullRequestAscStack(ctx context.Context, pullID int64) (*PullRequestAscStack, error) {
	s := new(PullRequestAscStack)
	has, err := db.GetEngine(ctx).Where("pull_id = ?", pullID).Get(s)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	return s, nil
}

// GetOpenPullRequestByAscChange returns the open pull request of change in repo (the base repository), or an
// ErrPullRequestNotExist. A merged or closed pull request of the change is not returned: submitting again
// opens a new one. The pull request is found by (repo, change), never by (topic, user).
func GetOpenPullRequestByAscChange(ctx context.Context, repoID int64, change string) (*PullRequest, error) {
	if !IsAscChange(change) {
		return nil, fmt.Errorf("not an asc change id")
	}
	pr := new(PullRequest)
	has, err := db.GetEngine(ctx).
		Table("pull_request").
		Join("INNER", "pull_request_asc_stack", "pull_request_asc_stack.pull_id = pull_request.id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where("pull_request_asc_stack.repo_id = ? AND pull_request_asc_stack.asc_change = ? AND pull_request.has_merged = ? AND issue.is_closed = ?",
			repoID, change, false, false).
		OrderBy("pull_request.id DESC").
		Get(pr)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrPullRequestNotExist{0, 0, repoID, repoID, "", ""}
	}
	return pr, nil
}

// ListOpenAscPullRequests returns the open, stacked pull requests of repoID, newest first, at most limit.
func ListOpenAscPullRequests(ctx context.Context, repoID int64, limit int) ([]*PullRequest, error) {
	var prs []*PullRequest
	err := db.GetEngine(ctx).
		Table("pull_request").
		Join("INNER", "pull_request_asc_stack", "pull_request_asc_stack.pull_id = pull_request.id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where("pull_request_asc_stack.repo_id = ? AND pull_request.has_merged = ? AND issue.is_closed = ?", repoID, false, false).
		OrderBy("pull_request.id DESC").
		Limit(limit).
		Find(&prs)
	return prs, err
}

// SetPullRequestAscStack records (or updates) the link of pr to change and to the pull request stackParentID
// (0 for the bottom of a stack). A pull request has one change: relinking it to another is refused.
func SetPullRequestAscStack(ctx context.Context, pr *PullRequest, change string, stackParentID int64) error {
	if !IsAscChange(change) {
		return fmt.Errorf("not an asc change id")
	}
	if pr.ID == 0 || stackParentID == pr.ID || stackParentID < 0 {
		return fmt.Errorf("bad stack link")
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		cur, err := GetPullRequestAscStack(ctx, pr.ID)
		if err != nil {
			return err
		}
		if cur == nil {
			_, err = e.Insert(&PullRequestAscStack{PullID: pr.ID, RepoID: pr.BaseRepoID, AscChange: change, StackParentID: stackParentID})
			return err
		}
		if cur.AscChange != change {
			return fmt.Errorf("pull request is linked to another change")
		}
		_, err = e.ID(cur.ID).Cols("stack_parent_id").Update(&PullRequestAscStack{StackParentID: stackParentID})
		return err
	})
}

// GetAscStackChildren returns the stack rows whose parent is the pull request parentID.
func GetAscStackChildren(ctx context.Context, parentID int64) ([]*PullRequestAscStack, error) {
	var rows []*PullRequestAscStack
	return rows, db.GetEngine(ctx).Where("stack_parent_id = ?", parentID).OrderBy("id").Find(&rows)
}

// ListAscStackBottoms returns the stack rows of repoID that have no parent pull request: the bottoms of the stacks,
// and the changes whose parent has not been linked (yet).
func ListAscStackBottoms(ctx context.Context, repoID int64) ([]*PullRequestAscStack, error) {
	var rows []*PullRequestAscStack
	err := db.GetEngine(ctx).Where("repo_id = ? AND stack_parent_id = 0", repoID).OrderBy("id").Find(&rows)
	return rows, err
}

// SetAscStackParent sets the parent pull request of the stack row of pullID.
func SetAscStackParent(ctx context.Context, pullID, parentID int64) error {
	_, err := db.GetEngine(ctx).Where("pull_id = ?", pullID).Cols("stack_parent_id").Update(&PullRequestAscStack{StackParentID: parentID})
	return err
}
