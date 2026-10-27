// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"google.golang.org/grpc"
)

// The dependencies page (asc.v1.DepsService): known vulnerabilities in the manifests and lockfiles of trunk, newer
// versions, and the settings of the repository.
//
//	GET  /{owner}/{repo}/asc/deps                      views: findings (default), updates, config
//	POST /{owner}/{repo}/asc/deps/scan                 scan trunk and record the findings (administrators)
//	POST /{owner}/{repo}/asc/deps/propose              preview or create the update proposals (administrators)
//	POST /{owner}/{repo}/asc/deps/findings/{id}/dismiss  dismiss or reopen a finding (administrators)
//
// Reading needs a signed-in person who may read the code. A change is made by an administrator of the repository, the
// "maintainer" of asc, as that person (asctrust.UserCall), so asc decides again. A proposal never writes trunk: asc
// records the rewritten pins as changes on agent/deps/<group> under review, and the merge goes through the usual
// review. Every POST is behind the CSRF check of the router and the form carries the token. A lookup that failed is
// never drawn as clean: the page says how many packages could not be checked.
const (
	tplAscDeps     base.TplName = "repo/asc_deps"
	tplAscDepsPlan base.TplName = "repo/asc_deps_plan"

	maxDepsForm = 8 << 10

	depsViewFindings = "findings"
	depsViewUpdates  = "updates"
	depsViewConfig   = "config"
)

// ascDepsPage is the template data under .AscDeps.
type ascDepsPage struct {
	View      string
	Views     []ascDepsView
	DepsURL   string
	CanManage bool
	Op        string

	// Findings view.
	Findings   *asctrust.DepFindingsPage
	States     []ascDepsTab
	State      string
	Severity   string
	Ecosystem  string
	NextURL    string
	Severities []string
	Reasons    []string
	// Updates view.
	Updates      *asctrust.DepUpdates
	SecurityOnly bool
	Kinds        []string
	// Config view.
	Config *asctrust.DepsConfigView
	// Plan is a preview of a proposal.
	Plan *asctrust.DepPlan

	Problem     string
	Unsupported bool
}

type ascDepsView struct {
	Key    string
	URL    string
	Active bool
}

type ascDepsTab struct {
	Key    string
	URL    string
	Active bool
	Count  uint32
}

func ascDepsURL(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/asc/deps"
}

func ascDepsPageOf(ctx *context.Context, repo *repo_model.Repository, view string) *ascDepsPage {
	p := &ascDepsPage{View: view, DepsURL: ascDepsURL(repo), CanManage: ctx.Repo.IsAdmin(), Op: asctrust.NewOpID()}
	for _, k := range asctrust.DepSeverityKeys {
		p.Severities = append(p.Severities, k.Key)
	}
	for _, k := range asctrust.DepDismissReasons {
		p.Reasons = append(p.Reasons, k.Key)
	}
	for _, k := range asctrust.DepKindKeys {
		p.Kinds = append(p.Kinds, k.Key)
	}
	for _, v := range []string{depsViewFindings, depsViewUpdates, depsViewConfig} {
		u := p.DepsURL
		if v != depsViewFindings {
			u += "?view=" + v
		}
		p.Views = append(p.Views, ascDepsView{Key: v, URL: u, Active: v == view})
	}
	return p
}

