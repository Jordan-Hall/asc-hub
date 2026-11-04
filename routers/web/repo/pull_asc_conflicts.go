// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	gocontext "context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// Conflicts of a stacked pull request with its target (board Review, "Conflict with main"): the banner on the pull
// request, the page of both sides (GET /pulls/{index}/asc/conflicts), the editor that settles them (?edit=1, POST
// the same path) and the request to the agent that made the change (POST /pulls/{index}/asc/ask_resolve).

const tplAscConflicts base.TplName = "repo/pulls/asc_conflicts"

const (
	// ascBannerPreviewBytes bounds the marked text read for the preview of the banner.
	ascBannerPreviewBytes = 64 << 10
	// ascBannerPreviewLines bounds the lines of each side in the preview.
	ascBannerPreviewLines = 6
)

// ascBannerReadFile reads the marked text of the preview. Tests replace it.
var ascBannerReadFile = ascbridge.ReadFileByID

// ascConflictReadCtx is the call context the text of conflicting files is read with: the claim of the viewer, so asc
// applies the path rules of the repository to them. It is nil (path and kind only) for a viewer who cannot read code.
func ascConflictReadCtx(ctx *context.Context) (gocontext.Context, func()) {
	if !ctx.Repo.CanRead(unit.TypeCode) {
		return nil, func() {}
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, ctx.Repo.Repository)
	if err != nil {
		return nil, func() {}
	}
	return callCtx, release
}

// ascConflictBanner fills the conflict banner of m for the pull request page. It never fails the page: what it
// cannot learn is left out.
func ascConflictBanner(ctx *context.Context, pull *issues_model.PullRequest, m *AscMergeBadge, repoName string) {
	m.Target = pull.BaseBranch
	m.Others = max(len(m.Conflicts)-1, 0) + m.MoreConflicts
	if pull.Issue != nil && pull.Issue.LoadRepo(ctx) == nil {
		m.Link = pull.Issue.Link() + "/asc/conflicts"
		m.AskLink = pull.Issue.Link() + "/asc/ask_resolve"
	}
	if panel, _ := ctx.Data["AscStackPanel"].(*pull_service.AscStackPanel); panel != nil {
		for _, e := range panel.Entries {
			if e.Current {
				m.Position = e.Position
				m.Free = max(e.Position-1, 0)
			}
		}
	}
	if ctx.Doer != nil {
		ok, err := pull_service.AscMayRewrite(ctx, ctx.Doer, pull)
		m.CanResolve = err == nil && ok
	}
	if m.CanResolve {
		m.Agent = ascSuggestionAgent(ctx, pull)
	}
	if m.markedID == "" {
		return
	}
	readCtx, release := ascConflictReadCtx(ctx)
	defer release()
	if readCtx == nil {
		return
	}
	data, _, err := ascBannerReadFile(readCtx, repoName, m.markedID, ascBannerPreviewBytes)
	if err != nil || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return
	}
	// A truncated text still has its first hunk when the hunk closes within the bound.
	if hunks := pull_service.ParseAscConflictHunks(string(data)); len(hunks) > 0 {
		h := hunks[0]
		m.PreviewLine = h.Line
		for i, l := range h.Yours[:min(len(h.Yours), ascBannerPreviewLines)] {
			m.Preview = append(m.Preview, AscConflictRow{Line: h.Line + 1 + i, Add: true, Text: l})
		}
		for i, l := range h.Main[:min(len(h.Main), ascBannerPreviewLines)] {
			m.Preview = append(m.Preview, AscConflictRow{Line: h.Line + 1 + i, Text: l, Target: i == 0})
		}
	}
}

// ascConflictsTarget loads the pull request of the conflicts page. The page shows code, so reading needs the pull
// request and the code units. It reports false after it has answered.
func ascConflictsTarget(ctx *context.Context) (*issues_model.Issue, bool) {
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) || !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("asc conflicts", nil)
		return nil, false
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return nil, false
	}
	return issue, true
}

// ascMayResolve reports whether the viewer may settle the conflicts of pull.
func ascMayResolve(ctx *context.Context, pull *issues_model.PullRequest) (bool, error) {
	if ctx.Doer == nil || ctx.Repo.Repository.IsArchived {
		return false, nil
	}
	return pull_service.AscMayRewrite(ctx, ctx.Doer, pull)
}

// AscConflicts handles GET /{owner}/{repo}/pulls/{index}/asc/conflicts: both sides of every conflicting file of the
// change, and with ?edit=1 the editor for a viewer who may resolve.
func AscConflicts(ctx *context.Context) {
	issue, ok := ascConflictsTarget(ctx)
	if !ok {
		return
	}
	ascRenderConflicts(ctx, issue, ctx.FormBool("edit"), nil, "")
}

