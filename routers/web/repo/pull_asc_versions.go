// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	"errors"
	"strconv"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/gitdiff"
	pull_service "forgejo.org/services/pull"
)

// The versions dropdown, the interdiff and "changes since your last review" of the pull request pages (SPEC
// 20.5 flow 2, PLAN unit C37). The dropdown is templates/repo/pulls/asc_versions.tmpl. Every caller has already
// checked that the viewer may read the pull request; the change always comes from the pull request itself.

// prepareAscVersions puts the verified versions of a stacked pull request, and the version of the viewer's
// last review, into ctx.Data for the dropdown. AscVersionFrom and AscVersionTo are always set (0: not shown). It
// never fails the page: for a pull request that is not stacked, or when asc cannot answer, no dropdown is shown.
func prepareAscVersions(ctx *context.Context, pull *issues_model.PullRequest) *pull_service.AscVersionList {
	ctx.Data["AscVersionFrom"] = uint32(0)
	ctx.Data["AscVersionTo"] = uint32(0)
	ctx.Data["AscHeadVersion"] = uint32(0)
	ctx.Data["AscCanRestore"] = false
	if pull == nil || !setting.Asc.Enabled {
		return nil
	}
	versions, err := pull_service.AscVersions(ctx, pull)
	if err != nil {
		if !errors.Is(err, pull_service.ErrAscNotStacked) {
			log.Warn("asc versions of pull request %d: %v", pull.ID, err)
		}
		return nil
	}
	if len(versions.Versions) == 0 {
		return nil
	}
	ctx.Data["AscVersions"] = versions
	prepareAscSignatures(ctx, versions)
	prepareAscVersionDetails(ctx, pull, versions)
	prepareAscRestore(ctx, pull, versions)
	if ctx.Doer != nil {
		seen, err := pull_service.AscSeenVersion(ctx, pull, ctx.Doer.ID, versions)
		if err != nil {
			log.Error("asc last reviewed version of pull request %d: %v", pull.ID, err)
		} else if seen > 0 && seen < versions.Latest() {
			ctx.Data["AscSinceVersion"] = seen
		}
	}
	return versions
}

// ascVersionView is what the files page shows when it is asked for one version, or for the interdiff of two.
type ascVersionView struct {
	from, to   uint32
	start, end string // commit ids the page shows the diff between
	interdiff  *pull_service.AscInterDiff
}

// ascVersionViewOf reads the asc_from and asc_to query of the files page. It returns nil when neither is given.
// A number that is not a version of the pull request's change, a commit that the repository lacks, or an
// unusable combination answers 404, and the second result is false after any answer.
func ascVersionViewOf(ctx *context.Context, pull *issues_model.PullRequest, versions *pull_service.AscVersionList, gitRepo *git.Repository, mergeBase string) (*ascVersionView, bool) {
	fromQ, toQ := ctx.FormString("asc_from"), ctx.FormString("asc_to")
	if fromQ == "" && toQ == "" {
		return nil, true
	}
	if toQ == "" {
		ctx.NotFound("asc_from without asc_to", nil)
		return nil, false
	}
	to, ok := parseAscVersionNumber(toQ)
	if !ok || !versions.Has(to) {
		ctx.NotFound("asc_to is not a version", nil)
		return nil, false
	}
	view := &ascVersionView{to: to, start: mergeBase, end: versions.Versions[to-1].GitCommit}
	if fromQ != "" {
		from, ok := parseAscVersionNumber(fromQ)
		if !ok || !versions.Has(from) || from == to {
			ctx.NotFound("asc_from is not a version", nil)
			return nil, false
		}
		view.from = from
		view.start = versions.Versions[from-1].GitCommit
	}
	for _, id := range []string{view.start, view.end} {
		if _, err := gitRepo.GetCommit(id); err != nil {
			ctx.NotFound("asc version commit is not in the repository", err)
			return nil, false
		}
	}
	if view.from != 0 {
		d, err := pull_service.AscInterDiffOf(ctx, pull, view.from, view.to, nil)
		if err != nil {
			if errors.Is(err, pull_service.ErrAscBadVersion) || errors.Is(err, pull_service.ErrAscNotStacked) || errors.Is(err, pull_service.ErrAscBadPaths) {
				ctx.NotFound("AscInterDiffOf", err)
			} else {
				ctx.ServerError("AscInterDiffOf", err)
			}
			return nil, false
		}
		view.interdiff = d
	}
	return view, true
}

// parseAscVersionNumber parses a version number: decimal digits only, at least 1.
func parseAscVersionNumber(s string) (uint32, bool) {
	if s == "" || len(s) > 10 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint32(n), true
}

// parseAscInterDiff turns the patch of an interdiff into a diff under the limits of the ordinary diff page.
func parseAscInterDiff(ctx *context.Context, d *pull_service.AscInterDiff, maxLines, maxFiles int, skipTo string) (*gitdiff.Diff, error) {
	diff, err := gitdiff.ParsePatch(ctx, maxLines, setting.Git.MaxGitDiffLineCharacters, maxFiles, bytes.NewReader(d.Patch), skipTo)
	if err != nil {
		return nil, err
	}
	diff.Start = skipTo
	if d.Truncated {
		diff.IsIncomplete = true
	}
	return diff, nil
}

// prepareAscRestore sets AscHeadVersion (the version the head of the pull request is at, 0 when it is at none) and
// AscCanRestore (the viewer may restore a version now) for the versions dropdown. Neither failure fails the page.
func prepareAscRestore(ctx *context.Context, pull *issues_model.PullRequest, versions *pull_service.AscVersionList) {
	head, err := pull_service.AscHeadVersion(ctx, pull, versions)
	if err != nil {
		log.Warn("asc head version of pull request %d: %v", pull.ID, err)
	} else {
		ctx.Data["AscHeadVersion"] = head
	}
	if ctx.Doer == nil || len(versions.Versions) < 2 {
		return
	}
	open, err := pull_service.AscPullIsOpen(ctx, pull)
	if err != nil || !open {
		if err != nil {
			log.Warn("asc restore: state of pull request %d: %v", pull.ID, err)
		}
		return
	}
	may, err := pull_service.AscMayRestoreVersion(ctx, pull, ctx.Doer)
	if err != nil {
		log.Warn("asc restore: permission on pull request %d: %v", pull.ID, err)
		return
	}
	ctx.Data["AscCanRestore"] = may
}
