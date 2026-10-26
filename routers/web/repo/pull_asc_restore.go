// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// "Restore version" of a stacked pull request (SPEC 20.5 flow 2, PLAN unit C47). The button is in
// templates/repo/pulls/asc_versions.tmpl; the work and the permission check are pull_service.AscRestoreVersion.

// ascRestoreFailure maps an error of AscRestoreVersion to its answer. A status of 0 means the page is shown
// again with the flash message of the locale key; known is false for an error that is a server error.
func ascRestoreFailure(err error) (status int, key string, known bool) {
	switch {
	case errors.Is(err, pull_service.ErrAscRestoreForbidden):
		return http.StatusForbidden, "", true
	case errors.Is(err, pull_service.ErrAscNotStacked), errors.Is(err, pull_service.ErrAscBadVersion):
		return http.StatusNotFound, "", true
	case errors.Is(err, pull_service.ErrAscRestoreNotOpen):
		return 0, "repo.pulls.asc_versions.restore_not_open", true
	case errors.Is(err, pull_service.ErrAscRestoreCurrent):
		return 0, "repo.pulls.asc_versions.restore_current", true
	case errors.Is(err, pull_service.ErrAscRestoreGone):
		return 0, "repo.pulls.asc_versions.restore_gone", true
	case errors.Is(err, pull_service.ErrAscUnavailable), errors.Is(err, pull_service.ErrAscBadAnswer):
		return 0, "repo.pulls.asc_versions.restore_unavailable", true
	}
	return 0, "", false
}

// AscRestoreVersion handles POST /{owner}/{repo}/pulls/{index}/asc/restore_version with the form field
// "version". The route is behind sign-in, the CSRF check of every web POST, and the pull request unit's read
// check; the service decides whether the doer may restore.
func AscRestoreVersion(ctx *context.Context) {
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
	version, ok := parseAscVersionNumber(ctx.FormString("version"))
	if !ok {
		ctx.NotFound("version is not a version number", nil)
		return
	}

	res, err := pull_service.AscRestoreVersion(ctx, issue.PullRequest, ctx.Doer, version)
	if err != nil {
		status, key, known := ascRestoreFailure(err)
		switch {
		case !known:
			ctx.ServerError("AscRestoreVersion", err)
		case status == http.StatusForbidden:
			ctx.Error(http.StatusForbidden)
		case status != 0:
			ctx.NotFound("AscRestoreVersion", err)
		default:
			if errors.Is(err, pull_service.ErrAscUnavailable) || errors.Is(err, pull_service.ErrAscBadAnswer) {
				log.Warn("asc restore version of pull request %d: %v", issue.PullRequest.ID, err)
			}
			ctx.Flash.Error(ctx.Tr(key))
			ctx.Redirect(issue.Link())
		}
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_versions.restored", res.Version))
	ctx.Redirect(issue.Link())
}
