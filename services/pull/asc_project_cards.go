// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	project_model "forgejo.org/models/project"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/references"
	notify_service "forgejo.org/services/notify"
)

// Stacks on project boards. A card of a pull request that is a change of a stack shows the stack and how much of it has
// landed; a card of an issue shows the stack that names it (the one that fixes it first), as the issue list does. Both
// are read from the stack rows when the board is drawn, so a card follows the real state of its stack.
//
// A project may mark one column as its Landed column (project_model.SetLandedColumn). When a pull request merges and its
// stack has landed whole (a pull request that is not stacked is a stack of one), the cards of the pull requests of the
// stack and of the issues they close move to the end of the Landed column of every project they are on.

// AscProjectCardStacks gives, for the cards of a board, the stack to show on each (by issue id). Only stacks of
// repositories the viewer may read pull requests in are given.
func AscProjectCardStacks(ctx context.Context, viewer *user_model.User, cards map[int64]issues_model.IssueList) (map[int64]AscIssueStack, error) {
	var issues issues_model.IssueList
	out := map[int64]AscIssueStack{}
	for _, list := range cards {
		for _, card := range list {
			if !card.IsPull {
				issues = append(issues, card)
				continue
			}
			st, err := ascPullCardStack(ctx, viewer, card)
			if err != nil {
				return nil, err
			}
			if st != nil {
				out[card.ID] = *st
			}
		}
	}
	chips, err := AscIssueStackChips(ctx, viewer, issues)
	if err != nil {
		return nil, err
	}
	for id, st := range chips {
		out[id] = st
	}
	return out, nil
}

// ascPullCardStack is the stack of the card of a pull request, with Top the pull request itself; nil when it is not a
// change of a stack or the viewer may not read it.
func ascPullCardStack(ctx context.Context, viewer *user_model.User, card *issues_model.Issue) (*AscIssueStack, error) {
	pr, err := issues_model.GetPullRequestByIssueID(ctx, card.ID)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if ok, err := viewerMayReadPull(ctx, viewer, pr); err != nil || !ok {
		return nil, err
	}
	panel, err := BuildAscStackPanel(ctx, pr)
	if err != nil || panel == nil || len(panel.Entries) == 0 {
		return nil, err
	}
	st := &AscIssueStack{Entries: panel.Entries}
	for _, e := range panel.Entries {
		if e.State == AscStackStateMerged {
			st.Landed++
		}
		if e.Current {
			st.Top = e
		}
	}
	return st, nil
}

// ascLandedIssueIDs returns the issue ids whose cards go to the Landed column once pr has merged: none while its stack
// has an entry that has not merged, else the issues of the pull requests of the stack, then the issues they close.
func ascLandedIssueIDs(ctx context.Context, pr *issues_model.PullRequest) ([]int64, error) {
	if !pr.HasMerged {
		return nil, nil
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return nil, err
	}
	pulls := []int64{pr.IssueID}
	panel, err := BuildAscStackPanel(ctx, pr)
	if err != nil {
		return nil, err
	}
	if panel != nil && len(panel.Entries) > 0 {
		pulls = pulls[:0]
		for _, e := range panel.Entries {
			if e.State != AscStackStateMerged {
				return nil, nil
			}
			issue, err := issues_model.GetIssueByIndex(ctx, pr.BaseRepoID, e.Index)
			if err != nil {
				return nil, err
			}
			pulls = append(pulls, issue.ID)
		}
	}
	var closed []int64
	if err := db.GetEngine(ctx).Table("comment").
		Where("type = ? AND ref_is_pull = ? AND ref_action = ?", issues_model.CommentTypePullRef, true, references.XRefActionCloses).
		In("ref_issue_id", pulls).Asc("id").Cols("issue_id").Find(&closed); err != nil {
		return nil, err
	}
	out := pulls
	seen := map[int64]bool{}
	for _, id := range pulls {
		seen[id] = true
	}
	for _, id := range closed {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// MoveAscLandedCards moves the cards of a stack that has landed whole, and of the issues it closes, to the Landed
// column of each project they are on (see the comment at the top). It returns the number of cards moved.
func MoveAscLandedCards(ctx context.Context, pr *issues_model.PullRequest) (int, error) {
	ids, err := ascLandedIssueIDs(ctx, pr)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	var cards []project_model.ProjectIssue
	if err := db.GetEngine(ctx).In("issue_id", ids).Find(&cards); err != nil {
		return 0, err
	}
	byProject := map[int64]bool{}
	moved := 0
	for _, c := range cards {
		if byProject[c.ProjectID] {
			continue
		}
		byProject[c.ProjectID] = true
		column, err := project_model.GetLandedColumn(ctx, c.ProjectID)
		if err != nil {
			return moved, err
		}
		if column == nil {
			continue
		}
		done, err := project_model.MoveCardsToColumnEnd(ctx, column, ids)
		if err != nil {
			return moved, err
		}
		moved += len(done)
	}
	return moved, nil
}

type ascLandedCardsNotifier struct {
	notify_service.NullNotifier
}

var _ notify_service.Notifier = (*ascLandedCardsNotifier)(nil)

func init() {
	notify_service.RegisterNotifier(&ascLandedCardsNotifier{})
}

func moveAscLandedCardsLogged(ctx context.Context, pr *issues_model.PullRequest) {
	if _, err := MoveAscLandedCards(ctx, pr); err != nil {
		log.Error("asc landed cards of %-v: %v", pr, err)
	}
}

func (*ascLandedCardsNotifier) MergePullRequest(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	moveAscLandedCardsLogged(ctx, pr)
}

func (*ascLandedCardsNotifier) AutoMergePullRequest(ctx context.Context, _ *user_model.User, pr *issues_model.PullRequest) {
	moveAscLandedCardsLogged(ctx, pr)
}
