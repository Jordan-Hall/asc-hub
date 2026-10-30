// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The merge queue and the stack operations on the web (docs/BUILDOUT.md sections 6 and 7):
//
//	GET  /{owner}/{repo}/asc/queue                      the queue of a branch (templates/repo/asc_queue.tmpl)
//	POST /{owner}/{repo}/asc/queue/pause|resume         pause or resume the branch (administrators)
//	GET  /{owner}/{repo}/pulls/{n}/asc/confirm?op=...   the dry run and the confirmation of an operation
//	POST /{owner}/{repo}/pulls/{n}/asc/enqueue          merge when ready, for the stack from the bottom up to n
//	POST /{owner}/{repo}/pulls/{n}/asc/dequeue          take n (and what is queued above it) out of the queue
//	POST /{owner}/{repo}/pulls/{n}/asc/merge            merge the stack from the bottom up to n now
//	POST /{owner}/{repo}/pulls/{n}/asc/restack          restack the stack of n onto the target
//	POST /{owner}/{repo}/pulls/{n}/asc/revert           revert the landed stack of n
//
// Every write is a POST behind sign-in and the CSRF check of the router. The handlers answer 404 when asc is
// off or the viewer cannot read the pull requests, 403 for a visitor who is not signed in, and leave the decision
// whether the doer may do the operation to the service, which fails closed. Operations are reached from the
// confirmation page, which runs the dry run first, so nothing is written by a link.

// The operations of the confirmation page.
const (
	ascOpQueue   = "queue"
	ascOpUnqueue = "unqueue"
	ascOpMerge   = "merge"
	ascOpRestack = "restack"
	ascOpRevert  = "revert"
)

const (
	tplAscQueue   = "repo/asc_queue"
	tplAscConfirm = "repo/asc_confirm"

	// ascMaxBranchBytes bounds the branch name taken from a request.
	ascMaxBranchBytes = 255
)

// ascStackTarget loads the pull request of an asc stack operation after the checks every one of them shares.
// It reports false after it has answered.
func ascStackTarget(ctx *context.Context) (*issues_model.Issue, *issues_model.PullRequest, bool) {
	if !ctx.IsSigned {
		ctx.Error(http.StatusForbidden)
		return nil, nil, false
	}
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc stack operation", nil)
		return nil, nil, false
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return nil, nil, false
	}
	return issue, issue.PullRequest, true
}

// ascLandTarget is ascStackTarget for the operations that land a prefix of the stack (queue and merge): the form field
// "to" names another pull request of the same stack (by its number) as the top of the prefix to land. "To" that is
// not a member of the stack of the pull request is a 404, so a number cannot reach another stack or repository.
func ascLandTarget(ctx *context.Context) (*issues_model.Issue, *issues_model.PullRequest, bool) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return nil, nil, false
	}
	to := ctx.FormInt64("to")
	if to == 0 || to == issue.Index {
		return issue, pr, true
	}
	panel, err := pull_service.BuildAscStackPanel(ctx, pr)
	if err != nil {
		ctx.ServerError("BuildAscStackPanel", err)
		return nil, nil, false
	}
	if !ascStackHasIndex(panel, to) {
		ctx.NotFound("asc stack operation", nil)
		return nil, nil, false
	}
	target, err := issues_model.GetPullRequestByIndex(ctx, pr.BaseRepoID, to)
	if err != nil {
		ctx.NotFound("asc stack operation", err)
		return nil, nil, false
	}
	if err := target.LoadIssue(ctx); err != nil {
		ctx.ServerError("LoadIssue", err)
		return nil, nil, false
	}
	if err := target.Issue.LoadRepo(ctx); err != nil {
		ctx.ServerError("LoadRepo", err)
		return nil, nil, false
	}
	target.Issue.PullRequest = target
	return target.Issue, target, true
}

// ascStackHasIndex says whether the pull request with this number is in the panel.
func ascStackHasIndex(panel *pull_service.AscStackPanel, index int64) bool {
	if panel == nil {
		return false
	}
	for _, e := range panel.Entries {
		if e.Index == index {
			return true
		}
	}
	return false
}

