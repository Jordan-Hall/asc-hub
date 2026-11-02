// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"strconv"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The "Path rules" page of a repository: /{owner}/{repo}/settings/asc/path-rules. In a big repository each person, team,
// agent, CI job or deploy key sees only the folders it is allowed to: a folder it may not read is shown to it as
// restricted, by name only, in clones, on the web and in search. The page is for administrators of the repository; asc
// checks the hub's claim (role admin, this repository) again on every call.
//
// The whole rule set is edited at once and saved with the revision it was read at, so two administrators never overwrite
// each other. "Preview as" shows what one viewer would get on some paths under the stored rules.
const (
	tplAscPathRules base.TplName = "repo/settings/asc_path_rules"

	// maxPathRulesForm bounds the form: 512 rows of a directory, a subject and a level, and a preview.
	maxPathRulesForm = 1 << 20
	// pathRulesBlankRows are the empty rows offered for new rules.
	pathRulesBlankRows = 3
)

type ascPathRulesPage struct {
	Rules       []asctrust.AccessRule
	Revision    uint64
	Updated     string
	UpdatedBy   string
	Levels      []string
	SaveOp      string
	Problem     string
	Unsupported bool
	PageURL     string
	// The preview form as typed, and its answer.
	Principal string
	RepoLevel string
	Paths     string
	Preview   *asctrust.PathPreview
}

func ascPathRulesLink(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/settings/asc/path-rules"
}

func ascPathRulesPageFor(ctx *context.Context, repo *repo_model.Repository) *ascPathRulesPage {
	p := &ascPathRulesPage{Levels: asctrust.PathRuleLevels, SaveOp: asctrust.NewOpID(), PageURL: ascPathRulesLink(repo), RepoLevel: "read"}
	ctx.Data["Title"] = ctx.Tr("asc_review.pr_title")
	ctx.Data["PageIsSettingsAscPathRules"] = true
	ctx.Data["AscPathRules"] = p
	trustweb.NoStore(ctx)
	return p
}

// ascPathRulesLoad reads the stored rules into p, plus blank rows. It answers the request and returns false on failure.
func ascPathRulesLoad(ctx *context.Context, p *ascPathRulesPage, repo *repo_model.Repository, name string) bool {
	conn, err := asctrust.Conn()
	if err != nil {
		p.Problem = "The ASC server is not reachable right now."
		ctx.HTML(http.StatusBadGateway, tplAscPathRules)
		return false
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		f := trustweb.FailureOf("path rules credential", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscPathRules)
		return false
	}
	defer release()
	set, err := asctrust.ListPathRules(callCtx, conn, name)
	if err != nil {
		if asctrust.Classify(err).Kind == asctrust.KindUnimplemented {
			p.Unsupported = true
			ctx.HTML(http.StatusOK, tplAscPathRules)
			return false
		}
		f := trustweb.FailureOf("path rules", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscPathRules)
		return false
	}
	p.Rules = set.Rules
	p.Revision = set.Revision
	p.UpdatedBy = set.UpdatedBy
	if !set.Updated.IsZero() {
		p.Updated = set.Updated.UTC().Format("2006-01-02 15:04 UTC")
	}
	for range pathRulesBlankRows {
		p.Rules = append(p.Rules, asctrust.AccessRule{Level: "none"})
	}
	return true
}

// AscPathRules serves GET /{owner}/{repo}/settings/asc/path-rules.
func AscPathRules(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	p := ascPathRulesPageFor(ctx, repo)
	if ascPathRulesLoad(ctx, p, repo, name) {
		ctx.HTML(http.StatusOK, tplAscPathRules)
	}
}

