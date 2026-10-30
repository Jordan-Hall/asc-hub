// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"encoding/hex"
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

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// The "Secret scanning" page of a repository (docs/BUILDOUT.md section 4): /{owner}/{repo}/settings/asc/security.
//
// Every push that lands on trunk is scanned before the ref moves. This page lists the rules, the findings and the
// pushes that were refused, lets an administrator work a finding off (revoked, false positive, accepted risk) or
// scan a revision on demand, and edits the allowlist (`[secrets] allow_paths` of the policy) the only way it can
// be edited: as a proposal that becomes a reviewed change.
//
// A finding never carries a secret. asc sends a keyed fingerprint and a preview of at most two characters, and
// the hub cuts the preview again before it draws it. The refusal of a push names the rule and the place, never
// the value.
const (
	tplAscSecurity base.TplName = "repo/settings/asc_security"

	maxSecurityForm = 64 << 10
)

// ascSecurityPage is the template data under .AscSecurity.
type ascSecurityPage struct {
	Data        *asctrust.SecretsPage
	Blocked     []asctrust.AuditRow
	State       string
	States      []ascStateOption
	NextURL     string
	Allowlist   string
	AllowOp     string
	Base        string
	Ops         map[uint64]string
	Problem     string
	Unsupported bool
	PolicyURL   string
	SecurityURL string
	AuditURL    string
	Branch      string
	Proposed    *ascProposed
	// AllRules is set when the rules table shows every rule; by default it shows the first few.
	AllRules bool
}

type ascStateOption struct {
	Key, Label string
	Count      uint32
	Active     bool
}

func ascSecurityPageFor(ctx *context.Context, repo *repo_model.Repository) *ascSecurityPage {
	p := &ascSecurityPage{AllowOp: asctrust.NewOpID(), Ops: map[uint64]string{}, Branch: repo.DefaultBranch}
	p.PolicyURL, p.SecurityURL, p.AuditURL, _ = ascPolicyLinks(repo)
	p.SecurityURL = setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/settings/asc/security"
	ctx.Data["Title"] = ctx.Tr("asc_shell.sn_secret_scanning")
	ctx.Data["PageIsSettingsAscSecurity"] = true
	ctx.Data["AscSecurity"] = p
	trustweb.NoStore(ctx)
	return p
}

// AscSecurity serves GET /{owner}/{repo}/settings/asc/security with ?state= and ?page=.
func AscSecurity(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	p := ascSecurityPageFor(ctx, repo)
	ascSecurityRender(ctx, repo, name, p, ctx.FormString("state"), ctx.FormString("page"))
}

func ascSecurityRender(ctx *context.Context, repo *repo_model.Repository, name string, p *ascSecurityPage, state, pageToken string) {
	status := http.StatusOK
	if p.Problem != "" {
		status = http.StatusBadRequest
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureSecretScan) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscSecurity)
		return
	}
	if state == "" {
		state = "open"
	}
	p.State = state
	p.AllRules = ctx.FormString("rules") == "all"
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc security: no connection to asc serve: %v", err)
		p.Problem = "The ASC server is not reachable right now."
		ctx.HTML(http.StatusBadGateway, tplAscSecurity)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		f := trustweb.FailureOf("security credential", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscSecurity)
		return
	}
	defer release()

	data, err := asctrust.LoadSecrets(callCtx, conn, name, asctrust.SecretsQuery{State: state, PageToken: pageToken})
	if err != nil {
		f := trustweb.FailureOf("security load", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc security", nil)
			return
		}
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscSecurity)
		return
	}
	p.Data = data
	p.Allowlist = strings.Join(data.Rules.AllowPaths, "\n")
	for _, row := range data.Findings.Rows {
		p.Ops[row.ID] = asctrust.NewOpID()
	}
	for _, c := range data.Findings.Counts {
		p.States = append(p.States, ascStateOption{Key: c.Key, Label: c.Label, Count: c.Count, Active: c.Key == state})
	}
	if data.Findings.Next != "" {
		p.NextURL = p.SecurityURL + "?" + url.Values{"state": {state}, "page": {data.Findings.Next}}.Encode()
	}
	// The base of an allowlist proposal is the policy in force now.
	if page, err := asctrust.LoadPolicyPage(callCtx, conn, name); err == nil {
		p.Base = hex.EncodeToString(page.Base)
	}
	// The pushes that were refused are in the audit log; the list is advice, so its failure is not the page's.
	if res, err := asctrust.ListAudit(callCtx, conn, &ascv1.OwnerRef{Kind: ascv1.OwnerKind_OWNER_KIND_REPO, Name: name}, asctrust.AuditQuery{Limit: 10},
		&ascv1.AuditFilter{Actions: []string{"secret.push.blocked"}}); err == nil {
		p.Blocked = res.Page.Rows
	}
	ctx.HTML(status, tplAscSecurity)
}

