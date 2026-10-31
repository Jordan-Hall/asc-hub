// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"strconv"
	"strings"
	"unicode"

	"forgejo.org/models/moderation"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	moderation_service "forgejo.org/services/moderation"
)

// maxModerationReason bounds the reason an administrator writes for an action on a report.
const maxModerationReason = 500

// moderationReason cleans the reason of the form: control characters out, at most maxModerationReason characters.
func moderationReason(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if runes := []rune(cleaned); len(runes) > maxModerationReason {
		cleaned = string(runes[:maxModerationReason])
	}
	return cleaned
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

// recordModeration appends the action to the audit log of asc once it has been made: an action that ended with a warning or an
// error is not recorded as done. It is deferred by PerformAction.
func recordModeration(ctx *context.Context, contentType moderation.ReportedContentType, contentID int64, ca moderation_service.ContentAction, ra moderation_service.ReportAction, reason string) {
	if ctx.Flash == nil || ctx.Flash.WarningMsg != "" || ctx.Flash.ErrorMsg != "" || ctx.Resp.WrittenStatus() >= 400 {
		return
	}
	target, details := moderationDetails(contentType, contentID, ca, ra, reason)
	log.Info("admin %s: moderation %s on %s: %s", ctx.Doer.Name, details["content_action"], target, reason)
	asctrust.RecordHostAudit(ctx, asctrust.HostAudit{
		Action: asctrust.AuditHostModerationActed, Actor: ctx.Doer.Name, Target: target, Request: ctx.Req, Details: details,
	})
}
