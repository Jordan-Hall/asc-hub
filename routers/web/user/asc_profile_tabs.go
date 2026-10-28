// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"slices"
	"strconv"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// The Work, Reviews and pinned parts of a person's profile (board Profile). Everything drawn from pull requests is
// limited to what the viewer may read, exactly like the overview.

const (
	ascProfileTabMax  = 100 // the most rows one of the tabs lists
	ascProfileScanTab = 400 // the most rows looked at to find them

	tplAscPinSettings base.TplName = "user/settings/pins"
)

// AscProfileReview is one pull request the person reviewed.
type AscProfileReview struct {
	Title string
	Repo  string
	Link  string
	State string // approved, changes or comment
	When  time.Time
}

// ascProfileReviews lists the pull requests user reviewed (a published review, newest first, once per pull request)
// that viewer may read.
func ascProfileReviews(ctx gocontext.Context, user, viewer *user_model.User, limit int) []AscProfileReview {
	var reviews []*issues_model.Review
	err := db.GetEngine(ctx).
		Where("reviewer_id = ? AND type != ?", user.ID, issues_model.ReviewTypePending).
		OrderBy("created_unix DESC, id DESC").Limit(ascProfileScanTab).Find(&reviews)
	if err != nil {
		log.Error("asc profile: reviews of user %d: %v", user.ID, err)
		return nil
	}
	seen := map[int64]bool{}
	var out []AscProfileReview
	for _, r := range reviews {
		if len(out) >= limit {
			break
		}
		if seen[r.IssueID] {
			continue
		}
		seen[r.IssueID] = true
		issue, err := issues_model.GetIssueByID(ctx, r.IssueID)
		if err != nil || !issue.IsPull || issue.LoadRepo(ctx) != nil || issue.LoadPullRequest(ctx) != nil {
			continue
		}
		if !ascCanReadPulls(ctx, viewer, issue.Repo) {
			continue
		}
		state := "comment"
		switch r.Type {
		case issues_model.ReviewTypeApprove:
			state = "approved"
		case issues_model.ReviewTypeReject:
			state = "changes"
		}
		out = append(out, AscProfileReview{Title: issue.Title, Repo: issue.Repo.FullName(), Link: ascPullLink(issue.PullRequest), State: state, When: r.CreatedUnix.AsTime()})
	}
	return out
}

// AscProfilePinView is one pinned repository.
type AscProfilePinView struct {
	FullName    string
	Link        string
	Description string
	Private     bool
	Changes     int64
}

// ascProfilePins lists the pins of the person that viewer may read.
func ascProfilePins(ctx gocontext.Context, person, viewer *user_model.User) []AscProfilePinView {
	ids, err := user_model.ListAscProfilePins(ctx, person.ID)
	if err != nil {
		log.Error("asc profile: pins of user %d: %v", person.ID, err)
		return nil
	}
	var out []AscProfilePinView
	for _, id := range ids {
		repo, err := repo_model.GetRepositoryByID(ctx, id)
		if err != nil {
			continue
		}
		perm, err := access.GetUserRepoPermission(ctx, repo, viewer)
		if err != nil || !(perm.CanRead(unit.TypeCode) || perm.CanRead(unit.TypePullRequests)) {
			continue
		}
		changes, _ := db.GetEngine(ctx).Table("issue").Where("repo_id = ? AND is_pull = ?", repo.ID, true).Count()
		out = append(out, AscProfilePinView{FullName: repo.FullName(), Link: repo.Link(), Description: repo.Description, Private: repo.IsPrivate, Changes: changes})
	}
	return out
}

// ascProfileTabData prepares the Work and Reviews tabs and the numbers on their labels. It does nothing for a person
// whose activity the viewer may not read.
func ascProfileTabData(ctx *context.Context, tab string) {
	person := ctx.ContextUser
	if !person.IsIndividual() {
		return
	}
	loc := ctx.Locale
	switch tab {
	case "work":
		ctx.Data["AscProfileWork"] = ascProfileWorkN(ctx, person, ctx.Doer, loc, ascProfileTabMax)
	case "reviews":
		ctx.Data["AscProfileReviews"] = ascProfileReviews(ctx, person, ctx.Doer, ascProfileTabMax)
	}
}

// ascProfileCounts puts the numbers of the Work and Reviews labels on the page.
func ascProfileCounts(ctx *context.Context) {
	person := ctx.ContextUser
	if !person.IsIndividual() || !activities_model.ActivityReadable(person, ctx.Doer) {
		return
	}
	ctx.Data["AscWorkCount"] = len(ascProfileWorkN(ctx, person, ctx.Doer, ctx.Locale, 99))
	ctx.Data["AscReviewCount"] = len(ascProfileReviews(ctx, person, ctx.Doer, 99))
}

// AscPinSettings shows the page where a person chooses the repositories pinned to their profile.
func AscPinSettings(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_work.pins_title")
	ctx.Data["PageIsSettingsProfile"] = true
	ctx.Data["Link"] = setting.AppSubURL + "/user/settings/pins"
	repos, err := ascPinCandidates(ctx, ctx.Doer)
	if err != nil {
		ctx.ServerError("ascPinCandidates", err)
		return
	}
	pinned, err := user_model.ListAscProfilePins(ctx, ctx.Doer.ID)
	if err != nil {
		ctx.ServerError("ListAscProfilePins", err)
		return
	}
	type row struct {
		ID       int64
		FullName string
		Pinned   bool
	}
	rows := make([]row, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, row{ID: r.ID, FullName: r.FullName(), Pinned: slices.Contains(pinned, r.ID)})
	}
	ctx.Data["PinRows"] = rows
	ctx.Data["PinMax"] = user_model.AscMaxProfilePins
	ctx.HTML(http.StatusOK, tplAscPinSettings)
}

// AscPinSettingsPost saves the pins: only repositories the person owns, at most six.
func AscPinSettingsPost(ctx *context.Context) {
	_ = ctx.Req.ParseForm()
	allowed, err := ascPinCandidates(ctx, ctx.Doer)
	if err != nil {
		ctx.ServerError("ascPinCandidates", err)
		return
	}
	var ids []int64
	for _, raw := range ctx.Req.PostForm["repo"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || !slices.ContainsFunc(allowed, func(r *repo_model.Repository) bool { return r.ID == id }) || slices.Contains(ids, id) {
			ctx.Error(http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	if len(ids) > user_model.AscMaxProfilePins {
		ctx.Flash.Error(ctx.Tr("asc_work.pins_too_many", user_model.AscMaxProfilePins))
		ctx.Redirect(setting.AppSubURL + "/user/settings/pins")
		return
	}
	if err := user_model.SetAscProfilePins(ctx, ctx.Doer.ID, ids); err != nil {
		ctx.ServerError("SetAscProfilePins", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("settings.update_profile_success"))
	ctx.Redirect(ctx.Doer.HomeLink())
}

// ascPinCandidates are the repositories the person can pin: the ones they own, public or not (the profile shows a
// pin only to those who may read the repository).
func ascPinCandidates(ctx gocontext.Context, person *user_model.User) ([]*repo_model.Repository, error) {
	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: 200},
		Actor:       person,
		OwnerID:     person.ID,
		Private:     true,
		OrderBy:     db.SearchOrderByAlphabetically,
	})
	return repos, err
}