// AscLandScope is one choice of how much of the stack to land: the prefix that ends at the pull request Index.
type AscLandScope struct {
	Index   int64
	Count   int // changes landed by this choice
	Total   int
	Title   string
	Checked bool
}

// ascLandScopes lists the prefixes of the stack that can be landed, bottom first, and marks the one that ends at current.
func ascLandScopes(panel *pull_service.AscStackPanel, current int64) []AscLandScope {
	if panel == nil {
		return nil
	}
	open := make([]pull_service.AscStackEntry, 0, len(panel.Entries))
	for _, e := range panel.Entries {
		if e.State == "open" || e.State == "draft" || e.Current {
			open = append(open, e)
		}
	}
	scopes := make([]AscLandScope, 0, len(open))
	for i, e := range open {
		scopes = append(scopes, AscLandScope{Index: e.Index, Count: i + 1, Total: len(open), Title: e.Title, Checked: e.Index == current})
	}
	return scopes
}

// ascValidBranch reports whether name can be a branch name taken from a request: not empty, bounded, valid
// UTF-8, no control character.
func ascValidBranch(name string) bool {
	if name == "" || len(name) > ascMaxBranchBytes || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ascOpFailure is how an error of an operation is shown.
type ascOpFailure struct {
	Key    string // locale key
	Args   []any
	Status int // the status of a page answer; flashes redirect and use 0
	Known  bool
}

// ascMaxShownPaths bounds the conflict paths named in a message.
const ascMaxShownPaths = 5

func ascPathsArg(paths []string) string {
	if len(paths) > ascMaxShownPaths {
		paths = append(paths[:ascMaxShownPaths:ascMaxShownPaths], "…")
	}
	return strings.Join(paths, ", ")
}

// ascOpError maps an error of a stack operation to its message. Only text this code wrote is shown: no error of
// asc or of a database reaches the page. known is false for an error the caller must treat as a server error.
func ascOpError(err error) ascOpFailure {
	var conflict *ascbridge.ConflictError
	switch {
	case err == nil:
		return ascOpFailure{}
	case errors.Is(err, pull_service.ErrAscStackNotStacked):
		return ascOpFailure{Key: "repo.pulls.asc_op.err.not_stacked", Status: http.StatusNotFound, Known: true}
	case errors.As(err, &conflict):
		key := "repo.pulls.asc_op.err.conflict"
		if errors.Is(err, ascbridge.ErrRevertConflict) {
			key = "repo.pulls.asc_op.err.revert_conflict"
		}
		return ascOpFailure{Key: key, Args: []any{ascPathsArg(conflict.Paths)}, Known: true}
	}
	for _, e := range []struct {
		target error
		key    string
		status int
	}{
		{automerge.ErrAscQueueNotAllowed, "not_allowed", http.StatusForbidden},
		{automerge.ErrAscQueueNoPermission, "not_allowed", http.StatusForbidden},
		{pull_service.ErrAscStackNotAllowed, "not_allowed", http.StatusForbidden},
		{pull_service.ErrAscRestackNotAllowed, "not_allowed", http.StatusForbidden},
		{pull_service.ErrAscRevertNotAllowed, "not_allowed", http.StatusForbidden},
		{ascbridge.ErrStackDenied, "not_allowed", http.StatusForbidden},
		{automerge.ErrAscQueueDisabled, "disabled", 0},
		{pull_service.ErrAscStackDisabled, "disabled", 0},
		{pull_service.ErrAscRevertDisabled, "disabled", 0},
		{ascbridge.ErrStacksUnavailable, "unavailable", 0},
		{automerge.ErrAscQueuePaused, "paused", 0},
		{automerge.ErrAscQueueBusy, "busy", 0},
		{pull_service.ErrAscRevertBusy, "revert_busy", 0},
		{pull_service.ErrAscLandNeedsContentMerge, "needs_content_merge", 0},
		{pull_service.ErrAscStackHeadMoved, "head_moved", 0},
		{pull_service.ErrAscRestackNotOpen, "not_open", 0},
		{pull_service.ErrAscRestackNoPull, "no_pull", 0},
		{pull_service.ErrAscRevertNotLanded, "not_landed", 0},
		{ascbridge.ErrStackNotLanded, "not_landed", 0},
		{pull_service.ErrAscRevertBottom, "bad_bottom", http.StatusNotFound},
		{ascbridge.ErrStackFrozen, "frozen", 0},
		{ascbridge.ErrStackNotFound, "asc_not_found", 0},
		{ascbridge.ErrStackBadRequest, "asc_refused", 0},
		{ascbridge.ErrBadAnswer, "bad_answer", 0},
		{pull_service.ErrAscRevertBadAnswer, "bad_answer", 0},
	} {
		if errors.Is(err, e.target) {
			return ascOpFailure{Key: "repo.pulls.asc_op.err." + e.key, Status: e.status, Known: true}
		}
	}
	// A refusal of asc (the policy, the gate) carries asc's own sentence: the person is told what to do.
	var refusal *pull_service.AscRefusal
	if errors.As(err, &refusal) {
		// The typed code goes with asc's sentence, and the files it names are in the sentence.
		text := refusal.Message
		if refusal.Code != "" {
			text = refusal.Code + ": " + text
		}
		return ascOpFailure{Key: "repo.pulls.asc_op.err.refused", Args: []any{text}, Known: true}
	}
	// Not ready and not queueable carry our own text (a pull request number and a reason).
	if errors.Is(err, pull_service.ErrAscStackNotMergable) || errors.Is(err, automerge.ErrAscQueueNotQueueable) {
		return ascOpFailure{Key: "repo.pulls.asc_op.err.not_ready", Args: []any{err.Error()}, Known: true}
	}
	return ascOpFailure{}
}

// ascOpFlashFail answers a failed operation: a flash on the pull request page, a 403 for a refused doer, a 404
// for a missing stack, a server error for what is not known.
func ascOpFlashFail(ctx *context.Context, op string, issue *issues_model.Issue, err error) {
	f := ascOpError(err)
	switch {
	case !f.Known:
		ctx.ServerError("asc "+op, err)
	case f.Status == http.StatusNotFound:
		ctx.NotFound("asc "+op, err)
	default:
		if f.Status == http.StatusForbidden {
			log.Warn("asc %s refused for user %d on pull request %d: %v", op, ctx.Doer.ID, issue.ID, err)
		}
		ctx.Flash.Error(ctx.Tr(f.Key, f.Args...))
		ctx.Redirect(issue.Link())
	}
}

// AscQueueEnqueue handles "Merge when ready" for the stack from the bottom up to this pull request.
func AscQueueEnqueue(ctx *context.Context) {
	issue, pr, ok := ascLandTarget(ctx)
	if !ok {
		return
	}
	res, err := automerge.AscQueueEnqueue(ctx, ctx.Doer, pr)
	if err != nil {
		ascOpFlashFail(ctx, ascOpQueue, issue, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.queued", len(res.Queued)))
	ctx.Redirect(issue.Link())
}

// AscQueueDequeue takes the stack of this pull request, and the queued pull requests above it, out of the queue.
func AscQueueDequeue(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	res, err := automerge.AscQueueCancelEntries(ctx, ctx.Doer, pr)
	if err != nil {
		ascOpFlashFail(ctx, ascOpUnqueue, issue, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.dequeued"))
	if len(res.Skipped) > 0 {
		ctx.Flash.Warning(ctx.Tr("repo.pulls.asc_op.dequeued_others", len(res.Skipped)))
	}
	ctx.Redirect(issue.Link())
}

// AscQueueMergeNow merges the stack from the bottom up to this pull request at once, under the lease of the
// branch so no other land runs beside it.
func AscQueueMergeNow(ctx *context.Context) {
	issue, pr, ok := ascLandTarget(ctx)
	if !ok {
		return
	}
	merged, err := automerge.AscQueueLandNow(ctx, ctx.Doer, pr)
	if err != nil {
		ascOpFlashFail(ctx, ascOpMerge, issue, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.pulls.asc_op.merged", len(merged)))
	ctx.Redirect(issue.Link())
}

// AscQueuePage renders the queue of one branch (the default branch unless ?branch= says otherwise).
func AscQueuePage(ctx *context.Context) {
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc queue", nil)
		return
	}
	repo := ctx.Repo.Repository
	if ok, err := automerge.AscQueueBackedBy(repo); err != nil {
		ctx.ServerError("AscQueueBackedBy", err)
		return
	} else if !ok {
		ctx.NotFound("asc queue", nil)
		return
	}
	branch := strings.TrimSpace(ctx.FormString("branch"))
	if branch == "" {
		branch = repo.DefaultBranch
	}
	if !ascValidBranch(branch) {
		ctx.NotFound("asc queue", nil)
		return
	}
	view, err := automerge.AscQueueViewOf(ctx, repo, branch, ctx.Doer)
	if err != nil {
		ctx.ServerError("AscQueueViewOf", err)
		return
	}
	canAdmin, err := automerge.AscQueueMayAdmin(ctx, ctx.Doer, repo)
	if err != nil {
		ctx.ServerError("AscQueueMayAdmin", err)
		return
	}
	var ids []int64
	for _, rows := range [][]automerge.AscQueueRow{view.Active, view.Recent} {
		for _, r := range rows {
			ids = append(ids, r.Entry.DoerID)
		}
	}
	list, err := user_model.GetUsersByIDs(ctx, ids)
	if err != nil {
		ctx.ServerError("GetUsersByIDs", err)
		return
	}
	users := make(map[int64]*user_model.User, len(list))
	for _, u := range list {
		users[u.ID] = u
	}
	ctx.Data["Title"] = ctx.Locale.TrString("repo.asc_queue.title")
	ctx.Data["PageIsAscQueue"] = true
	ctx.Data["AscQueue"] = view
	ctx.Data["AscQueueUsers"] = users
	ctx.Data["AscQueueCanAdmin"] = canAdmin
	ctx.Data["AscQueueBranch"] = branch
	if ctx.Doer != nil && ctx.Repo.CanWrite(unit.TypePullRequests) {
		queued := map[int64]bool{}
		for _, r := range view.Active {
			queued[r.Entry.PullID] = true
		}
		if stacks, err := ascQueueStacksOf(ctx, repo, branch, queued); err != nil {
			log.Warn("asc queue: the open stacks: %v", err)
		} else {
			ctx.Data["AscQueueStacks"] = stacks
		}
	}
	ctx.Data["AscQueueRevalidate"] = automerge.AscQueueNoContentMerge()
	ctx.Data["AscQuarantine"] = ascQueueQuarantine(ctx)
	ascQueueStats(ctx, repo, branch, view)
	ctx.HTML(http.StatusOK, tplAscQueue)
}

// ascQuarantineView is the template data under .AscQuarantine: the failures that do not hold up a landing, and why.
type ascQuarantineView struct {
	Rows      []asctrust.GroupRow
	More      bool
	ChecksURL string
	// Problem is a fixed sentence when the list could not be read; the queue itself is shown either way.
	Problem string
}

// ascQueueQuarantine reads the quarantined failure groups for the queue page. They are CI data about changes and
// tests, so the viewer must pass the guard of the checks pages (trustweb.CheckReader: asc repository, read access to
// the code and to the Actions); anyone else sees no section. A failure to read them never stops the queue page: the
// section says it could not be read.
func ascQueueQuarantine(ctx *context.Context) *ascQuarantineView {
	repo, name, ok := trustweb.CheckReader(ctx)
	if !ok || !asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		return nil
	}
	v := &ascQuarantineView{ChecksURL: ascChecksURL(repo)}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc queue: no connection to asc serve: %v", err)
		v.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		return v
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		v.Problem = trustweb.FailureOf("queue quarantine credential", err).Localized(ctx)
		return v
	}
	defer release()
	q, err := asctrust.LoadQuarantined(callCtx, conn, name)
	if err != nil {
		v.Problem = trustweb.FailureOf("queue quarantine", err).Localized(ctx)
		return v
	}
	v.Rows, v.More = q.Rows, q.More
	return v
}

// ascQueueSetPaused is the shared body of AscQueuePause and AscQueueResume.
func ascQueueSetPaused(ctx *context.Context, paused bool) {
	if !ctx.IsSigned {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc queue", nil)
		return
	}
	branch := strings.TrimSpace(ctx.FormString("branch"))
	if !ascValidBranch(branch) {
		ctx.NotFound("asc queue", nil)
		return
	}
	back := ctx.Repo.RepoLink + "/asc/queue?branch=" + url.QueryEscape(branch)
	err := automerge.AscQueueSetPaused(ctx, ctx.Doer, ctx.Repo.Repository, branch, paused)
	switch {
	case errors.Is(err, automerge.ErrAscQueueNoPermission):
		ctx.Error(http.StatusForbidden)
		return
	case err != nil:
		ctx.ServerError("AscQueueSetPaused", err)
		return
	}
	if paused {
		ctx.Flash.Success(ctx.Tr("repo.asc_queue.paused_done"))
	} else {
		ctx.Flash.Success(ctx.Tr("repo.asc_queue.resumed_done"))
		automerge.AscQueueKick(ctx.Repo.Repository.ID, branch)
	}
	ctx.Redirect(back)
}

// AscQueuePause pauses the queue of a branch. Only an administrator may.
func AscQueuePause(ctx *context.Context) { ascQueueSetPaused(ctx, true) }

// AscQueueResume resumes the queue of a branch and asks for a run. Only an administrator may.
func AscQueueResume(ctx *context.Context) { ascQueueSetPaused(ctx, false) }

// ascQueueStats fills the stat cards and the estimates. The counts come from the hub's own entries; the timing and the
// median come from asc and are left out when asc does not answer or does not know an entry.
func ascQueueStats(ctx *context.Context, repo *repo_model.Repository, branch string, view *automerge.AscQueueView) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	landedToday, refused := 0, 0
	for _, r := range view.Recent {
		if r.Entry.State == automerge.AscQueueMerged && r.Entry.UpdatedUnix >= startOfDay {
			landedToday++
		}
	}
	for _, r := range view.Active {
		if r.Entry.State == automerge.AscQueueBlocked {
			refused++
		}
	}
	ctx.Data["AscQueueLandedToday"] = landedToday
	// One point to a day for the last 14: the entries that landed, as far as the recent list reaches.
	perDay := make([]uint32, 14)
	for _, r := range view.Recent {
		if r.Entry.State != automerge.AscQueueMerged {
			continue
		}
		if back := int((startOfDay - int64(r.Entry.UpdatedUnix) + 86399) / 86400); back >= 0 && back < len(perDay) {
			perDay[len(perDay)-1-back]++
		}
	}
	ctx.Data["AscQueueLandedSpark"] = asctrust.Sparkline(perDay, 80, 28)
	ctx.Data["AscQueueRefused"] = refused
	timings := map[string]ascQueueTiming{}
	if name, err := ascGraphRepoName(repo); err == nil {
		if snap, err := ascQueueRead(ctx.Req.Context(), name, branch); err == nil {
			for _, t := range snap.Entries {
				timings[t.Change] = t
			}
			ctx.Data["AscQueueMedian"] = snap.Median
			if ctx.Doer != nil {
				// The live answer is for signed-in people; an anonymous visitor would only poll an empty one.
				ctx.Data["AscQueueLiveURL"] = ctx.Repo.RepoLink + "/asc/queue/live?branch=" + url.QueryEscape(branch)
			}
			if len(view.Active) > 0 {
				if first, ok := timings[view.Active[0].Entry.AscChange]; ok && first.ETA != "" {
					ctx.Data["AscQueueNext"] = first.ETA
				}
			}
		} else {
			log.Warn("asc queue: timing not available: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		}
	}
	ctx.Data["AscQueueTimings"] = timings
}
