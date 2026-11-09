// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"errors"
	"net/http"
	"strconv"

	"forgejo.org/models/moderation"
	"forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/ascmoderation"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	moderation_service "forgejo.org/services/moderation"
	user_service "forgejo.org/services/user"
)

// maxModerationReason bounds the reason an administrator writes for an action on a report.
const maxModerationReason = 500

// moderationReason cleans the reason of the form: control characters out, at most maxModerationReason characters.
func moderationReason(raw string) string {
	return ascmoderation.Clean(raw, maxModerationReason)
}

var moderationContentTypeNames = map[moderation.ReportedContentType]string{
	moderation.ReportedContentTypeUser:       "user",
	moderation.ReportedContentTypeRepository: "repository",
	moderation.ReportedContentTypeIssue:      "issue",
	moderation.ReportedContentTypeComment:    "comment",
}

var moderationContentActionNames = map[moderation_service.ContentAction]string{
	moderation_service.ContentActionNone:           "none",
	moderation_service.ContentActionSuspendAccount: "suspend_account",
	moderation_service.ContentActionDeleteAccount:  "delete_account",
	moderation_service.ContentActionDeleteRepo:     "delete_repository",
	moderation_service.ContentActionDeleteIssue:    "delete_issue",
	moderation_service.ContentActionDeleteComment:  "delete_comment",
}

var moderationReportActionNames = map[moderation_service.ReportAction]string{
	moderation_service.ReportActionNone:          "none",
	moderation_service.ReportActionMarkAsHandled: "mark_as_handled",
	moderation_service.ReportActionMarkAsIgnored: "mark_as_ignored",
}

// moderationDetails is what the audit event of an action holds: what was reported and what was done, with the reason.
func moderationDetails(contentType moderation.ReportedContentType, contentID int64, ca moderation_service.ContentAction, ra moderation_service.ReportAction, reason string) (target string, details map[string]string) {
	typeName := moderationContentTypeNames[contentType]
	target = typeName + ":" + strconv.FormatInt(contentID, 10)
	details = map[string]string{
		"content_type":   typeName,
		"content_id":     strconv.FormatInt(contentID, 10),
		"content_action": moderationContentActionNames[ca],
		"report_action":  moderationReportActionNames[ra],
	}
	if reason != "" {
		details["reason"] = reason
	}
	return target, details
}

// moderationTarget reads whose the content is before a content action removes it: the decision names its owner, who
// may appeal. Content that is already gone has no owner to tell.
func moderationTarget(ctx *context.Context, contentType moderation.ReportedContentType, contentID int64, ca moderation_service.ContentAction) ascmoderation.Target {
	if ca == moderation_service.ContentActionNone {
		return ascmoderation.Target{}
	}
	t, err := ascmoderation.TargetOf(ctx, contentType, contentID)
	if err != nil {
		log.Warn("moderation: the owner of %d:%d: %v", contentType, contentID, err)
	}
	return t
}

// recordModeration appends the action to the audit log of asc once it has been made, and keeps a content action as a
// decision its owner can read and appeal: an action that ended with a warning, an error or a notice that nothing was
// needed (an account already suspended) is not recorded as done. It is deferred by PerformAction.
func recordModeration(ctx *context.Context, contentType moderation.ReportedContentType, contentID int64, ca moderation_service.ContentAction, ra moderation_service.ReportAction, reason string, t ascmoderation.Target) {
	if ctx.Flash == nil || ctx.Flash.WarningMsg != "" || ctx.Flash.ErrorMsg != "" || ctx.Flash.InfoMsg != "" || ctx.Resp.WrittenStatus() >= 400 {
		return
	}
	target, details := moderationDetails(contentType, contentID, ca, ra, reason)
	if d, err := ascmoderation.Record(ctx, ctx.Doer, contentType, contentID, ca, reason, t); err != nil {
		log.Error("moderation: keep the decision on %s: %v", target, err)
	} else if d != nil {
		details["decision"] = strconv.FormatInt(d.ID, 10)
	}
	log.Info("admin %s: moderation %s on %s: %s", ctx.Doer.Name, details["content_action"], target, reason)
	asctrust.RecordHostAudit(ctx, asctrust.HostAudit{
		Action: asctrust.AuditHostModerationActed, Actor: ctx.Doer.Name, Target: target, Request: ctx.Req, Details: details,
	})
}

// moderationAppealsShown bounds the appeals the queue lists.
const moderationAppealsShown = 50

