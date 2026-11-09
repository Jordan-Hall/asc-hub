// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package moderation

import (
	"errors"
	"net/http"
	"strconv"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/moderation"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascmoderation"
	"forgejo.org/services/context"
	moderation_service "forgejo.org/services/moderation"
)

const (
	tplDecisions base.TplName = "moderation/asc_decisions"
	tplDecision  base.TplName = "moderation/asc_decision"
	// tplProhibitLogin is the page of an account that may not sign in, where a suspension is appealed.
	tplProhibitLogin base.TplName = "user/auth/prohibit_login"
	// decisionsShown bounds the decisions the list shows.
	decisionsShown = 50
)

// decisionRow is one decision as the pages show it.
type decisionRow struct {
	*moderation.Decision
	ActionKey  string
	Link       string
	Appealable bool
}

func rowOf(d *moderation.Decision) decisionRow {
	return decisionRow{Decision: d, ActionKey: ascmoderation.ActionKey(d.Action), Link: ascmoderation.DecisionPath(d.ID), Appealable: ascmoderation.Appealable(d)}
}

// Decisions lists the decisions about the signed-in person's content (GET /-/moderation/decisions).
func Decisions(ctx *context.Context) {
	list, err := moderation.DecisionsOf(ctx, ctx.Doer.ID, decisionsShown)
	if err != nil {
		ctx.ServerError("DecisionsOf", err)
		return
	}
	rows := make([]decisionRow, 0, len(list))
	for _, d := range list {
		rows = append(rows, rowOf(d))
	}
	ctx.Data["Title"] = ctx.Tr("asc_admin.mod_decisions_title")
	ctx.Data["Decisions"] = rows
	ctx.HTML(http.StatusOK, tplDecisions)
}

// ownDecision loads the decision of the address; another person's decision answers 404, like one that does not exist.
func ownDecision(ctx *context.Context) *moderation.Decision {
	d, err := moderation.GetDecision(ctx, ctx.ParamsInt64(":id"))
	if err != nil {
		ctx.ServerError("GetDecision", err)
		return nil
	}
	if d == nil || d.OwnerID != ctx.Doer.ID {
		ctx.NotFound("GetDecision", nil)
		return nil
	}
	return d
}

// Decision shows one decision with its reason and the appeal: the form while the owner may appeal, else its state
// (GET /-/moderation/decisions/{id}).
func Decision(ctx *context.Context) {
	d := ownDecision(ctx)
	if d == nil {
		return
	}
	ctx.Data["Title"] = ctx.Tr("asc_admin.mod_decision_title")
	ctx.Data["Decision"] = rowOf(d)
	ctx.Data["MaxAppeal"] = moderation.MaxAppealText
	ctx.HTML(http.StatusOK, tplDecision)
}

// AppealPost records the owner's appeal (POST /-/moderation/decisions/{id}/appeal, field text).
func AppealPost(ctx *context.Context) {
	d := ownDecision(ctx)
	if d == nil {
		return
	}
	link := ascmoderation.DecisionPath(d.ID)
	text := ascmoderation.Clean(ctx.FormString("text"), moderation.MaxAppealText)
	if text == "" {
		ctx.Flash.Error(ctx.Tr("asc_admin.mod_appeal_empty"))
		ctx.Redirect(link)
		return
	}
	if !ascmoderation.Appealable(d) {
		ctx.Flash.Info(ctx.Tr("asc_admin.mod_appeal_once"))
		ctx.Redirect(link)
		return
	}
	if err := moderation.Appeal(ctx, d.ID, ctx.Doer.ID, text); err != nil {
		if errors.Is(err, moderation.ErrAppealNotAllowed) {
			ctx.Flash.Info(ctx.Tr("asc_admin.mod_appeal_once"))
			ctx.Redirect(link)
			return
		}
		ctx.ServerError("Appeal", err)
		return
	}
	log.Info("moderation: %s appealed decision %d", ctx.Doer.Name, d.ID)
	ctx.Flash.Success(ctx.Tr("asc_admin.mod_appeal_sent"))
	ctx.Redirect(link)
}