// AscDeps serves GET /{owner}/{repo}/asc/deps.
func AscDeps(ctx *context.Context) {
	repo, name, ok := trustweb.CodeRepo(ctx, false)
	if !ok {
		return
	}
	trustweb.NoStore(ctx)
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.deps.title")
	ctx.Data["PageIsAscDeps"] = true
	view := ctx.FormString("view")
	switch view {
	case "":
		view = depsViewFindings
	case depsViewFindings, depsViewUpdates, depsViewConfig:
	default:
		ctx.NotFound("asc deps", nil)
		return
	}
	p := ascDepsPageOf(ctx, repo, view)
	ctx.Data["AscDeps"] = p
	if !asctrust.HasFeature(ctx, asctrust.FeatureDeps) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscDeps)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc deps: no connection to asc serve: %v", err)
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscDeps)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("deps credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscDeps)
		return
	}
	defer release()
	fail := func(op string, err error) {
		f := trustweb.FailureOf(op, err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc deps", nil)
			return
		}
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscDeps)
	}
	switch view {
	case depsViewUpdates:
		p.SecurityOnly = ctx.FormBool("security")
		up, err := asctrust.LoadDepUpdates(callCtx, conn, name, p.SecurityOnly)
		if err != nil {
			fail("deps updates", err)
			return
		}
		p.Updates = up
	case depsViewConfig:
		cfg, err := asctrust.LoadDepsConfig(callCtx, conn, name)
		if err != nil {
			fail("deps config", err)
			return
		}
		p.Config = cfg
	default:
		p.State, p.Severity, p.Ecosystem = ctx.FormString("state"), ctx.FormString("severity"), strings.TrimSpace(ctx.FormString("ecosystem"))
		page, err := asctrust.LoadDepFindings(callCtx, conn, name, asctrust.DepFindingsQuery{State: p.State, Severity: p.Severity, Ecosystem: p.Ecosystem, PageToken: ctx.FormString("page")})
		if err != nil {
			fail("deps findings", err)
			return
		}
		p.Findings = page
		cur := p.State
		if cur == "" {
			cur = "open"
		}
		for _, k := range asctrust.DepStateKeys {
			q := url.Values{}
			if k.Key != "open" {
				q.Set("state", k.Key)
			}
			if p.Severity != "" {
				q.Set("severity", p.Severity)
			}
			if p.Ecosystem != "" {
				q.Set("ecosystem", p.Ecosystem)
			}
			u := p.DepsURL
			if len(q) > 0 {
				u += "?" + q.Encode()
			}
			p.States = append(p.States, ascDepsTab{Key: k.Key, URL: u, Active: k.Key == cur, Count: page.Counts[k.Key]})
		}
		if page.Next != "" {
			q := url.Values{"page": {page.Next}}
			if p.State != "" {
				q.Set("state", p.State)
			}
			if p.Severity != "" {
				q.Set("severity", p.Severity)
			}
			if p.Ecosystem != "" {
				q.Set("ecosystem", p.Ecosystem)
			}
			p.NextURL = p.DepsURL + "?" + q.Encode()
		}
	}
	ctx.HTML(http.StatusOK, tplAscDeps)
}

// ascDepsFail answers a failed change: a message on the page the person came from, 403 and 404 as the status.
func ascDepsFail(ctx *context.Context, op, back string, err error) {
	f := trustweb.FailureOf(op, err)
	switch f.Status {
	case http.StatusForbidden:
		log.Warn("asc %s refused for user %d: %v", op, ctx.Doer.ID, err)
		ctx.Error(http.StatusForbidden, f.Localized(ctx))
	case http.StatusNotFound:
		ctx.NotFound("asc "+op, nil)
	default:
		ctx.Flash.Error(f.Localized(ctx))
		ctx.Redirect(back)
	}
}

// ascDepsWriter is the start of every change: the guards (an administrator of an asc repository, a form within its
// limit, a server with the feature), the connection and the credential of the person. On a refusal it has answered and
// returns false; otherwise the caller defers w.release().
type ascDepsWriter struct {
	repo    *repo_model.Repository
	name    string
	back    string
	conn    grpc.ClientConnInterface
	call    gocontext.Context
	release func()
}

func ascDepsWrite(ctx *context.Context, op string) (*ascDepsWriter, bool) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return nil, false
	}
	trustweb.NoStore(ctx)
	if !trustweb.ParseForm(ctx, maxDepsForm) {
		return nil, false
	}
	w := &ascDepsWriter{repo: repo, name: name, back: ascDepsURL(repo)}
	if !asctrust.HasFeature(ctx, asctrust.FeatureDeps) {
		ctx.NotFound("asc deps", nil)
		return nil, false
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc deps: no connection to asc serve: %v", err)
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(w.back)
		return nil, false
	}
	call, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		ascDepsFail(ctx, op+" credential", w.back, err)
		return nil, false
	}
	w.conn, w.call, w.release = conn, call, release
	return w, true
}

