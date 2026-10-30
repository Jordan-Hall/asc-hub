// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The "Workflow secrets" page of a repository: /{owner}/{repo}/settings/asc/workflow-secrets. A command step of a workflow reads a
// secret by name (`secrets = { TOKEN = "name" }`); asc keeps the values, sealed, and hands one only to a command of a workflow
// that names it. The page is for administrators of the repository and shows names and hints, never a value: a value is typed
// once, goes to asc, and is not kept, flashed, logged or returned here.
const (
	tplAscWorkflowSecrets base.TplName = "repo/settings/asc_workflow_secrets"

	maxWorkflowSecretForm = 2 * asctrust.MaxWorkflowSecretValue
)

type ascWorkflowSecretsPage struct {
	Rows        []asctrust.WorkflowSecretRow
	Ops         map[string]string
	SetOp       string
	Problem     string
	Unsupported bool
	PageURL     string
}

func ascWorkflowSecretsLink(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/settings/asc/workflow-secrets"
}

func ascWorkflowSecretsPageFor(ctx *context.Context, repo *repo_model.Repository) *ascWorkflowSecretsPage {
	p := &ascWorkflowSecretsPage{Ops: map[string]string{}, SetOp: asctrust.NewOpID(), PageURL: ascWorkflowSecretsLink(repo)}
	ctx.Data["Title"] = ctx.Tr("asc_review.wfs_title")
	ctx.Data["PageIsSettingsAscWorkflowSecrets"] = true
	ctx.Data["AscWorkflowSecrets"] = p
	trustweb.NoStore(ctx)
	return p
}

// AscWorkflowSecrets serves GET /{owner}/{repo}/settings/asc/workflow-secrets.
func AscWorkflowSecrets(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	p := ascWorkflowSecretsPageFor(ctx, repo)
	status := http.StatusOK
	conn, err := asctrust.Conn()
	if err != nil {
		p.Problem = "The ASC server is not reachable right now."
		ctx.HTML(http.StatusBadGateway, tplAscWorkflowSecrets)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		f := trustweb.FailureOf("workflow secrets credential", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscWorkflowSecrets)
		return
	}
	defer release()
	rows, err := asctrust.ListWorkflowSecrets(callCtx, conn, name)
	if err != nil {
		if asctrust.Classify(err).Kind == asctrust.KindUnimplemented {
			p.Unsupported = true
			ctx.HTML(http.StatusOK, tplAscWorkflowSecrets)
			return
		}
		f := trustweb.FailureOf("workflow secrets", err)
		p.Problem = f.Message
		ctx.HTML(f.Status, tplAscWorkflowSecrets)
		return
	}
	p.Rows = rows
	for _, r := range rows {
		p.Ops[r.Name] = asctrust.NewOpID()
	}
	ctx.HTML(status, tplAscWorkflowSecrets)
}

// AscWorkflowSecretSet serves POST .../workflow-secrets with the fields name, value and op.
func AscWorkflowSecretSet(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxWorkflowSecretForm) {
		return
	}
	back := ascWorkflowSecretsLink(repo)
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error("The ASC server is not reachable right now.")
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("workflow secrets credential", err).Message)
		ctx.Redirect(back)
		return
	}
	defer release()
	row, err := asctrust.SetWorkflowSecret(callCtx, conn, name, ctx.Req.PostFormValue("name"), ctx.Req.PostFormValue("value"), ctx.Req.PostFormValue("op"))
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("workflow secret set", err).Message)
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_review.wfs_saved", row.Name))
	ctx.Redirect(back)
}

// AscWorkflowSecretDelete serves POST .../workflow-secrets/delete with the fields name and op.
func AscWorkflowSecretDelete(ctx *context.Context) {
	repo, name, ok := trustweb.AdminRepo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxWorkflowSecretForm) {
		return
	}
	back := ascWorkflowSecretsLink(repo)
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error("The ASC server is not reachable right now.")
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoAdmin, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("workflow secrets credential", err).Message)
		ctx.Redirect(back)
		return
	}
	defer release()
	secret := ctx.Req.PostFormValue("name")
	if err := asctrust.DeleteWorkflowSecret(callCtx, conn, name, secret, ctx.Req.PostFormValue("op")); err != nil {
		ctx.Flash.Error(trustweb.FailureOf("workflow secret delete", err).Message)
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_review.wfs_deleted", secret))
	ctx.Redirect(back)
}
