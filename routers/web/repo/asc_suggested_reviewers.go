// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"strings"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	issue_service "forgejo.org/services/issue"
)

// Suggested reviewers of a stacked change (Review.SuggestReviewers): the people who reviewed most in this repository and who opened
// threads on the files the change touches. The suggestion is advice; a person is only offered when the hub lets them be asked
// (they are in the list of reviewers the hub itself offers), and asking is the hub's own review request.

const ascSuggestedReviewersLimit = 5

// AscSuggestedReviewer is a person asc suggests and the hub offers.
type AscSuggestedReviewer struct {
	User        *user_model.User
	Reviews     uint32
	OnSameFiles uint32
	Reason      string
}

// ascSuggestedReviewersOf keeps the suggestions that are people the hub offers and that are not asked yet, in asc's order.
func ascSuggestedReviewersOf(resp *ascv1.SuggestReviewersResponse, offered []*repoReviewerSelection) []AscSuggestedReviewer {
	byName := make(map[string]*repoReviewerSelection, len(offered))
	for _, o := range offered {
		if o != nil && o.User != nil && !o.IsTeam {
			byName[strings.ToLower(o.User.Name)] = o
		}
	}
	var out []AscSuggestedReviewer
	for _, s := range resp.GetReviewers() {
		o, ok := byName[strings.ToLower(s.GetHandle())]
		if !ok || o.Checked || s.GetIsAgent() {
			continue
		}
		out = append(out, AscSuggestedReviewer{User: o.User, Reviews: s.GetReviews(), OnSameFiles: s.GetOnSameFiles(), Reason: asctrust.CleanText(s.GetReason(), 200)})
		if len(out) >= ascSuggestedReviewersLimit {
			break
		}
	}
	return out
}

// prepareAscSuggestedReviewers puts the suggestions into ctx.Data["AscSuggestedReviewers"]. It never fails the page.
func prepareAscSuggestedReviewers(ctx *context.Context, issue *issues_model.Issue, offered []*repoReviewerSelection) {
	if !setting.Asc.Enabled || !issue.IsPull || len(offered) == 0 || issue.IsClosed {
		return
	}
	if err := issue.LoadPullRequest(ctx); err != nil || issue.PullRequest.HasMerged {
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, issue.PullRequest.ID)
	repo := ctx.Repo.Repository
	if err != nil || link == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	name, err := ascGraphRepoName(repo)
	change := ascChangeBytes(link.AscChange)
	if err != nil || change == nil {
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).SuggestReviewers(callCtx, &ascv1.SuggestReviewersRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Limit: ascSuggestedReviewersLimit + 5})
	if err != nil {
		log.Warn("asc suggested reviewers: %v", err)
		return
	}
	if list := ascSuggestedReviewersOf(resp, offered); len(list) > 0 {
		ctx.Data["AscSuggestedReviewers"] = list
	}
}

// AscRequestSuggestedReviewer handles POST /{owner}/{repo}/pulls/{index}/asc/reviewer with the form field "user" (the id of
// the person): the hub's own review request, with the hub's own checks, and a redirect back to the pull request.
func AscRequestSuggestedReviewer(ctx *context.Context) {
	if !ctx.IsSigned {
		ctx.Error(http.StatusForbidden)
		return
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	if err := issue.LoadRepo(ctx); err != nil {
		ctx.ServerError("LoadRepo", err)
		return
	}
	reviewer, err := user_model.GetUserByID(ctx, ctx.FormInt64("user"))
	if err != nil {
		ctx.NotFound("GetUserByID", err)
		return
	}
	if err := issue_service.IsValidReviewRequest(ctx, reviewer, ctx.Doer, true, issue); err != nil {
		if issues_model.IsErrNotValidReviewRequest(err) {
			ctx.Error(http.StatusForbidden)
			return
		}
		ctx.ServerError("IsValidReviewRequest", err)
		return
	}
	if _, err := issue_service.ReviewRequest(ctx, issue, ctx.Doer, reviewer, true); err != nil {
		if issues_model.IsErrReviewRequestOnClosedPR(err) {
			ctx.Error(http.StatusForbidden)
			return
		}
		ctx.ServerError("ReviewRequest", err)
		return
	}
	ctx.Redirect(issue.Link())
}
