// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"fmt"

	issues_model "forgejo.org/models/issues"
)

// The stack panel, the needs-restack banner and the mid-stack merge block of a stacked pull request
// (SPEC section 20.5 and the seams of section 20.6, PLAN unit C36).
//
// The facts come from pull_request_asc_stack (C34). asc's Changes.Stack is the truth for a stack; Forgejo's
// rows are the cache it keeps for listing. A pull request is merge-blocked while the pull request of its
// parent change has not merged: the parent must land first ("Merge stack (N)" is C38). The block fails
// closed: a database error is an error, never a permission to merge.
//
// The "Restack" button of the needs-restack banner is shown by the router (routers/web/repo/asc_stack_actions.go) and
// runs pull_service.RestackAscStack (asc_restack.go).

// ascStackMaxDepth bounds the walk along a chain of stacked pull requests, so a corrupt cycle ends.
const ascStackMaxDepth = 64

// The states a stack entry shows.
const (
	AscStackStateOpen   = "open"
	AscStackStateMerged = "merged"
	AscStackStateClosed = "closed"
)

// AscStackEntry is one pull request in the stack panel.
type AscStackEntry struct {
	Index    int64
	Title    string
	State    string
	Link     string
	Position int // 1 for the bottom
	Current  bool
}

// AscStackPanel is what the pull request page shows for a stacked pull request.
type AscStackPanel struct {
	Entries []AscStackEntry // bottom to top
	// NeedsRestack is true when the parent change has landed (or its pull request was closed unmerged) and
	// this pull request is still open: its commits still sit on the old parent.
	NeedsRestack bool
	// MergeBlocked is true while the parent pull request has not merged. Parent is its entry.
	MergeBlocked bool
	Parent       *AscStackEntry
}

// AscStackParentBlocking returns the pull request that blocks pr from merging because it is the unmerged
// pull request of pr's parent change, or nil when nothing blocks (not stacked, bottom of a stack, parent
// merged or gone).
func AscStackParentBlocking(ctx context.Context, pr *issues_model.PullRequest) (*issues_model.PullRequest, error) {
	if pr == nil || pr.ID == 0 {
		return nil, nil
	}
	stack, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil {
		return nil, err
	}
	if stack == nil || stack.StackParentID == 0 {
		return nil, nil
	}
	parent, err := issues_model.GetPullRequestByID(ctx, stack.StackParentID)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if parent.HasMerged {
		return nil, nil
	}
	return parent, nil
}

func ascStackEntry(ctx context.Context, pr *issues_model.PullRequest) (AscStackEntry, error) {
	if err := pr.LoadIssue(ctx); err != nil {
		return AscStackEntry{}, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return AscStackEntry{}, err
	}
	state := AscStackStateOpen
	switch {
	case pr.HasMerged:
		state = AscStackStateMerged
	case pr.Issue.IsClosed:
		state = AscStackStateClosed
	}
	return AscStackEntry{
		Index: pr.Issue.Index,
		Title: pr.Issue.Title,
		State: state,
		Link:  fmt.Sprintf("%s/pulls/%d", pr.BaseRepo.Link(), pr.Issue.Index),
	}, nil
}

// BuildAscStackPanel returns the panel for pr, or nil when pr is not a stacked pull request. Only pull
// requests of pr's own base repository are listed, so the panel shows nothing the viewer could not read.
func BuildAscStackPanel(ctx context.Context, pr *issues_model.PullRequest) (*AscStackPanel, error) {
	if pr == nil || pr.ID == 0 {
		return nil, nil
	}
	own, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil || own == nil {
		return nil, err
	}
	seen := map[int64]bool{pr.ID: true}
	chain := []*issues_model.PullRequest{pr}

	// Down to the bottom.
	row := own
	for range ascStackMaxDepth {
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
		if seen[parent.ID] || parent.BaseRepoID != pr.BaseRepoID {
			break
		}
		seen[parent.ID] = true
		chain = append([]*issues_model.PullRequest{parent}, chain...)
		if row, err = issues_model.GetPullRequestAscStack(ctx, parent.ID); err != nil {
			return nil, err
		}
		if row == nil {
			break
		}
	}

	// Up to the top, along the oldest child at each step.
	cur := pr
	for range ascStackMaxDepth {
		kids, err := issues_model.GetAscStackChildren(ctx, cur.ID)
		if err != nil {
			return nil, err
		}
		var next *issues_model.PullRequest
		for _, k := range kids {
			c, err := issues_model.GetPullRequestByID(ctx, k.PullID)
			if err != nil {
				if issues_model.IsErrPullRequestNotExist(err) {
					continue
				}
				return nil, err
			}
			if !seen[c.ID] && c.BaseRepoID == pr.BaseRepoID {
				next = c
				break
			}
		}
		if next == nil {
			break
		}
		seen[next.ID] = true
		chain = append(chain, next)
		cur = next
	}

	panel := &AscStackPanel{}
	for i, p := range chain {
		e, err := ascStackEntry(ctx, p)
		if err != nil {
			return nil, err
		}
		e.Position = i + 1
		e.Current = p.ID == pr.ID
		panel.Entries = append(panel.Entries, e)
	}

	if own.StackParentID != 0 {
		parent, err := issues_model.GetPullRequestByID(ctx, own.StackParentID)
		if err != nil && !issues_model.IsErrPullRequestNotExist(err) {
			return nil, err
		}
		if err == nil {
			pe, err := ascStackEntry(ctx, parent)
			if err != nil {
				return nil, err
			}
			panel.Parent = &pe
			panel.MergeBlocked = !parent.HasMerged
			if err := pr.LoadIssue(ctx); err != nil {
				return nil, err
			}
			// Landed, or closed without landing (obsolete): the child still sits on the old parent.
			panel.NeedsRestack = !pr.HasMerged && !pr.Issue.IsClosed &&
				(parent.HasMerged || pe.State == AscStackStateClosed)
		}
	}
	return panel, nil
}