// moderationAppeal is one open appeal in the queue.
type moderationAppeal struct {
	*moderation.Decision
	OwnerName string // empty when the account is gone
	ActionKey string
}

// loadAppeals puts the open appeals, the oldest first, in .Appeals. It answers false after an error page.
func loadAppeals(ctx *context.Context) bool {
	open, err := moderation.OpenAppeals(ctx, moderationAppealsShown)
	if err != nil {
		ctx.ServerError("OpenAppeals", err)
		return false
	}
	ids := make([]int64, 0, len(open))
	for _, d := range open {
		ids = append(ids, d.OwnerID)
	}
	list, err := user.GetUsersByIDs(ctx, ids)
	if err != nil {
		ctx.ServerError("GetUsersByIDs", err)
		return false
	}
	owners := make(map[int64]*user.User, len(list))
	for _, u := range list {
		owners[u.ID] = u
	}
	appeals := make([]moderationAppeal, 0, len(open))
	for _, d := range open {
		a := moderationAppeal{Decision: d, ActionKey: ascmoderation.ActionKey(d.Action)}
		if u, ok := owners[d.OwnerID]; ok {
			a.OwnerName = u.Name
		}
		appeals = append(appeals, a)
	}
	ctx.Data["Appeals"] = appeals
	return true
}

// AnswerAppeal keeps or reverses a decision its owner appealed (POST /admin/moderation/appeals/{id}, answer uphold or
// overturn, with a reason). Overturning a suspension lets the account sign in again; content that was deleted is not
// brought back, and the owner is told so. The answer is written to the audit log of asc and emailed to the owner.
func AnswerAppeal(ctx *context.Context) {
	id := ctx.ParamsInt64(":id")
	d, err := moderation.GetDecision(ctx, id)
	if err != nil {
		ctx.ServerError("GetDecision", err)
		return
	}
	if d == nil {
		ctx.NotFound("GetDecision", nil)
		return
	}
	answer := ctx.FormString("answer")
	if answer != "uphold" && answer != "overturn" {
		ctx.Error(http.StatusBadRequest, "Invalid parameter: answer")
		return
	}
	reason := moderationReason(ctx.FormString("reason"))
	if reason == "" {
		ctx.Resp.Header().Add("HX-Reswap", "none")
		ctx.Flash.Warning(ctx.Tr("asc_admin.mod_reason_needed"), true)
		ctx.HTML(http.StatusOK, tplAlert)
		return
	}
	overturn := answer == "overturn"
	if err := moderation.DecideAppeal(ctx, d.ID, ctx.Doer.ID, overturn, reason); err != nil {
		if errors.Is(err, moderation.ErrAppealNotAllowed) {
			ctx.Flash.Info(ctx.Tr("asc_admin.mod_appeal_answered"), true)
			ctx.HTML(http.StatusOK, tplAlert)
			return
		}
		ctx.ServerError("DecideAppeal", err)
		return
	}
	if overturn && ascmoderation.IsSuspension(d) {
		if owner, err := user.GetUserByID(ctx, d.OwnerID); err == nil && owner.ProhibitLogin {
			if err := user_service.UpdateAuth(ctx, owner, &user_service.UpdateAuthOptions{ProhibitLogin: optional.Some(false)}); err != nil {
				ctx.ServerError("UpdateAuth", err)
				return
			}
		} else if err != nil && !user.IsErrUserNotExist(err) {
			ctx.ServerError("GetUserByID", err)
			return
		}
	}
	if answered, err := moderation.GetDecision(ctx, d.ID); err == nil && answered != nil {
		ascmoderation.TellAnswer(ctx, answered)
	}
	details := map[string]string{
		"decision": strconv.FormatInt(d.ID, 10),
		"appeal":   map[bool]string{true: "overturned", false: "upheld"}[overturn],
		"reason":   reason,
	}
	target := moderationContentTypeNames[d.ContentType] + ":" + strconv.FormatInt(d.ContentID, 10)
	log.Info("admin %s: appeal of decision %d %s", ctx.Doer.Name, d.ID, details["appeal"])
	asctrust.RecordHostAudit(ctx, asctrust.HostAudit{
		Action: asctrust.AuditHostModerationActed, Actor: ctx.Doer.Name, Target: target, Request: ctx.Req, Details: details,
	})
	if overturn {
		ctx.Flash.Success(ctx.Tr("asc_admin.mod_appeal_overturned"), true)
	} else {
		ctx.Flash.Success(ctx.Tr("asc_admin.mod_appeal_upheld"), true)
	}
	ctx.HTML(http.StatusOK, tplAlert)
}