// pathRulesFromForm reads the rows of the form: the fields dir, subject and level repeat once per row, in order. A row
// without a subject is an empty row and is dropped; a directory of "/" or nothing is the root.
func pathRulesFromForm(ctx *context.Context) ([]asctrust.AccessRule, bool) {
	dirs, subjects, levels := ctx.Req.PostForm["dir"], ctx.Req.PostForm["subject"], ctx.Req.PostForm["level"]
	if len(dirs) != len(subjects) || len(dirs) != len(levels) || len(dirs) > asctrust.MaxPathRules+pathRulesBlankRows {
		return nil, false
	}
	var rules []asctrust.AccessRule
	for i := range dirs {
		subject := strings.TrimSpace(subjects[i])
		if subject == "" {
			continue
		}
		rules = append(rules, asctrust.AccessRule{Dir: asctrust.CleanRuleDir(dirs[i]), Subject: subject, Level: strings.TrimSpace(levels[i])})
	}
	return rules, true
}

// AscPathRulesSave serves POST .../path-rules with the rows, revision and op.
func AscPathRulesSave(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxPathRulesForm) {
		return
	}
	back := ascPathRulesLink(repo)
	rules, ok := pathRulesFromForm(ctx)
	if !ok {
		ctx.Flash.Error(ctx.Tr("asc_review.pr_bad_form"))
		ctx.Redirect(back)
		return
	}
	revision, err := strconv.ParseUint(ctx.Req.PostFormValue("revision"), 10, 64)
	if err != nil {
		ctx.Flash.Error(ctx.Tr("asc_review.pr_bad_form"))
		ctx.Redirect(back)
		return
	}
	if err := asctrust.CheckPathRules(rules); err != nil {
		// The rows stay on the page so the person fixes the one named instead of typing them all again.
		p := ascPathRulesPageFor(ctx, repo)
		p.Rules, p.Revision, p.SaveOp = rules, revision, ctx.Req.PostFormValue("op")
		for range pathRulesBlankRows {
			p.Rules = append(p.Rules, asctrust.AccessRule{Level: "none"})
		}
		p.Problem = trustweb.FailureOf("path rules check", err).Message
		ctx.HTML(http.StatusBadRequest, tplAscPathRules)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error("The ASC server is not reachable right now.")
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("path rules credential", err).Message)
		ctx.Redirect(back)
		return
	}
	defer release()
	if _, err := asctrust.SetPathRules(callCtx, conn, name, rules, revision, ctx.Req.PostFormValue("op")); err != nil {
		if asctrust.Classify(err).Kind == asctrust.KindConflict {
			ctx.Flash.Error(ctx.Tr("asc_review.pr_stale"))
		} else {
			ctx.Flash.Error(trustweb.FailureOf("path rules set", err).Message)
		}
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_review.pr_saved", len(rules)))
	ctx.Redirect(back)
}

// AscPathRulesPreview serves POST .../path-rules/preview with principal, repo_level and paths (one per line).
func AscPathRulesPreview(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxPathRulesForm) {
		return
	}
	p := ascPathRulesPageFor(ctx, repo)
	if !ascPathRulesLoad(ctx, p, repo, name) {
		return
	}
	p.Principal = strings.TrimSpace(ctx.Req.PostFormValue("principal"))
	p.RepoLevel = strings.TrimSpace(ctx.Req.PostFormValue("repo_level"))
	p.Paths = ctx.Req.PostFormValue("paths")
	var paths []string
	for _, l := range strings.Split(p.Paths, "\n") {
		if l = asctrust.CleanRuleDir(l); l != "" {
			paths = append(paths, l)
		}
	}
	conn, err := asctrust.Conn()
	if err != nil {
		p.Problem = "The ASC server is not reachable right now."
		ctx.HTML(http.StatusBadGateway, tplAscPathRules)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		f := trustweb.FailureOf("path rules credential", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscPathRules)
		return
	}
	defer release()
	preview, err := asctrust.PreviewPathAccess(callCtx, conn, name, p.Principal, p.RepoLevel, paths)
	if err != nil {
		f := trustweb.FailureOf("path rules preview", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscPathRules)
		return
	}
	p.Preview = preview
	ctx.HTML(http.StatusOK, tplAscPathRules)
}
