// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The author writes the brief of a stacked change from the review page: what it does, why, how to check it, what could go wrong,
// and where to look first (Review.SetBrief). The form is in templates/repo/pulls/asc_brief.tmpl. Writing needs write access to the
// repository: the call carries the claim of the person (asctrust.CallRepoWrite), and asc decides again.

const (
	ascBriefFormMaxText  = 8 << 10
	ascBriefFormMaxWhere = 20
)

// ascBriefWhereOf reads the "where to look" lines of the form: one pointer per line, "path", "path:line" or "path:line note".
// A line without a usable path is dropped; a line number that is not a number is part of the note.
func ascBriefWhereOf(text string) []*ascv1.WhereToLook {
	var out []*ascv1.WhereToLook
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || len(out) >= ascBriefFormMaxWhere {
			continue
		}
		target, note, _ := strings.Cut(line, " ")
		path, lineNo, hasLine := strings.Cut(target, ":")
		w := &ascv1.WhereToLook{Note: strings.TrimSpace(note)}
		if hasLine {
			n, err := strconv.ParseUint(lineNo, 10, 32)
			if err != nil || n > ascSymbolsMaxLine {
				w.Note = strings.TrimSpace(lineNo + " " + note)
			} else {
				w.Line = uint32(n)
			}
		}
		if path == "" || !utf8.ValidString(path) || strings.ContainsRune(path, 0) || len(path) > 1024 {
			continue
		}
		w.Path = []byte(path)
		out = append(out, w)
	}
	return out
}

// AscSetBrief handles POST /{owner}/{repo}/pulls/{index}/asc/brief.
func AscSetBrief(ctx *context.Context) {
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
	if !issue.IsPull {
		ctx.NotFound("not a pull request", nil)
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, issue.PullRequest.ID)
	if err != nil {
		ctx.ServerError("GetPullRequestAscStack", err)
		return
	}
	repo := ctx.Repo.Repository
	if link == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("not a stacked change", nil)
		return
	}
	name, err := ascGraphRepoName(repo)
	change := ascChangeBytes(link.AscChange)
	if err != nil || change == nil {
		ctx.NotFound("not a stacked change", nil)
		return
	}

	brief := &ascv1.ReviewBrief{
		What: ctx.FormString("what"), Why: ctx.FormString("why"), How: ctx.FormString("how"), RiskNote: ctx.FormString("risk_note"),
		WhereToLook: ascBriefWhereOf(ctx.FormString("where")),
	}
	for _, s := range []string{brief.What, brief.Why, brief.How, brief.RiskNote} {
		if len(s) > ascBriefFormMaxText || !utf8.ValidString(s) {
			ctx.Flash.Error(ctx.Tr("repo.pulls.asc_brief.too_long"))
			ctx.Redirect(issue.Link())
			return
		}
	}

	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		if errors.Is(err, asctrust.ErrForbidden) {
			ctx.Error(http.StatusForbidden)
			return
		}
		ctx.ServerError("UserCall", err)
		return
	}
	defer release()
	conn, err := ascRPCConn()
	if err != nil {
		ctx.Flash.Error(ctx.Tr("repo.pulls.asc_brief.unavailable"))
		ctx.Redirect(issue.Link())
		return
	}
	cctx, cancel := gocontext.WithTimeout(gocontext.WithoutCancel(callCtx), ascBriefTimeout)
	defer cancel()
	if _, err := ascv1.NewReviewClient(conn).SetBrief(cctx, &ascv1.SetBriefRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Brief: brief}); err != nil {
		log.Warn("asc brief: SetBrief for %s: %v", name, err)
		ctx.Flash.Error(ctx.Tr("repo.pulls.asc_brief.unavailable"))
		ctx.Redirect(issue.Link())
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_brief.saved"))
	ctx.Redirect(issue.Link())
}