// AscSecurityFinding serves POST /{owner}/{repo}/settings/asc/security/finding with the fields id, state, note, op.
func AscSecurityFinding(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxSecurityForm) {
		return
	}
	p := ascSecurityPageFor(ctx, repo)
	id, err := strconv.ParseUint(ctx.Req.PostFormValue("id"), 10, 64)
	if err != nil || id == 0 {
		if ascWantsJSON(ctx) {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]any{"ok": false, "message": "That finding does not exist."})
			return
		}
		p.Problem = "That finding does not exist."
		ascSecurityRender(ctx, repo, name, p, "", "")
		return
	}
	problem := ascSecurityUpdate(ctx, repo, name, id)
	if ascWantsJSON(ctx) {
		if problem != "" {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]any{"ok": false, "message": problem})
			return
		}
		ctx.JSON(http.StatusOK, map[string]any{"ok": true})
		return
	}
	back := p.SecurityURL
	if s := ctx.Req.PostFormValue("back"); s != "" {
		if _, known := asctrust.SecretStateOf(s); known {
			back += "?" + url.Values{"state": {s}}.Encode()
		}
	}
	if problem != "" {
		ctx.Flash.Error(problem)
	} else {
		ctx.Flash.Success("The finding was updated.")
	}
	ctx.Redirect(back)
}

func ascSecurityUpdate(ctx *context.Context, repo *repo_model.Repository, name string, id uint64) string {
	conn, err := asctrust.Conn()
	if err != nil {
		return "The ASC server is not reachable right now."
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		return trustweb.FailureOf("security credential", err).Message
	}
	defer release()
	if _, err := asctrust.UpdateFinding(callCtx, conn, name, id, ctx.Req.PostFormValue("state"), trustweb.Lines(ctx.Req.PostFormValue("note")), ctx.Req.PostFormValue("op")); err != nil {
		return trustweb.FailureOf("security update", err).Message
	}
	return ""
}

// AscSecurityScan serves POST /{owner}/{repo}/settings/asc/security/scan: scan the default branch now.
func AscSecurityScan(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxSecurityForm) {
		return
	}
	p := ascSecurityPageFor(ctx, repo)
	msg, problem := ascSecurityScanRun(ctx, repo, name)
	if ascWantsJSON(ctx) {
		if problem != "" {
			ctx.JSON(http.StatusUnprocessableEntity, map[string]any{"ok": false, "message": problem})
			return
		}
		ctx.JSON(http.StatusOK, map[string]any{"ok": true, "message": msg})
		return
	}
	if problem != "" {
		ctx.Flash.Error(problem)
	} else {
		ctx.Flash.Success(msg)
	}
	ctx.Redirect(p.SecurityURL)
}

// ascWantsJSON is true for the script of the page, which shows the answer in place instead of reloading.
func ascWantsJSON(ctx *context.Context) bool {
	return strings.Contains(ctx.Req.Header.Get("Accept"), "application/json")
}

// ascSecurityScanRun scans the default branch of repo and says how it went, or what went wrong.
func ascSecurityScanRun(ctx *context.Context, repo *repo_model.Repository, name string) (msg, problem string) {
	conn, err := asctrust.Conn()
	if err != nil {
		return "", "The ASC server is not reachable right now."
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		return "", trustweb.FailureOf("security credential", err).Message
	}
	defer release()
	// The revision is always the default branch of the repository, never a value of the request.
	if repo.DefaultBranch == "" {
		return "", "This repository has no branch to scan yet."
	}
	sum, err := asctrust.ScanRev(callCtx, conn, name, "refs/heads/"+repo.DefaultBranch)
	if err != nil {
		return "", trustweb.FailureOf("security scan", err).Message
	}
	msg = "Scanned " + strconv.FormatUint(uint64(sum.FilesScanned), 10) + " files: " + strconv.Itoa(sum.Findings) + " findings."
	if sum.Truncated {
		msg += " The scan reached its limit, so some files were not read."
	}
	return msg, ""
}

// AscSecurityAllowlist serves POST /{owner}/{repo}/settings/asc/security/allowlist: the paths that secret scanning
// skips are edited as a proposal to the policy file, so a change of the allowlist is reviewed like code. Only the
// policy in force on trunk counts: an allowlist in a change under review has no effect until it lands.
func AscSecurityAllowlist(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxSecurityForm) {
		return
	}
	p := ascSecurityPageFor(ctx, repo)
	globs, err := asctrust.ParseGlobLines(trustweb.Lines(ctx.Req.PostFormValue("paths")))
	if err != nil {
		p.Problem = trustweb.FailureOf("allowlist", err).Message
		ascSecurityRender(ctx, repo, name, p, "", "")
		return
	}
	text, baseHex, problem := ascCurrentPolicy(ctx, repo, name)
	if problem == "" && text == "" {
		problem = "This repository has no policy file yet. Add one on the policy page first."
	}
	if problem != "" {
		p.Problem = problem
		ascSecurityRender(ctx, repo, name, p, "", "")
		return
	}
	newText := asctrust.TomlSetStrings(text, "secrets", "allow_paths", globs)
	message := "policy: set secrets.allow_paths"
	redirect, proposed, problem := ascPolicyPropose(ctx, repo, name, newText, message, ctx.Req.PostFormValue("op"), baseHex)
	switch {
	case redirect != "":
		ctx.Redirect(redirect)
	case proposed != nil:
		ctx.Flash.Success("The change " + proposed.Change + " was made and is under review. It will get its pull request as soon as the git view of the repository has it.")
		ctx.Redirect(p.SecurityURL)
	default:
		p.Problem = problem
		ascSecurityRender(ctx, repo, name, p, "", "")
	}
}