// ascRenderConflicts renders the conflicts page. kept holds the text the viewer sent (by path) to show again in the
// editor; failure is a translated message shown above it.
func ascRenderConflicts(ctx *context.Context, issue *issues_model.Issue, edit bool, kept map[string]string, failure string) {
	pull := issue.PullRequest
	readCtx, release := ascConflictReadCtx(ctx)
	defer release()
	conflicts, err := pull_service.AscPullConflicts(ctx, readCtx, pull)
	if err != nil {
		ascOpFlashFail(ctx, ascOpRestack, issue, err)
		return
	}
	canResolve, err := ascMayResolve(ctx, pull)
	if err != nil {
		ctx.ServerError("AscMayRewrite", err)
		return
	}
	if conflicts.State != ascbridge.RestackConflict {
		edit = false
	}
	if edit && !canResolve {
		ctx.Error(http.StatusForbidden)
		return
	}
	editable := 0
	for i := range conflicts.Files {
		if text, ok := kept[conflicts.Files[i].Path]; ok && conflicts.Files[i].Editable {
			conflicts.Files[i].Marked = text
		}
		if conflicts.Files[i].Editable {
			editable++
		}
	}
	ctx.Data["AscEditable"] = editable
	ctx.Data["Title"] = ctx.Tr("repo.pulls.asc_conflicts.title", issue.Index, pull.BaseBranch)
	ctx.Data["PageIsPullList"] = true
	ctx.Data["AscConflicts"] = conflicts
	ctx.Data["AscBack"] = issue.Link()
	ctx.Data["AscTarget"] = pull.BaseBranch
	ctx.Data["AscEdit"] = edit
	ctx.Data["AscCanResolve"] = canResolve && conflicts.State == ascbridge.RestackConflict
	ctx.Data["AscFailure"] = failure
	if canResolve {
		ctx.Data["AscAgent"] = ascSuggestionAgent(ctx, pull)
	}
	if conflicts.Below != nil {
		if err := conflicts.Below.LoadIssue(ctx); err == nil && conflicts.Below.Issue.LoadRepo(ctx) == nil {
			ctx.Data["AscBelow"] = conflicts.Below.Issue
		}
	}
	ctx.HTML(http.StatusOK, tplAscConflicts)
}

// ascResolveForm reads the resolutions of the editor: "path" and "content" pairs, one per file.
func ascResolveForm(ctx *context.Context) (map[string]string, bool) {
	if err := ctx.Req.ParseForm(); err != nil {
		return nil, false
	}
	paths, contents := ctx.Req.PostForm["path"], ctx.Req.PostForm["content"]
	if len(paths) == 0 || len(paths) != len(contents) || len(paths) > pull_service.AscConflictMaxFiles {
		return nil, false
	}
	out := make(map[string]string, len(paths))
	for i, p := range paths {
		if _, dup := out[p]; dup || p == "" {
			return nil, false
		}
		out[p] = contents[i]
	}
	return out, true
}

// AscResolveConflicts handles POST /{owner}/{repo}/pulls/{index}/asc/conflicts: the stack is restacked with the text
// the viewer wrote for each conflicting file. The service checks who may (as for Restack) and what (only the files
// that conflict now, and never with a marker left in them).
func AscResolveConflicts(ctx *context.Context) {
	issue, pr, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	contents, ok := ascResolveForm(ctx)
	if !ok {
		ctx.Error(http.StatusBadRequest)
		return
	}
	readCtx, release := ascConflictReadCtx(ctx)
	defer release()
	res, err := pull_service.ResolveAscConflicts(ctx, readCtx, ctx.Doer, pr, contents)
	switch {
	case err == nil:
		ctx.Flash.Success(ctx.Tr("repo.pulls.asc_conflicts.resolved", len(res.Moved)))
		ctx.Redirect(issue.Link())
	case res != nil && len(res.Failed) > 0:
		log.Error("asc resolve of pull request %d: %v", pr.ID, err)
		ctx.Flash.Error(ctx.Tr("repo.pulls.asc_op.err.restack_partial", len(res.Failed)))
		ctx.Redirect(issue.Link())
	case errors.Is(err, pull_service.ErrAscNoConflict):
		ctx.Flash.Info(ctx.Tr("repo.pulls.asc_conflicts.none_left"))
		ctx.Redirect(issue.Link())
	case errors.Is(err, pull_service.ErrAscResolutionMarkers):
		// The text is shown again, so nothing the viewer wrote is lost.
		ascRenderConflicts(ctx, issue, true, contents, ctx.Locale.TrString("repo.pulls.asc_conflicts.markers_left"))
	case errors.Is(err, pull_service.ErrAscResolutionPath):
		ascRenderConflicts(ctx, issue, true, contents, ctx.Locale.TrString("repo.pulls.asc_conflicts.changed"))
	default:
		ascOpFlashFail(ctx, ascOpRestack, issue, err)
	}
}

