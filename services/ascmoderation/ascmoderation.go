// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascmoderation keeps the decisions administrators take on reported content and the appeals of their owners
// (board AdminModeration): who the content belonged to, the action and its reason, an email to the owner, and the appeal
// the owner may make once. A suspended account cannot sign in, so it appeals from the sign-in page with a code that the
// hub makes only after the password was checked.
package ascmoderation

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/issues"
	"forgejo.org/models/moderation"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/translation"
	"forgejo.org/services/mailer"
	moderation_service "forgejo.org/services/moderation"
)

const (
	// AppealCodeTTL is how long the appeal form of the sign-in page stays usable.
	AppealCodeTTL = time.Hour
	// MailCodeTTL is how long the appeal link in the email of a suspension stays usable.
	MailCodeTTL = 14 * 24 * time.Hour
)

// Send is the function that sends an email; tests replace it.
var Send = func(to, subject, body string) { mailer.SendAsync(mailer.NewMessage(to, subject, body)) }

// Target is what an action is about, read before the action removes it.
type Target struct {
	OwnerID int64
	Ref     string
}

// TargetOf reads the owner and the name of reported content. Content that no longer exists has no target.
func TargetOf(ctx context.Context, contentType moderation.ReportedContentType, contentID int64) (Target, error) {
	switch contentType {
	case moderation.ReportedContentTypeUser:
		u, err := user_model.GetUserByID(ctx, contentID)
		if err != nil {
			return Target{}, err
		}
		return Target{OwnerID: u.ID, Ref: "@" + u.Name}, nil
	case moderation.ReportedContentTypeRepository:
		repo, err := repo_model.GetRepositoryByID(ctx, contentID)
		if err != nil {
			return Target{}, err
		}
		return Target{OwnerID: repo.OwnerID, Ref: repo.FullName()}, nil
	case moderation.ReportedContentTypeIssue:
		issue, err := issues.GetIssueByID(ctx, contentID)
		if err != nil {
			return Target{}, err
		}
		if err := issue.LoadRepo(ctx); err != nil {
			return Target{}, err
		}
		return Target{OwnerID: issue.PosterID, Ref: issue.Repo.FullName() + "#" + strconv.FormatInt(issue.Index, 10)}, nil
	case moderation.ReportedContentTypeComment:
		comment, err := issues.GetCommentByID(ctx, contentID)
		if err != nil {
			return Target{}, err
		}
		if err := comment.LoadIssue(ctx); err != nil {
			return Target{}, err
		}
		if err := comment.Issue.LoadRepo(ctx); err != nil {
			return Target{}, err
		}
		return Target{OwnerID: comment.PosterID, Ref: comment.Issue.Repo.FullName() + "#" + strconv.FormatInt(comment.Issue.Index, 10)}, nil
	}
	return Target{}, fmt.Errorf("ascmoderation: unknown content type %d", contentType)
}

// Clean takes control characters (but new lines) out of what a person wrote and keeps at most max characters.
func Clean(raw string, max int) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if runes := []rune(cleaned); len(runes) > max {
		cleaned = strings.TrimSpace(string(runes[:max]))
	}
	return cleaned
}

// ActionKey is the locale key that names a content action.
func ActionKey(action int) string {
	switch moderation_service.ContentAction(action) {
	case moderation_service.ContentActionSuspendAccount:
		return "asc_admin.mod_action_suspend"
	case moderation_service.ContentActionDeleteAccount:
		return "asc_admin.mod_action_delete_account"
	case moderation_service.ContentActionDeleteRepo:
		return "asc_admin.mod_action_delete_repo"
	case moderation_service.ContentActionDeleteIssue:
		return "asc_admin.mod_action_delete_issue"
	case moderation_service.ContentActionDeleteComment:
		return "asc_admin.mod_action_delete_comment"
	}
	return "asc_admin.mod_action_other"
}

// IsSuspension tells whether a decision suspended an account.
func IsSuspension(d *moderation.Decision) bool {
	return moderation_service.ContentAction(d.Action) == moderation_service.ContentActionSuspendAccount
}

// Appealable tells whether the owner of a decision can still appeal it: an account that was deleted cannot.
func Appealable(d *moderation.Decision) bool {
	return d.CanAppeal() && moderation_service.ContentAction(d.Action) != moderation_service.ContentActionDeleteAccount
}

// DecisionPath is the page where the owner reads a decision and appeals it.
func DecisionPath(id int64) string {
	return setting.AppSubURL + "/-/moderation/decisions/" + strconv.FormatInt(id, 10)
}

func absolute(path string) string {
	return strings.TrimSuffix(setting.AppURL, "/") + strings.TrimPrefix(path, setting.AppSubURL)
}

// Record keeps an action that was taken and emails the owner of the content. An action of none is not a decision.
func Record(ctx context.Context, moderator *user_model.User, contentType moderation.ReportedContentType, contentID int64, action moderation_service.ContentAction, reason string, t Target) (*moderation.Decision, error) {
	if action == moderation_service.ContentActionNone || moderator == nil {
		return nil, nil
	}
	d := &moderation.Decision{
		ContentType: contentType,
		ContentID:   contentID,
		ContentRef:  t.Ref,
		OwnerID:     t.OwnerID,
		Action:      int(action),
		Reason:      reason,
		ModeratorID: moderator.ID,
	}
	if err := moderation.RecordDecision(ctx, d); err != nil {
		return nil, err
	}
	if Appealable(d) {
		tellOwner(ctx, d)
	}
	return d, nil
}

