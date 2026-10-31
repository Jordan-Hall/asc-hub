// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/references"
)

// The stacks that point at an issue. A pull request that names an issue in its text ("fixes #12", "see #12") leaves a
// reference comment on that issue; when the pull request is a change of a stack, the stack is linked to the issue by it.
// Nothing is stored for the link: it is read from those comments, so it follows the text of the pull request.

// maxIssueStacks bounds the stacks one issue lists.
const maxIssueStacks = 10

// AscIssueStack is one stack that points at an issue.
type AscIssueStack struct {
	Entries []AscStackEntry // bottom to top
	Landed  int             // entries merged
	// Closes says that a pull request of the stack names the issue with a closing word ("fixes #12").
	Closes bool
	// Top is the entry to link to: the pull request that names the issue.
	Top AscStackEntry
}

// Done reports whether every entry of the stack has landed.
func (s *AscIssueStack) Done() bool { return len(s.Entries) > 0 && s.Landed == len(s.Entries) }

// referencingPulls maps issue ids to the pull request issue ids that name them, with whether any of them closes.
func referencingPulls(ctx context.Context, issueIDs []int64) (map[int64][]int64, map[[2]int64]bool, error) {
	if len(issueIDs) == 0 {
		return nil, nil, nil
	}
	var comments []*issues_model.Comment
	err := db.GetEngine(ctx).
		Where("type = ? AND ref_is_pull = ?", issues_model.CommentTypePullRef, true).
		In("issue_id", issueIDs).
		Asc("id").
		Cols("issue_id", "ref_issue_id", "ref_action").
		Find(&comments)
	if err != nil {
		return nil, nil, err
	}
	pulls := map[int64][]int64{}
	closes := map[[2]int64]bool{}
	seen := map[[2]int64]bool{}
	for _, c := range comments {
		key := [2]int64{c.IssueID, c.RefIssueID}
		if c.RefIssueID != 0 && !seen[key] {
			seen[key] = true
			pulls[c.IssueID] = append(pulls[c.IssueID], c.RefIssueID)
		}
		if c.RefAction == references.XRefActionCloses {
			closes[key] = true
		}
	}
	return pulls, closes, nil
}

// AscStacksOfIssue returns the stacks that name the issue, one per stack (the pull requests of one stack are one entry), the
// newest reference first. A reference by a pull request that is not a change of a stack is not listed here.
func AscStacksOfIssue(ctx context.Context, issue *issues_model.Issue) ([]AscIssueStack, error) {
	pulls, closes, err := referencingPulls(ctx, []int64{issue.ID})
	if err != nil {
		return nil, err
	}
	return stacksFor(ctx, issue.ID, pulls[issue.ID], closes)
}

func stacksFor(ctx context.Context, issueID int64, prIssueIDs []int64, closes map[[2]int64]bool) ([]AscIssueStack, error) {
	var out []AscIssueStack
	seenStack := map[int64]bool{} // by the index of the bottom entry
	for i := len(prIssueIDs) - 1; i >= 0 && len(out) < maxIssueStacks; i-- {
		pr, err := issues_model.GetPullRequestByIssueID(ctx, prIssueIDs[i])
		if err != nil {
			if issues_model.IsErrPullRequestNotExist(err) {
				continue
			}
			return nil, err
		}
		panel, err := BuildAscStackPanel(ctx, pr)
		if err != nil || panel == nil || len(panel.Entries) == 0 {
			if err != nil {
				return nil, err
			}
			continue
		}
		bottom := panel.Entries[0].Index
		if seenStack[bottom] {
			if closes[[2]int64{issueID, prIssueIDs[i]}] {
				for j := range out {
					if out[j].Entries[0].Index == bottom {
						out[j].Closes = true
					}
				}
			}
			continue
		}
		seenStack[bottom] = true
		st := AscIssueStack{Entries: panel.Entries, Closes: closes[[2]int64{issueID, prIssueIDs[i]}]}
		for _, e := range panel.Entries {
			if e.State == AscStackStateMerged {
				st.Landed++
			}
			if e.Current {
				st.Top = e
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// AscClosingStackLanded returns the stack that closed the issue: the issue is closed, a pull request of a stack names it with a
// closing word, and that pull request has merged. nil when there is none.
func AscClosingStackLanded(ctx context.Context, issue *issues_model.Issue, stacks []AscIssueStack) *AscIssueStack {
	if !issue.IsClosed {
		return nil
	}
	for i := range stacks {
		if stacks[i].Closes && stacks[i].Top.State == AscStackStateMerged {
			return &stacks[i]
		}
	}
	return nil
}

// AscIssueStackChips gives, for the issues of a list, the one stack to show on the row: the stack that fixes the issue if any,
// else the newest that names it. One query reads the references of the whole list.
func AscIssueStackChips(ctx context.Context, list issues_model.IssueList) (map[int64]AscIssueStack, error) {
	ids := make([]int64, 0, len(list))
	for _, is := range list {
		if !is.IsPull {
			ids = append(ids, is.ID)
		}
	}
	pulls, closes, err := referencingPulls(ctx, ids)
	if err != nil || len(pulls) == 0 {
		return nil, err
	}
	out := map[int64]AscIssueStack{}
	for issueID, prs := range pulls {
		stacks, err := stacksFor(ctx, issueID, prs, closes)
		if err != nil {
			return nil, err
		}
		pick := -1
		for i := range stacks {
			if stacks[i].Closes {
				pick = i
				break
			}
		}
		if pick < 0 && len(stacks) > 0 {
			pick = 0
		}
		if pick >= 0 {
			out[issueID] = stacks[pick]
		}
	}
	return out, nil
}
