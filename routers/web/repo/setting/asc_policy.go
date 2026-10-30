// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"encoding/hex"
	"errors"
	"net/http"
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

// The "Policy and trust" page of a repository (docs/BUILDOUT.md section 1): /{owner}/{repo}/settings/asc/policy.
//
// The rules live in the repository as .asc/policy.toml, so they are reviewed like code and travel with a fork. The
// page shows the file in force, says in plain sentences what it means, computes who can write where, and edits
// the file the only way it can be edited: a proposal becomes an ordinary change under review, with a pull request
// that a maintainer lands. The page never writes trunk and holds no policy of its own.
//
// Trust. The page is for administrators of the repository (Forgejo checks, then asc checks again with a
// short-lived credential of the person). The draft is validated by asc before it can be proposed, and the proposal
// is made on the changeset the page was drawn from, so a trunk that moved is a conflict and never an overwrite.
// A texts from asc is cleaned and escaped; the text of an asc failure is never shown.
const (
	tplAscPolicy base.TplName = "repo/settings/asc_policy"

	maxPolicyForm = 3*asctrust.MaxPolicyBytes + 8<<10
)

// ascPullSeams are the parts of opening the pull request of a proposal. Tests replace them.
var ascPullSeams = asctrust.DefaultPullSeams

// ascPolicyPage is the template data under .AscPolicy.
type ascPolicyPage struct {
	View        *asctrust.PolicyView
	Draft       string
	Dirty       bool
	Preview     *asctrust.Preview
	Message     string
	Problem     string
	Unsupported bool
	Op          string
	ToggleOps   map[string]string
	Base        string
	PolicyURL   string
	SecurityURL string
	AuditURL    string
	ChecksURL   string
	// Org is what the organisation that owns the repository requires of it; nil for a repository of a person.
	Org *asctrust.OrgPolicyView
	// Proposed is set when the change was made but its pull request could not be opened yet.
	Proposed *ascProposed
}

type ascProposed struct {
	Change string
}

func ascPolicyLinks(repo *repo_model.Repository) (policy, security, audit, checks string) {
	root := setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name
	return root + "/settings/asc/policy", root + "/settings/asc/security", root + "/settings/asc/audit", root + "/asc/checks"
}

func ascPolicyPageFor(ctx *context.Context, repo *repo_model.Repository) *ascPolicyPage {
	p := &ascPolicyPage{ToggleOps: map[string]string{}, Op: asctrust.NewOpID()}
	for key := range asctrust.Toggles {
		p.ToggleOps[key] = asctrust.NewOpID()
	}
	p.PolicyURL, p.SecurityURL, p.AuditURL, p.ChecksURL = ascPolicyLinks(repo)
	ctx.Data["Title"] = ctx.Tr("asc_shell.sn_policy_trust")
	ctx.Data["PageIsSettingsAscPolicy"] = true
	ctx.Data["AscPolicy"] = p
	trustweb.NoStore(ctx)
	return p
}

// AscPolicy serves GET /{owner}/{repo}/settings/asc/policy.
func AscPolicy(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	p := ascPolicyPageFor(ctx, repo)
	ascPolicyRender(ctx, repo, name, p, "", nil)
}

// ascPolicyRender loads the policy in force and draws the page. draft, when not empty, is the text in the editor;
// preview is the answer to "check this draft".
func ascPolicyRender(ctx *context.Context, repo *repo_model.Repository, name string, p *ascPolicyPage, draft string, preview *asctrust.Preview) {
	status, ok := ascPolicyLoad(ctx, repo, name, p, draft, preview)
	if !ok {
		return // the refusal was written
	}
	if p.Problem != "" && status == http.StatusOK {
		status = http.StatusBadRequest
	}
	ctx.HTML(status, tplAscPolicy)
}

// ascPolicyLoad fills p. It returns the status for the page, and false when it wrote the answer itself.
func ascPolicyLoad(ctx *context.Context, repo *repo_model.Repository, name string, p *ascPolicyPage, draft string, preview *asctrust.Preview) (int, bool) {
	if !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		p.Unsupported = true
		return http.StatusOK, true
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc policy: no connection to asc serve: %v", err)
		p.Problem = "The ASC server is not reachable right now."
		return http.StatusBadGateway, true
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		f := trustweb.FailureOf("policy credential", err)
		p.Problem = f.Message
		return f.Status, true
	}
	defer release()
	page, err := asctrust.LoadPolicyPage(callCtx, conn, name)
	if err != nil {
		f := trustweb.FailureOf("policy load", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc policy", nil)
			return 0, false
		}
		p.Problem = f.Message
		return f.Status, true
	}
	p.Org = ascOrgPolicy(ctx, conn, repo, name, page)
	p.View = page.View
	p.Base = hex.EncodeToString(page.Base)
	p.Draft = page.View.Text
	if !page.View.Present {
		p.Draft = asctrust.StarterPolicy(ctx.Doer.Name)
	}
	if draft != "" {
		p.Draft = draft
	}
	p.Dirty = strings.TrimSpace(trustweb.Lines(p.Draft)) != strings.TrimSpace(trustweb.Lines(page.View.Text))
	p.Preview = preview
	return http.StatusOK, true
}

