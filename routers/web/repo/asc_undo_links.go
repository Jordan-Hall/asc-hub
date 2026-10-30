// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"html"
	htmltemplate "html/template"
	"net/url"
	"strconv"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// Undo on the review page: "Restacked onto main, 3 min ago" with Undo, and the Undo of the banner that follows a
// merge. Both read the newest operation of one ref from the ref log as the signed-in person; only that operation can
// be undone, so there is at most one link. Every read is advice: a failure leaves the line or the link out.

// ascRestackLine is the line of the review page about the newest restack of the change.
type ascRestackLine struct {
	Base    string
	Actor   string
	Mine    bool
	At      time.Time
	UndoURL string
}

// ascUndoURL is the address of the undo preview of one operation of the log.
func ascUndoURL(repo *repo_model.Repository, row *asctrust.OpRow) string {
	return ascOpsURL(repo) + "/undo?" + url.Values{"seq": {strconv.FormatUint(row.Seq, 10)}, "ref": {row.Ref}}.Encode()
}

// ascNewestOp returns the newest operation on ref, or nil when there is none or it cannot be read.
func ascNewestOp(ctx *context.Context, repo *repo_model.Repository, ref string) *asctrust.OpRow {
	if !setting.Asc.Enabled || ctx.Doer == nil || !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		return nil
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return nil
	}
	defer release()
	page, err := asctrust.LoadOpLog(callCtx, conn, name, ascUndoWho(ctx), asctrust.OpLogQuery{Ref: ref, Limit: 1})
	if err != nil {
		log.Trace("asc newest operation of %s: %v", ref, asctrust.Classify(err))
		return nil
	}
	if len(page.Rows) == 0 || page.Rows[0].Ref != ref {
		return nil
	}
	return &page.Rows[0]
}

// ascRestackLineOf makes the restack line from the newest operation of the change's branch: nil unless that
// operation is a restack.
func ascRestackLineOf(repo *repo_model.Repository, base string, row *asctrust.OpRow, canUndo bool) *ascRestackLine {
	if row == nil || row.ViaKey != "restack" {
		return nil
	}
	line := &ascRestackLine{Base: base, Actor: row.Actor, Mine: row.Mine, At: row.At}
	if canUndo && row.Undoable {
		line.UndoURL = ascUndoURL(repo, row)
	}
	return line
}

// prepareAscRestackLine puts the restack line of an open stacked change into ctx.Data["AscRestackLine"].
func prepareAscRestackLine(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil || pull.HasMerged || pull.HeadBranch == "" || pull.HeadRepoID != pull.BaseRepoID || ctx.Doer == nil || ctx.Repo == nil {
		return
	}
	if err := pull.LoadIssue(ctx); err != nil || pull.Issue.IsClosed {
		return
	}
	repo := ctx.Repo.Repository
	row := ascNewestOp(ctx, repo, "refs/heads/"+pull.HeadBranch)
	if line := ascRestackLineOf(repo, pull.BaseBranch, row, ctx.Repo.CanWrite(unit.TypeCode)); line != nil {
		ctx.Data["AscRestackLine"] = line
	}
}

// ascMergedMessage is the banner that follows "Merge now": what landed and, while the merge is the newest operation on
// the branch, an Undo link (only the newest operation of a ref can be undone, so the link goes when anything else lands).
func ascMergedMessage(ctx *context.Context, pr *issues_model.PullRequest, merged int) htmltemplate.HTML {
	msg := ctx.Tr("repo.pulls.asc_op.merged", merged)
	row := ascNewestOp(ctx, ctx.Repo.Repository, "refs/heads/"+pr.BaseBranch)
	if row == nil || !row.Undoable {
		return msg
	}
	return msg + htmltemplate.HTML(" "+html.EscapeString(ctx.Locale.TrString("asc_review.merged_undo", pr.BaseBranch))+
		` <a href="`+html.EscapeString(ascUndoURL(ctx.Repo.Repository, row))+`" data-testid="asc-merged-undo">`+html.EscapeString(ctx.Locale.TrString("asc_trust.ops.undo"))+`</a>`)
}
