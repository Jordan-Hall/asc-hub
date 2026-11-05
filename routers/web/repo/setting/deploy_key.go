// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"net/http"

	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web"
	asymkey_service "forgejo.org/services/asymkey"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

// DeployKeys renders the access page, whose deploy keys section lists the deploy keys of the repository
func DeployKeys(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.settings.deploy_keys")
	ctx.Data["PageIsSettingsKeys"] = true

	if !loadAccess(ctx) {
		return
	}

	ctx.HTML(http.StatusOK, tplCollaboration)
}

// DeployKeysPost response for adding a deploy key of a repository
func DeployKeysPost(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.AddKeyForm)
	ctx.Data["Title"] = ctx.Tr("repo.settings.deploy_keys")
	ctx.Data["PageIsSettingsKeys"] = true

	if !loadAccess(ctx) {
		return
	}

	if ctx.HasError() {
		ctx.HTML(http.StatusOK, tplCollaboration)
		return
	}

	content, err := asymkey_model.CheckPublicKeyString(form.Content)
	if err != nil {
		if db.IsErrSSHDisabled(err) {
			ctx.Flash.Info(ctx.Tr("settings.ssh_disabled"))
		} else if asymkey_model.IsErrKeyUnableVerify(err) {
			ctx.Flash.Info(ctx.Tr("form.unable_verify_ssh_key"))
		} else if err == asymkey_model.ErrKeyIsPrivate {
			ctx.Data["HasError"] = true
			ctx.Data["Err_Content"] = true
			ctx.Flash.Error(ctx.Tr("form.must_use_public_key"))
		} else {
			ctx.Data["HasError"] = true
			ctx.Data["Err_Content"] = true
			ctx.Flash.Error(ctx.Tr("form.invalid_ssh_key", err.Error()))
		}
		ctx.Redirect(deployKeysBack(ctx))
		return
	}

	key, err := asymkey_model.AddDeployKeyBy(ctx, ctx.Repo.Repository.ID, ctx.Doer.ID, form.Title, content, !form.IsWritable)
	if err != nil {
		ctx.Data["HasError"] = true
		switch {
		case asymkey_model.IsErrDeployKeyAlreadyExist(err):
			ctx.Data["Err_Content"] = true
			ctx.RenderWithErr(ctx.Tr("repo.settings.key_been_used"), tplCollaboration, &form)
		case asymkey_model.IsErrKeyAlreadyExist(err):
			ctx.Data["Err_Content"] = true
			ctx.RenderWithErr(ctx.Tr("settings.ssh_key_been_used"), tplCollaboration, &form)
		case asymkey_model.IsErrKeyNameAlreadyUsed(err):
			ctx.Data["Err_Title"] = true
			ctx.RenderWithErr(ctx.Tr("repo.settings.key_name_used"), tplCollaboration, &form)
		case asymkey_model.IsErrDeployKeyNameAlreadyUsed(err):
			ctx.Data["Err_Title"] = true
			ctx.RenderWithErr(ctx.Tr("repo.settings.key_name_used"), tplCollaboration, &form)
		default:
			ctx.ServerError("AddDeployKey", err)
		}
		return
	}

	log.Trace("Deploy key added: %d", ctx.Repo.Repository.ID)
	ctx.Flash.Success(ctx.Tr("repo.settings.add_key_success", key.Name))
	ctx.Redirect(deployKeysBack(ctx))
}

// DeleteDeployKey response for deleting a deploy key
func DeleteDeployKey(ctx *context.Context) {
	if err := asymkey_service.DeleteDeployKey(ctx, ctx.FormInt64("id"), ctx.Repo.Repository.ID); err != nil {
		ctx.Flash.Error("DeleteDeployKey: " + err.Error())
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.deploy_key_deletion_success"))
	}

	ctx.JSONRedirect(deployKeysBack(ctx))
}

// loadDeployKeys puts the deploy keys of the repository, and the people who added them, in the page data. It reports
// false after it has answered with an error.
func loadDeployKeys(ctx *context.Context) bool {
	ctx.Data["DisableSSH"] = setting.SSH.Disabled
	keys, err := db.Find[asymkey_model.DeployKey](ctx, asymkey_model.ListDeployKeysOptions{RepoID: ctx.Repo.Repository.ID})
	if err != nil {
		ctx.ServerError("ListDeployKeys", err)
		return false
	}
	ids := make([]int64, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.AddedByID)
	}
	users, err := user_model.GetPossibleUserByIDs(ctx, ids)
	if err != nil {
		ctx.ServerError("GetPossibleUserByIDs", err)
		return false
	}
	adders := make(map[int64]*user_model.User, len(users))
	for _, u := range users {
		adders[u.ID] = u
	}
	ctx.Data["Deploykeys"] = keys
	ctx.Data["DeployKeyAdders"] = adders
	return true
}

// deployKeysBack is the page to go back to after a deploy key is added or deleted: the access page when the action
// came from it (back=access), else the deploy keys page.
func deployKeysBack(ctx *context.Context) string {
	if ctx.FormString("back") == "access" {
		return ctx.Repo.RepoLink + "/settings/collaboration"
	}
	return ctx.Repo.RepoLink + "/settings/keys"
}