// tellOwner emails the owner what was done and how to appeal. Without a mail service, or for an account that cannot
// get mail, nothing is sent: the owner still finds the decision on its page (and a suspended account at sign-in).
func tellOwner(ctx context.Context, d *moderation.Decision) {
	if setting.MailService == nil {
		return
	}
	u, err := user_model.GetUserByID(ctx, d.OwnerID)
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			log.Error("asc moderation: owner %d of decision %d: %v", d.OwnerID, d.ID, err)
		}
		return
	}
	if u.IsOrganization() || u.Email == "" {
		return
	}
	loc := translation.NewLocale(u.Language)
	var b strings.Builder
	b.WriteString(loc.TrString("asc_admin.mod_mail_lead", loc.TrString(ActionKey(d.Action)), d.ContentRef) + "\n\n")
	if d.Reason != "" {
		b.WriteString(loc.TrString("asc_admin.mod_mail_reason") + "\n" + d.Reason + "\n\n")
	}
	if IsSuspension(d) {
		link, err := appealLink(ctx, d)
		if err != nil {
			log.Error("asc moderation: appeal link of decision %d: %v", d.ID, err)
			link = strings.TrimSuffix(setting.AppURL, "/") + "/user/login"
		}
		b.WriteString(loc.TrString("asc_admin.mod_mail_appeal_signin") + "\n" + link + "\n")
	} else {
		b.WriteString(loc.TrString("asc_admin.mod_mail_appeal") + "\n" + absolute(DecisionPath(d.ID)) + "\n")
	}
	Send(u.EmailTo(), loc.TrString("asc_admin.mod_mail_subject"), b.String())
}

// TellAnswer emails the owner the answer to their appeal.
func TellAnswer(ctx context.Context, d *moderation.Decision) {
	if setting.MailService == nil {
		return
	}
	u, err := user_model.GetUserByID(ctx, d.OwnerID)
	if err != nil || u.IsOrganization() || u.Email == "" {
		return
	}
	loc := translation.NewLocale(u.Language)
	key := "asc_admin.mod_mail_upheld"
	if d.AppealStatus == moderation.AppealOverturned {
		key = "asc_admin.mod_mail_overturned"
		if !IsSuspension(d) {
			key = "asc_admin.mod_mail_overturned_removed"
		}
	}
	var b strings.Builder
	b.WriteString(loc.TrString(key, d.ContentRef) + "\n\n")
	if d.AppealReason != "" {
		b.WriteString(loc.TrString("asc_admin.mod_mail_reason") + "\n" + d.AppealReason + "\n")
	}
	Send(u.EmailTo(), loc.TrString("asc_admin.mod_mail_answer_subject"), b.String())
}

func appealPurpose(id int64) auth_model.AuthorizationPurpose {
	return auth_model.AuthorizationPurpose("asc_moderation_appeal:" + strconv.FormatInt(id, 10))
}

// appealLink is the single-use appeal link a suspended account gets by email: the email address is the second proof
// for an account that has two-factor sign-in, whose sign-in page offers no appeal.
func appealLink(ctx context.Context, d *moderation.Decision) (string, error) {
	code, err := makeCode(ctx, d, MailCodeTTL)
	if err != nil {
		return "", err
	}
	return absolute(setting.AppSubURL + "/-/moderation/appeal?decision=" + strconv.FormatInt(d.ID, 10) + "&code=" + url.QueryEscape(code)), nil
}

func makeCode(ctx context.Context, d *moderation.Decision, ttl time.Duration) (string, error) {
	lookup, validator, err := auth_model.GenerateAuthToken(ctx, d.OwnerID, optional.None[int64](), timeutil.TimeStampNow().Add(int64(ttl.Seconds())), appealPurpose(d.ID))
	if err != nil {
		return "", err
	}
	return lookup + ":" + validator, nil
}

// CodeOpens returns the decision an appeal code still opens the form of, without using the code up; nil when it opens
// nothing.
func CodeOpens(ctx context.Context, decisionID int64, code string) *moderation.Decision {
	u, _, _, err := user_model.VerifyUserAuthorizationToken(ctx, code, appealPurpose(decisionID))
	if err != nil || u == nil {
		return nil
	}
	d, err := moderation.GetDecision(ctx, decisionID)
	if err != nil || d == nil || d.OwnerID != u.ID || !IsSuspension(d) {
		return nil
	}
	return d
}

// AppealCode makes the code the sign-in page puts in the appeal form of a suspended account. Call it only after the
// password of the account was checked.
func AppealCode(ctx context.Context, d *moderation.Decision) (string, error) {
	return makeCode(ctx, d, AppealCodeTTL)
}

// AppealWithCode records the appeal of a suspended account from the sign-in page. The code is used up either way.
func AppealWithCode(ctx context.Context, decisionID int64, code, text string) error {
	u, _, used, err := user_model.VerifyUserAuthorizationToken(ctx, code, appealPurpose(decisionID))
	if err != nil || u == nil {
		return moderation.ErrAppealNotAllowed
	}
	if err := used(); err != nil {
		return err
	}
	d, err := moderation.GetDecision(ctx, decisionID)
	if err != nil {
		return err
	}
	if d == nil || d.OwnerID != u.ID || !IsSuspension(d) {
		return moderation.ErrAppealNotAllowed
	}
	return moderation.Appeal(ctx, d.ID, u.ID, text)
}