// ascOrgPolicy asks asc what the organisation that owns repo requires, resolved with the repository's own file. It is left out
// when the repository is not an organisation's, when asc does not offer the service, or when the call fails: the page is the
// repository's, and this is a note on it.
func ascOrgPolicy(ctx *context.Context, conn grpc.ClientConnInterface, repo *repo_model.Repository, name string, page *asctrust.PolicyPage) *asctrust.OrgPolicyView {
	if !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) {
		return nil
	}
	callCtx, release, err := asctrust.UserCallOrgPolicy(ctx, ctx.Doer, repo)
	if err != nil {
		return nil
	}
	defer release()
	cctx, cancel := gocontext.WithTimeout(callCtx, asctrust.CallTimeout)
	defer cancel()
	view, err := asctrust.OrgPolicyFor(cctx, conn, repo.OwnerName, strings.ToLower(name), asctrust.MergeDefaultsOf(page.Parsed))
	if err != nil {
		log.Warn("asc policy: the organisation's merge defaults: %v", err)
		return nil
	}
	return view
}

// AscPolicyCheck serves POST /{owner}/{repo}/settings/asc/policy/check: asc validates the draft and the page
// shows the diagnostics and what the draft would mean. Nothing is stored.
func AscPolicyCheck(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxPolicyForm) {
		return
	}
	p := ascPolicyPageFor(ctx, repo)
	draft := trustweb.Lines(ctx.Req.PostFormValue("text"))
	p.Message = trustweb.Lines(ctx.Req.PostFormValue("message"))
	preview, problem := ascPolicyPreview(ctx, repo, name, draft)
	p.Problem = problem
	ascPolicyRender(ctx, repo, name, p, draft, preview)
}

func ascPolicyPreview(ctx *context.Context, repo *repo_model.Repository, name, draft string) (*asctrust.Preview, string) {
	if err := asctrust.CheckText(draft); err != nil {
		return nil, trustweb.FailureOf("policy check", err).Message
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, "The ASC server is not reachable right now."
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return nil, trustweb.FailureOf("policy credential", err).Message
	}
	defer release()
	preview, err := asctrust.PreviewPolicy(callCtx, conn, name, draft)
	if err != nil {
		return nil, trustweb.FailureOf("policy check", err).Message
	}
	return preview, ""
}

// AscPolicyPropose serves POST /{owner}/{repo}/settings/asc/policy/propose: the draft becomes a change under
// review and the person is sent to its pull request.
func AscPolicyPropose(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxPolicyForm) {
		return
	}
	p := ascPolicyPageFor(ctx, repo)
	draft := trustweb.Lines(ctx.Req.PostFormValue("text"))
	p.Message = trustweb.Lines(ctx.Req.PostFormValue("message"))
	op := ctx.Req.PostFormValue("op")
	base := ctx.Req.PostFormValue("base")

	redirect, proposed, problem := ascPolicyPropose(ctx, repo, name, draft, p.Message, op, base)
	switch {
	case redirect != "":
		ctx.Redirect(redirect)
	case proposed != nil:
		ctx.Flash.Success("The change " + proposed.Change + " was made and is under review. It will get its pull request as soon as the git view of the repository has it.")
		ctx.Redirect(p.PolicyURL)
	default:
		p.Problem = problem
		ascPolicyRender(ctx, repo, name, p, draft, nil)
	}
}

