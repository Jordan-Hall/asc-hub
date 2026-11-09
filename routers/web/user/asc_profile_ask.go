// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"errors"
	"net/http"
	"strings"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
	issue_service "forgejo.org/services/issue"
)

// Request a review and Mention on the profile of a person (board Profile): the viewer picks one of their own open pull
// requests, and either asks the person to review it (a review request, which the hub also tells asc as RequestReview
// and which feeds the inbox of the person) or posts a comment on it that mentions the person. The picker is loaded
// when its dialog opens, lists only pull requests the person may read, and every action is checked again on POST.

const (
	// ascAskPullsMax is the most pull requests the picker lists, the most recently updated first.
	ascAskPullsMax = 20
	// ascAskMentionMax is the longest comment Mention posts.
	ascAskMentionMax = 64 << 10

	tplAscProfileAsk base.TplName = "user/asc_profile_ask"
)

// AscAskPull is a pull request of the picker.
type AscAskPull struct {
	ID    int64
	Index int64
	Title string
	Repo  string
}

// ascAskCan says whether doer may ask person about issue: the person may read its pull requests and, for a review,
// doer may request their review. A blocked doer, a closed or locked pull request and an archived repository refuse.
func ascAskCan(ctx gocontext.Context, doer, person *user_model.User, issue *issues_model.Issue, review bool) bool {
	if !issue.IsPull || issue.IsClosed || doer.ID == person.ID || person.IsOrganization() {
		return false
	}
	if err := issue.LoadRepo(ctx); err != nil || issue.Repo.IsArchived {
		return false
	}
	if user_model.IsBlocked(ctx, person.ID, doer.ID) {
		return false
	}
	perm, err := access_model.GetUserRepoPermission(ctx, issue.Repo, doer)
	if err != nil || !perm.CanRead(unit.TypePullRequests) {
		return false
	}
	if issue.IsLocked && !perm.CanWriteIssuesOrPulls(true) && !doer.IsAdmin {
		return false
	}
	if !ascCanReadPulls(ctx, person, issue.Repo) {
		return false
	}
	if review {
		return issue_service.IsValidReviewRequest(ctx, person, doer, true, issue) == nil
	}
	return true
}

// ascAskPulls lists the open pull requests of doer that doer may ask person about.
func ascAskPulls(ctx gocontext.Context, doer, person *user_model.User, review bool) ([]AscAskPull, error) {
	issues, err := issues_model.Issues(ctx, &issues_model.IssuesOptions{
		Paginator: &db.ListOptions{Page: 1, PageSize: ascAskPullsMax},
		PosterID:  doer.ID,
		IsPull:    optional.Some(true),
		IsClosed:  optional.Some(false),
		SortType:  "recentupdate",
	})
	if err != nil {
		return nil, err
	}
	var out []AscAskPull
	for _, issue := range issues {
		if !ascAskCan(ctx, doer, person, issue, review) {
			continue
		}
		out = append(out, AscAskPull{ID: issue.ID, Index: issue.Index, Title: issue.Title, Repo: issue.Repo.FullName()})
	}
	return out, nil
}

// ascAskKind is "review" or "mention" from the path, else "".
func ascAskKind(ctx *context.Context) string {
	switch k := ctx.Params("kind"); k {
	case "review", "mention":
		return k
	}
	return ""
}

// ascAskPerson is the person of the profile when the signed-in doer may ask them, else it writes a 404.
func ascAskPerson(ctx *context.Context) *user_model.User {
	person := ctx.ContextUser
	if ctx.Doer == nil || person == nil || person.IsOrganization() || person.ID == ctx.Doer.ID || ascAskKind(ctx) == "" ||
		user_model.AscProfileLimited(person, ctx.Doer) {
		ctx.NotFound("ascAskPerson", nil)
		return nil
	}
	return person
}

// AscProfileAsk serves GET /{username}/-/ask/{kind}: the picker of the Request a review or Mention dialog.
func AscProfileAsk(ctx *context.Context) {
	person := ascAskPerson(ctx)
	if person == nil {
		return
	}
	kind := ascAskKind(ctx)
	pulls, err := ascAskPulls(ctx, ctx.Doer, person, kind == "review")
	if err != nil {
		ctx.ServerError("ascAskPulls", err)
		return
	}
	ctx.Data["AscAskKind"] = kind
	ctx.Data["AscAskPulls"] = pulls
	ctx.Data["AscAskMax"] = ascAskMentionMax
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.HTML(http.StatusOK, tplAscProfileAsk)
}

