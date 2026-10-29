// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"strings"

	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The facts strip, intent, stack position, review block and "where it is now" of the change page (board Changeset).
// Intent, size, files, checks and where the change lives come from the commit and need the code unit, which the
// route has already checked. The stack position and the review block come from the pull request the change landed
// through, so they are read only for a viewer who may read pull requests (a person with code access alone sees
// neither the name of a stack nor who approved).

// changeIntentOf is the body of a commit message without its trailers (Signed-off-by, Co-authored-by and the like):
// the reason the change exists. The first line is the title and is shown by the header.
func changeIntentOf(message string) string {
	_, body, ok := strings.Cut(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	if !ok {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	// drop the trailer block at the end: lines of the form "Token: value" after the last blank line
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	start := end
	for start > 0 && isTrailerLine(lines[start-1]) {
		start--
	}
	if start < end && (start == 0 || strings.TrimSpace(lines[start-1]) == "") {
		end = start
	}
	return strings.TrimSpace(strings.Join(lines[:end], "\n"))
}

// isTrailerLine reports whether line looks like a git trailer: a token of letters, digits and dashes, a colon and a value.
func isTrailerLine(line string) bool {
	k, v, ok := strings.Cut(line, ":")
	if !ok || strings.TrimSpace(v) == "" || k == "" || strings.ContainsAny(k, " \t") {
		return false
	}
	for _, r := range k {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// changeStackPos is where the change sits in its stack.
type changeStackPos struct {
	Name     string
	Position int
	Total    int
	Prev     *pull_service.AscStackEntry
	Next     *pull_service.AscStackEntry
	PullLink string
}

// changeApproval is one approval of the pull request the change landed through.
type changeApproval struct {
	User *user_model.User
	When int64
}

// changeReview is the review block: approvals of the landed pull request against what its branch rule asked for.
type changeReview struct {
	Approvals []changeApproval
	Required  int
}

// changeWhere is "where it is now": the default branch if it holds the change, the releases (tags) that contain it.
type changeWhere struct {
	OnDefault bool
	Branch    string
	Tags      []string
}

const changeMaxTags = 5

// prepareAscChange adds the facts of a change page. Each part that cannot be read is left out; none of it blocks the page.
func prepareAscChange(ctx *context.Context, commit *git.Commit, statuses []*git_model.CommitStatus, additions, deletions, files int) {
	commitID := commit.ID.String()
	ctx.Data["ChangeIntent"] = changeIntentOf(commit.Message())
	pass, total := relChecks(statuses)
	ctx.Data["ChangeFacts"] = &struct {
		Additions, Deletions, Files, ChecksPass, ChecksTotal int
	}{additions, deletions, files, pass, total}

	repo := ctx.Repo.Repository
	if ctx.Repo.GitRepo != nil && !repo.IsEmpty {
		w := &changeWhere{Branch: repo.DefaultBranch}
		if ok, err := ctx.Repo.GitRepo.IsCommitInBranch(commitID, repo.DefaultBranch); err == nil {
			w.OnDefault = ok
		}
		if tags, err := ctx.Repo.GitRepo.ListOccurrences(ctx, "tag", commitID); err == nil {
			for _, t := range tags {
				if t != "" && len(w.Tags) < changeMaxTags {
					w.Tags = append(w.Tags, t)
				}
			}
		}
		ctx.Data["ChangeWhere"] = w
	}

	if !ctx.Repo.CanRead(unit.TypePullRequests) || !repo.CanEnablePulls() {
		return
	}
	pr, err := issues_model.GetPullRequestByMergedCommit(ctx, repo.ID, commitID)
	if err != nil || pr == nil {
		return
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return
	}
	ctx.Data["ChangePull"] = fmt.Sprintf("%s/pulls/%d", ctx.Repo.RepoLink, pr.Index)

	if panel, err := pull_service.BuildAscStackPanel(ctx, pr); err != nil {
		log.Error("change page: stack of %s#%d: %v", repo.FullName(), pr.Index, err)
	} else if panel != nil {
		pos := &changeStackPos{Name: pr.HeadBranch, Total: len(panel.Entries), PullLink: fmt.Sprintf("%s/pulls/%d", ctx.Repo.RepoLink, pr.Index)}
		for i := range panel.Entries {
			e := panel.Entries[i]
			if e.Current {
				pos.Position = e.Position
				if i > 0 {
					pos.Prev = &panel.Entries[i-1]
				}
				if i+1 < len(panel.Entries) {
					pos.Next = &panel.Entries[i+1]
				}
			}
		}
		ctx.Data["ChangeStack"] = pos
	}

	reviews, err := issues_model.FindLatestReviews(ctx, issues_model.FindReviewOptions{
		IssueID: pr.IssueID, Dismissed: optional.Some(false),
		Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
	})
	if err != nil {
		return
	}
	// Only the latest approving or rejecting review of each person counts, and a dismissed one does not.
	rv := &changeReview{}
	if pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, repo.ID, pr.BaseBranch); err == nil && pb != nil {
		rv.Required = int(pb.RequiredApprovals)
	}
	if err := reviews.LoadReviewers(ctx); err != nil {
		return
	}
	for _, r := range reviews {
		if r.Type != issues_model.ReviewTypeApprove || r.Reviewer == nil {
			continue
		}
		rv.Approvals = append(rv.Approvals, changeApproval{User: r.Reviewer, When: int64(r.CreatedUnix)})
	}
	if len(rv.Approvals) > 0 || rv.Required > 0 {
		ctx.Data["ChangeReview"] = rv
	}
}
