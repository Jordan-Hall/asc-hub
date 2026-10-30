// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"slices"
	"strconv"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
)

// The "Merge a stack" panel of the queue page: the open stacks that land on the branch, each with the two ways to land it. Both
// go to the confirmation page of the top change, where the scope (how much of the stack) and the dry run are chosen, so the panel
// decides nothing and every land is checked again there.

const (
	ascQueueStacksLimit = 50 // open stacks listed
	ascQueueChainLimit  = 64 // changes counted in one stack
)

// ascQueueStack is one open stack of the panel.
type ascQueueStack struct {
	Top      *issues_model.PullRequest
	Changes  int
	Queued   bool
	QueueURL string
	MergeURL string
}

// ascQueueStacksOf lists the open stacks of repo whose changes land on branch, newest first. A stack is named by its top change:
// the open change that no other open change is stacked on.
func ascQueueStacksOf(ctx context.Context, repo *repo_model.Repository, branch string, queued map[int64]bool) ([]ascQueueStack, error) {
	open, err := issues_model.ListOpenAscPullRequests(ctx, repo.ID, 500)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*issues_model.PullRequest, len(open))
	for _, pr := range open {
		byID[pr.ID] = pr
	}
	parent := make(map[int64]int64, len(open))
	hasChild := map[int64]bool{}
	for _, pr := range open {
		link, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		if link != nil && byID[link.StackParentID] != nil {
			parent[pr.ID] = link.StackParentID
			hasChild[link.StackParentID] = true
		}
	}
	var out []ascQueueStack
	for _, pr := range open {
		if hasChild[pr.ID] || pr.BaseBranch != branch {
			continue
		}
		n := 1
		for id := pr.ID; n < ascQueueChainLimit && parent[id] != 0; n++ {
			id = parent[id]
		}
		if err := pr.LoadIssue(ctx); err != nil {
			return nil, err
		}
		link := repo.Link() + "/pulls/" + strconv.FormatInt(pr.Issue.Index, 10) + "/asc/confirm?op="
		out = append(out, ascQueueStack{Top: pr, Changes: n, Queued: queued[pr.ID], QueueURL: link + "queue", MergeURL: link + "merge"})
		if len(out) >= ascQueueStacksLimit {
			break
		}
	}
	slices.SortStableFunc(out, func(a, b ascQueueStack) int { return int(b.Top.ID - a.Top.ID) })
	return out, nil
}