// AscDepsScan handles POST /{owner}/{repo}/asc/deps/scan with the optional field refresh (ignore the advisory cache).
func AscDepsScan(ctx *context.Context) {
	w, ok := ascDepsWrite(ctx, "deps scan")
	if !ok {
		return
	}
	defer w.release()
	scan, err := asctrust.ScanDeps(w.call, w.conn, w.name, ctx.FormBool("refresh"))
	if err != nil {
		ascDepsFail(ctx, "deps scan", w.back, err)
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_trust.deps.scanned", scan.Packages, scan.Findings))
	if scan.LookupKey != "complete" {
		ctx.Flash.Warning(ctx.Locale.TrString("asc_trust.deps.lookup_"+scan.LookupKey, scan.Unknown))
	}
	if scan.Truncated {
		ctx.Flash.Warning(ctx.Locale.TrString("asc_trust.deps.truncated"))
	}
	ctx.Redirect(w.back)
}

// AscDepsPropose handles POST /{owner}/{repo}/asc/deps/propose with the fields mode (preview or apply), security,
// max_kind and, for apply, op. A preview shows the plan and the form that applies exactly it.
func AscDepsPropose(ctx *context.Context) {
	w, ok := ascDepsWrite(ctx, "deps propose")
	if !ok {
		return
	}
	defer w.release()
	mode := ctx.FormString("mode")
	if mode != "preview" && mode != "apply" {
		ctx.Error(http.StatusBadRequest)
		return
	}
	q := asctrust.DepProposeQuery{SecurityOnly: ctx.FormBool("security"), MaxKind: ctx.FormString("max_kind"), DryRun: mode == "preview", Op: ctx.FormString("op")}
	back := w.back + "?view=" + depsViewUpdates
	plan, err := asctrust.ProposeDeps(w.call, w.conn, w.name, q)
	if err != nil {
		ascDepsFail(ctx, "deps propose", back, err)
		return
	}
	if q.DryRun {
		ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.deps.plan_title")
		ctx.Data["PageIsAscDeps"] = true
		p := ascDepsPageOf(ctx, w.repo, depsViewUpdates)
		p.Plan, p.SecurityOnly = plan, q.SecurityOnly
		ctx.Data["AscDeps"] = p
		ctx.Data["AscDepsMaxKind"] = q.MaxKind
		ctx.HTML(http.StatusOK, tplAscDepsPlan)
		return
	}
	if len(plan.Proposals) == 0 {
		ctx.Flash.Info(ctx.Locale.TrString("asc_trust.deps.nothing_to_propose"))
	} else {
		refs := make([]string, 0, len(plan.Proposals))
		for _, pr := range plan.Proposals {
			refs = append(refs, pr.Ref)
		}
		ctx.Flash.Success(ctx.Tr("asc_trust.deps.proposed", len(plan.Proposals), strings.Join(refs, ", ")))
	}
	ctx.Redirect(back)
}

// AscDepsDismiss handles POST /{owner}/{repo}/asc/deps/findings/{id}/dismiss with the fields reason (a key of
// the reasons, or reopen), note and op.
func AscDepsDismiss(ctx *context.Context) {
	w, ok := ascDepsWrite(ctx, "deps dismiss")
	if !ok {
		return
	}
	defer w.release()
	id, err := strconv.ParseUint(ctx.Params("id"), 10, 64)
	if err != nil || id == 0 {
		ctx.NotFound("asc deps", nil)
		return
	}
	back := w.back
	// The state tab the person came from is kept, from a fixed set of values.
	switch ctx.FormString("state") {
	case "dismissed", "fixed":
		back += "?state=" + ctx.FormString("state")
	}
	row, err := asctrust.DismissDep(w.call, w.conn, w.name, id, ctx.FormString("reason"), trustweb.Lines(ctx.FormString("note")), ctx.FormString("op"))
	if err != nil {
		ascDepsFail(ctx, "deps dismiss", back, err)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_trust.deps.dismiss_done."+row.StateKey, row.Name))
	ctx.Redirect(back)
}