// ascAskIssue is the pull request of the form when doer may ask person about it, else it writes the answer.
func ascAskIssue(ctx *context.Context, person *user_model.User, review bool) *issues_model.Issue {
	id := ctx.FormInt64("pull")
	if id <= 0 {
		ctx.Error(http.StatusBadRequest)
		return nil
	}
	issue, err := issues_model.GetIssueByID(ctx, id)
	if err != nil {
		if issues_model.IsErrIssueNotExist(err) {
			ctx.NotFound("GetIssueByID", nil)
			return nil
		}
		ctx.ServerError("GetIssueByID", err)
		return nil
	}
	if issue.PosterID != ctx.Doer.ID || !ascAskCan(ctx, ctx.Doer, person, issue, review) {
		ctx.NotFound("ascAskCan", nil)
		return nil
	}
	return issue
}

// AscProfileAskPost serves POST /{username}/-/ask/{kind}: requests the review of the person on the chosen pull request,
// or posts the text as a comment on it that mentions the person.
func AscProfileAskPost(ctx *context.Context) {
	person := ascAskPerson(ctx)
	if person == nil {
		return
	}
	kind := ascAskKind(ctx)
	back := person.HomeLink()
	if kind == "mention" {
		text := strings.TrimSpace(ctx.FormString("content"))
		if text == "" || len(text) > ascAskMentionMax {
			ctx.Flash.Error(ctx.Tr("user.asc_profile.mention_empty"))
			ctx.Redirect(back)
			return
		}
		issue := ascAskIssue(ctx, person, false)
		if issue == nil {
			return
		}
		at := "@" + person.Name
		if !ascMentions(text, person.Name) {
			text = at + " " + text
		}
		comment, err := issue_service.CreateIssueComment(ctx, ctx.Doer, issue.Repo, issue, text, nil)
		if err != nil {
			if errors.Is(err, user_model.ErrBlockedByUser) {
				ctx.Flash.Error(ctx.Tr("user.asc_profile.ask_refused"))
				ctx.Redirect(back)
				return
			}
			ctx.ServerError("CreateIssueComment", err)
			return
		}
		log.Info("asc audit: profile mention by %s of %s on %s#%d", ctx.Doer.Name, person.Name, issue.Repo.FullName(), issue.Index)
		ctx.Redirect(issue.Link() + "#" + comment.HashTag())
		return
	}

	issue := ascAskIssue(ctx, person, true)
	if issue == nil {
		return
	}
	if _, err := issue_service.ReviewRequest(ctx, issue, ctx.Doer, person, true); err != nil {
		if issues_model.IsErrReviewRequestOnClosedPR(err) || issues_model.IsErrNotValidReviewRequest(err) {
			ctx.Flash.Error(ctx.Tr("user.asc_profile.ask_refused"))
			ctx.Redirect(back)
			return
		}
		ctx.ServerError("ReviewRequest", err)
		return
	}
	log.Info("asc audit: profile review request by %s of %s on %s#%d", ctx.Doer.Name, person.Name, issue.Repo.FullName(), issue.Index)
	ctx.Flash.Success(ctx.Tr("user.asc_profile.review_requested", person.GetDisplayName(), issue.Repo.FullName(), issue.Index))
	ctx.Redirect(back)
}

// ascMentions says whether text already mentions @name as a whole word.
func ascMentions(text, name string) bool {
	at := "@" + strings.ToLower(name)
	lower := strings.ToLower(text)
	for i := strings.Index(lower, at); i >= 0; {
		end := i + len(at)
		before := i == 0 || !ascNameByte(lower[i-1])
		// a name ends at a byte no name holds, or at a dot that ends the sentence
		after := end == len(lower) || !ascNameByte(lower[end]) ||
			lower[end] == '.' && (end+1 == len(lower) || !ascNameByte(lower[end+1]))
		if before && after {
			return true
		}
		next := strings.Index(lower[end:], at)
		if next < 0 {
			break
		}
		i = end + next
	}
	return false
}

func ascNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.'
}