// ascResolveTask is what the agent is asked to do. Paths are checked by ascSuggestionScope.
func ascResolveTask(target string, paths []string) string {
	return fmt.Sprintf("This change conflicts with %q. Restack it onto the current %q and resolve the conflicts in "+
		"these files, keeping what both sides meant: %s. Change nothing else. Push the result as a new version of "+
		"the change to its branch. When a conflict cannot be resolved without a decision of a person, push nothing "+
		"and explain why.", target, target, strings.Join(paths, ", "))
}

// AscAskResolve handles POST /{owner}/{repo}/pulls/{index}/asc/ask_resolve: it starts a run of the agent that made
// the change, as the doer, to resolve its conflicts with the target. The run may write the conflicting files only.
func AscAskResolve(ctx *context.Context) {
	issue, pull, ok := ascStackTarget(ctx)
	if !ok {
		return
	}
	allowed, err := pull_service.AscMayRewrite(ctx, ctx.Doer, pull)
	if err != nil {
		ctx.ServerError("AscMayRewrite", err)
		return
	}
	if !allowed {
		ctx.Error(http.StatusForbidden)
		return
	}
	agent := ascSuggestionAgent(ctx, pull)
	if agent == "" {
		ctx.NotFound("no agent made the change", nil)
		return
	}
	if open, err := pull_service.AscPullIsOpen(ctx, pull); err != nil || !open {
		ctx.Flash.Error(ctx.Tr("repo.pulls.asc_versions.restore_not_open"))
		ctx.Redirect(issue.Link())
		return
	}
	readCtx, release := ascConflictReadCtx(ctx)
	defer release()
	conflicts, err := pull_service.AscPullConflicts(ctx, readCtx, pull)
	if err != nil {
		ascOpFlashFail(ctx, ascOpRestack, issue, err)
		return
	}
	if conflicts.State != ascbridge.RestackConflict || len(conflicts.Files) == 0 || conflicts.MoreFiles > 0 {
		key := "repo.pulls.asc_conflicts.none_left"
		if conflicts.MoreFiles > 0 {
			key = "repo.pulls.asc_conflicts.ask_too_many"
		}
		ctx.Flash.Info(ctx.Tr(key))
		ctx.Redirect(issue.Link())
		return
	}
	paths := make([]string, 0, len(conflicts.Files))
	for _, f := range conflicts.Files {
		scope, ok := ascSuggestionScope(f.Path)
		if !ok {
			ctx.Flash.Error(ctx.Tr("repo.pulls.asc_conflicts.ask_path"))
			ctx.Redirect(issue.Link())
			return
		}
		paths = append(paths, scope...)
	}
	req := &ascv1.CreateAgentRunRequest{
		Agent:     agent,
		Title:     fmt.Sprintf("Resolve the conflicts of #%d with %s", issue.Index, pull.BaseBranch),
		Task:      ascResolveTask(pull.BaseBranch, paths),
		Target:    pull.HeadBranch,
		Paths:     paths,
		Limits:    &ascv1.AgentBudgetLimits{MaxPaths: uint32(len(paths))},
		ForChange: ascagentui.ChangeBytes(conflicts.Change),
	}
	switch err := ascSuggestionRun(ctx, ctx.Doer, ctx.Repo.Repository, req); {
	case errors.Is(err, asctrust.ErrForbidden):
		ctx.Error(http.StatusForbidden)
		return
	case err != nil:
		log.Warn("asc ask resolve: run of %s for pull request %d: %v", agent, pull.ID, err)
		ctx.Flash.Error(ctx.Tr("repo.pulls.asc_conflicts.ask_failed"))
	default:
		log.Info("asc audit: ask resolve: repository %d, pull request %d: user %d asked %s to resolve %d files",
			ctx.Repo.Repository.ID, pull.ID, ctx.Doer.ID, agent, len(paths))
		ctx.Flash.Success(ctx.Tr("repo.pulls.asc_conflicts.asked", agent))
	}
	ctx.Redirect(issue.Link())
}
