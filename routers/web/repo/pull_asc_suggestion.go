// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"strconv"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// "Apply as vN" of a suggested change of a stacked pull request (services/pull/asc_suggestion.go). The button is in
// templates/repo/pulls/asc_suggestions.tmpl; the work and the permission check are pull_service.AscApplySuggestion.

// prepareAscSuggestions puts the open suggested changes of a stacked pull request into ctx.Data["AscSuggestions"], and
// whether the viewer may apply them into ctx.Data["AscCanApplySuggestion"]. It never fails the page.
func prepareAscSuggestions(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil || !setting.Asc.Enabled || ctx.Data["AscStackPanel"] == nil {
		return
	}
	list, err := pull_service.AscSuggestions(ctx, pull)
	if err != nil {
		if !errors.Is(err, pull_service.ErrAscNotStacked) {
			log.Warn("asc suggestions of pull request %d: %v", pull.ID, err)
		}
		return
	}
	if len(list) == 0 {
		return
	}
	ctx.Data["AscSuggestions"] = list
	latest, _ := ctx.Data["AscVersionsLatest"].(uint32)
	ctx.Data["AscSuggestionNext"] = latest + 1
	if !ctx.IsSigned || ctx.Doer == nil {
		return
	}
	allowed, err := pull_service.AscMayRestoreVersion(ctx, pull, ctx.Doer)
	if err != nil || !allowed {
		return
	}
	open, err := pull_service.AscPullIsOpen(ctx, pull)
	ctx.Data["AscCanApplySuggestion"] = err == nil && open
}

// parseAscSuggestionID reads an id of a thread or a comment from a form value.
func parseAscSuggestionID(s string) (uint64, bool) {
	n, err := strconv.ParseUint(s, 10, 53)
	return n, err == nil && n > 0
}

// ascSuggestionFailure maps an error of AscApplySuggestion to its answer. A status of 0 means the page is shown again
// with the flash message of the locale key; known is false for an error that is a server error.
func ascSuggestionFailure(err error) (status int, key string, known bool) {
	var stale *pull_service.AscSuggestionStale
	switch {
	case errors.As(err, &stale):
		return 0, "repo.pulls.asc_suggestion.stale", true
	case errors.Is(err, pull_service.ErrAscRestoreForbidden):
		return http.StatusForbidden, "", true
	case errors.Is(err, pull_service.ErrAscNotStacked), errors.Is(err, pull_service.ErrAscBadVersion):
		return http.StatusNotFound, "", true
	case errors.Is(err, pull_service.ErrAscRestoreNotOpen):
		return 0, "repo.pulls.asc_versions.restore_not_open", true
	case errors.Is(err, pull_service.ErrAscUnavailable), errors.Is(err, pull_service.ErrAscBadAnswer):
		return 0, "repo.pulls.asc_suggestion.unavailable", true
	}
	return 0, "", false
}

// AscApplySuggestion handles POST /{owner}/{repo}/pulls/{index}/asc/apply_suggestion with the form fields "thread" and
// "comment". The route is behind sign-in, the CSRF check of every web POST and the pull request unit's read check; the
// service decides whether the doer may apply.
func AscApplySuggestion(ctx *context.Context) {
	if !ctx.IsSigned {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !setting.Asc.Enabled {
		ctx.NotFound("asc is not enabled", nil)
		return
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	thread, ok1 := parseAscSuggestionID(ctx.FormString("thread"))
	comment, ok2 := parseAscSuggestionID(ctx.FormString("comment"))
	if !ok1 || !ok2 {
		ctx.NotFound("thread or comment is not an id", nil)
		return
	}
	res, err := pull_service.AscApplySuggestion(ctx, issue.PullRequest, ctx.Doer, thread, comment)
	if err != nil {
		status, key, known := ascSuggestionFailure(err)
		switch {
		case !known:
			ctx.ServerError("AscApplySuggestion", err)
		case status == http.StatusForbidden:
			ctx.Error(http.StatusForbidden)
		case status != 0:
			ctx.NotFound("AscApplySuggestion", err)
		default:
			var stale *pull_service.AscSuggestionStale
			if errors.As(err, &stale) {
				ctx.Flash.Error(ctx.Tr(key, stale.Reason))
			} else {
				log.Warn("asc apply suggestion of pull request %d: %v", issue.PullRequest.ID, err)
				ctx.Flash.Error(ctx.Tr(key))
			}
			ctx.Redirect(issue.Link())
		}
		return
	}
	switch {
	case res.Pending:
		ctx.Flash.Warning(ctx.Tr("repo.pulls.asc_suggestion.applied_pending", res.Version))
	case res.MarkFailed:
		ctx.Flash.Warning(ctx.Tr("repo.pulls.asc_suggestion.applied_unmarked", res.Version))
	default:
		ctx.Flash.Success(ctx.Tr("repo.pulls.asc_suggestion.applied", res.Version))
	}
	ctx.Redirect(issue.Link())
}