// AppealSignInPost records the appeal of a suspended account from the sign-in page (POST /-/moderation/appeal, fields
// decision, code and text). The code was made after the password was checked and is used once.
func AppealSignInPost(ctx *context.Context) {
	signIn := setting.AppSubURL + "/user/login"
	id, err := strconv.ParseInt(ctx.FormString("decision"), 10, 64)
	text := ascmoderation.Clean(ctx.FormString("text"), moderation.MaxAppealText)
	if err != nil || id <= 0 || text == "" {
		ctx.Flash.Error(ctx.Tr("asc_admin.mod_appeal_empty"))
		ctx.Redirect(signIn)
		return
	}
	if err := ascmoderation.AppealWithCode(ctx, id, ctx.FormString("code"), text); err != nil {
		if errors.Is(err, moderation.ErrAppealNotAllowed) {
			ctx.Flash.Error(ctx.Tr("asc_admin.mod_appeal_expired"))
			ctx.Redirect(signIn)
			return
		}
		ctx.ServerError("AppealWithCode", err)
		return
	}
	ctx.Flash.Success(ctx.Tr("asc_admin.mod_appeal_sent"))
	ctx.Redirect(signIn)
}

// AppealLinkForm opens the appeal form from the link emailed with a suspension (GET /-/moderation/appeal, query decision
// and code). The code is not used up until the appeal is sent.
func AppealLinkForm(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("auth.prohibit_login")
	id, _ := strconv.ParseInt(ctx.FormString("decision"), 10, 64)
	code := ctx.FormString("code")
	d := ascmoderation.CodeOpens(ctx, id, code)
	if d == nil {
		ctx.Data["AscAppealLinkDead"] = true
		ctx.HTML(http.StatusOK, tplProhibitLogin)
		return
	}
	ctx.Data["AscSuspension"] = rowOf(d)
	if ascmoderation.Appealable(d) {
		ctx.Data["AscAppealCode"] = code
		ctx.Data["AscAppealMax"] = moderation.MaxAppealText
	}
	ctx.HTML(http.StatusOK, tplProhibitLogin)
}

// SuspendedAppeal puts the appeal form of a suspended account in the data of the sign-in page: the reason of the
// newest suspension and a code for the form, or the state of the appeal already made. Call it only once the password
// of uid was checked.
func SuspendedAppeal(ctx *context.Context, uid int64) {
	if !setting.Moderation.Enabled || uid <= 0 {
		return
	}
	u, err := user_model.GetUserByID(ctx, uid)
	if err != nil || !u.ProhibitLogin {
		return
	}
	d, err := moderation.LatestSuspension(ctx, uid, int(moderation_service.ContentActionSuspendAccount))
	if err != nil {
		log.Error("moderation: the suspension of %d: %v", uid, err)
		return
	}
	if d == nil {
		return
	}
	// A password alone is not a sign-in for an account with a second factor: it shows nothing of the decision and
	// appeals with the link that was emailed with it.
	twoFactor, err := auth_model.HasTwoFactorByUID(ctx, uid)
	if err != nil {
		log.Error("moderation: two-factor of %d: %v", uid, err)
		return
	}
	if twoFactor {
		ctx.Data["AscAppealByMail"] = true
		return
	}
	ctx.Data["AscSuspension"] = rowOf(d)
	if !ascmoderation.Appealable(d) {
		return
	}
	code, err := ascmoderation.AppealCode(ctx, d)
	if err != nil {
		log.Error("moderation: appeal code of decision %d: %v", d.ID, err)
		return
	}
	ctx.Data["AscAppealCode"] = code
	ctx.Data["AscAppealMax"] = moderation.MaxAppealText
}