// ascPolicyPropose makes the proposal. It returns the link of the pull request when there is one; the proposed
// change when it was made but has no pull request yet; or the sentence for the person when it failed.
func ascPolicyPropose(ctx *context.Context, repo *repo_model.Repository, name, text, message, op, baseHex string) (redirect string, proposed *ascProposed, problem string) {
	if !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		return "", nil, "This server does not have the policy API yet."
	}
	if err := asctrust.CheckText(text); err != nil {
		return "", nil, trustweb.FailureOf("policy propose", err).Message
	}
	base, err := hex.DecodeString(baseHex)
	if err != nil || (len(base) != 0 && len(base) != 32) {
		return "", nil, "The page is out of date. Reload it and try again."
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return "", nil, "The ASC server is not reachable right now."
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		return "", nil, trustweb.FailureOf("policy credential", err).Message
	}
	defer release()

	// The base the page was drawn from must still be the base in force; otherwise the person is editing a stale
	// file and the proposal would silently revert someone else's change.
	cur, err := asctrust.LoadPolicyPage(callCtx, conn, name)
	if err != nil {
		return "", nil, trustweb.FailureOf("policy propose", err).Message
	}
	if hex.EncodeToString(cur.Base) != hex.EncodeToString(base) {
		return "", nil, "The policy changed while you were editing. Reload the page, apply your change again, and propose it."
	}
	if strings.TrimSpace(trustweb.Lines(cur.View.Text)) == strings.TrimSpace(text) {
		return "", nil, "Nothing changed: the draft is the policy in force."
	}
	prop, err := asctrust.ProposePolicy(callCtx, conn, name, base, text, message, op)
	if err != nil {
		return "", nil, trustweb.FailureOf("policy propose", err).Message
	}
	pr, err := asctrust.OpenProposalPull(ctx, conn, repo, name, ctx.Doer, prop, message, ascPullSeams)
	if err != nil {
		if !errors.Is(err, asctrust.ErrNotMirrored) {
			log.Error("asc policy: open the pull request of change %s: %v", prop.Short, err)
		}
		return "", &ascProposed{Change: prop.Short}, ""
	}
	if err := pr.LoadIssue(ctx); err != nil {
		log.Error("asc policy: load the issue of pull request %d: %v", pr.ID, err)
		return "", &ascProposed{Change: prop.Short}, ""
	}
	return pr.Issue.Link(), nil, ""
}

// AscPolicyProtection serves POST /{owner}/{repo}/settings/asc/policy/protection: a switch of the "Protections"
// block. It edits the one key in the file in force and proposes the result, so a switch is a reviewed change too.
func AscPolicyProtection(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, 8<<10) {
		return
	}
	p := ascPolicyPageFor(ctx, repo)
	key := ctx.Req.PostFormValue("key")
	var on bool
	switch ctx.Req.PostFormValue("value") {
	case "on":
		on = true
	case "off":
	default:
		p.Problem = "Choose on or off."
		ascPolicyRender(ctx, repo, name, p, "", nil)
		return
	}
	if _, known := asctrust.Toggles[key]; !known {
		p.Problem = "That is not a protection of this repository."
		ascPolicyRender(ctx, repo, name, p, "", nil)
		return
	}
	text, baseHex, problem := ascCurrentPolicy(ctx, repo, name)
	if problem != "" {
		p.Problem = problem
		ascPolicyRender(ctx, repo, name, p, "", nil)
		return
	}
	newText, err := asctrust.ApplyToggle(text, key, on)
	if err != nil {
		p.Problem = "That is not a protection of this repository."
		ascPolicyRender(ctx, repo, name, p, "", nil)
		return
	}
	loc := asctrust.Toggles[key]
	state := "false"
	if on {
		state = "true"
	}
	message := "policy: set " + loc[0] + "." + loc[1] + " = " + state
	redirect, proposed, problem := ascPolicyPropose(ctx, repo, name, newText, message, ctx.Req.PostFormValue("op"), baseHex)
	switch {
	case redirect != "":
		ctx.Redirect(redirect)
	case proposed != nil:
		ctx.Flash.Success("The change " + proposed.Change + " was made and is under review. It will get its pull request as soon as the git view of the repository has it.")
		ctx.Redirect(p.PolicyURL)
	default:
		p.Problem = problem
		ascPolicyRender(ctx, repo, name, p, "", nil)
	}
}

// ascCurrentPolicy reads the text of the policy in force and the changeset it was read from (hex).
func ascCurrentPolicy(ctx *context.Context, repo *repo_model.Repository, name string) (text, baseHex, problem string) {
	if !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		return "", "", "This server does not have the policy API yet."
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return "", "", "The ASC server is not reachable right now."
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return "", "", trustweb.FailureOf("policy credential", err).Message
	}
	defer release()
	page, err := asctrust.LoadPolicyPage(callCtx, conn, name)
	if err != nil {
		return "", "", trustweb.FailureOf("policy load", err).Message
	}
	if page.View.Error != "" {
		return "", "", page.View.Error
	}
	return page.View.Text, hex.EncodeToString(page.Base), ""
}
