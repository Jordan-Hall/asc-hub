// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/url"
	"strings"
	"unicode/utf8"

	activities_model "forgejo.org/models/activities"
	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/auth"
	"forgejo.org/models/db"
	org_model "forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	system_model "forgejo.org/models/system"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	user_service "forgejo.org/services/user"
)

// The page of a person in the administration (board AdminUser): a head with Edit, Reset 2FA and Suspend, tabs, and on the
// first tab the account and the usage. The tabs are links to the same address with ?tab=, so each is a page of its own.

const (
	userTabOverview = "overview"
	userTabEmails   = "emails"
	userTabKeys     = "keys"
	userTabSessions = "sessions"
	userTabActivity = "activity"

	// adminReasonMax is the longest reason for Reset 2FA or Suspend, in characters.
	adminReasonMax = 500
	// userActivityMax is how many actions the Activity tab shows.
	userActivityMax = 30
)

var userTabs = []string{userTabOverview, userTabEmails, userTabKeys, userTabSessions, userTabActivity}

// userTabOf is the tab the address asks for; an unknown one is the first.
func userTabOf(raw string) string {
	for _, t := range userTabs {
		if raw == t {
			return t
		}
	}
	return userTabOverview
}

// userSession is what the Sessions tab shows when asc cannot or does not answer.
type userSessions struct {
	Rows        []asctrust.SessionRow
	Problem     string
	Unsupported bool
}

// prepareUserTab puts the data of one tab into the page. It writes the answer itself and returns false on a server error.
func prepareUserTab(ctx *context.Context, u *user_model.User, tab string) bool {
	ctx.Data["Tab"] = tab
	ctx.Data["Tabs"] = userTabs
	switch tab {
	case userTabEmails:
		emails, err := user_model.GetEmailAddresses(ctx, u.ID)
		if err != nil {
			ctx.ServerError("GetEmailAddresses", err)
			return false
		}
		ctx.Data["Emails"] = emails
		ctx.Data["EmailsTotal"] = len(emails)
	case userTabKeys:
		ssh, err := db.Find[asymkey_model.PublicKey](ctx, asymkey_model.FindPublicKeyOptions{ListOptions: db.ListOptionsAll, OwnerID: u.ID})
		if err != nil {
			ctx.ServerError("FindPublicKeys", err)
			return false
		}
		gpg, err := db.Find[asymkey_model.GPGKey](ctx, asymkey_model.FindGPGKeyOptions{ListOptions: db.ListOptionsAll, OwnerID: u.ID})
		if err != nil {
			ctx.ServerError("FindGPGKeys", err)
			return false
		}
		ctx.Data["SSHKeys"] = ssh
		ctx.Data["GPGKeys"] = gpg
	case userTabSessions:
		ctx.Data["UserSessions"] = loadUserSessions(ctx, u)
	case userTabActivity:
		feeds, _, err := activities_model.GetFeeds(ctx, activities_model.GetFeedsOptions{
			RequestedUser: u, Actor: ctx.Doer, IncludePrivate: true, OnlyPerformedBy: true,
			ListOptions: db.ListOptions{PageSize: userActivityMax, Page: 1},
		})
		if err != nil {
			ctx.ServerError("GetFeeds", err)
			return false
		}
		ctx.Data["Feeds"] = feeds
	default:
		return prepareUserOverview(ctx, u)
	}
	return true
}

// prepareUserOverview adds the account facts the board lists (organizations, repositories, sign-in methods) and the usage.
func prepareUserOverview(ctx *context.Context, u *user_model.User) bool {
	owned, err := repo_model.CountRepository(ctx, &repo_model.SearchRepoOptions{OwnerID: u.ID, Private: true, Collaborate: optional.Some(false)})
	if err != nil {
		ctx.ServerError("CountRepository", err)
		return false
	}
	collab, err := repo_model.CountRepository(ctx, &repo_model.SearchRepoOptions{OwnerID: u.ID, Private: true, Collaborate: optional.Some(true)})
	if err != nil {
		ctx.ServerError("CountRepository", err)
		return false
	}
	ctx.Data["ReposOwned"], ctx.Data["ReposCollab"] = owned, collab
	orgs, err := db.Find[org_model.Organization](ctx, org_model.FindOrgOptions{ListOptions: db.ListOptionsAll, UserID: u.ID, IncludePrivate: true})
	if err != nil {
		ctx.ServerError("FindOrgs", err)
		return false
	}
	names := make([]string, 0, len(orgs))
	for _, o := range orgs {
		names = append(names, o.DisplayName())
	}
	ctx.Data["OrgNames"] = strings.Join(names, ", ")
	ctx.Data["OrgsTotal"] = len(orgs)
	ctx.Data["HasPassword"] = u.Passwd != ""
	if wn, err := auth.GetWebAuthnCredentialsByUID(ctx, u.ID); err == nil {
		ctx.Data["Passkeys"] = len(wn)
	}
	ctx.Data["UserUsage"] = loadUserUsage(ctx, u)
	return true
}

// userUsage is the usage block: the figures asc keeps for the person, or nothing when asc cannot say.
type userUsage struct {
	Usage *asctrust.UsagePage
}

// loadUserUsage asks asc for the usage of the person this month, as the site administrator. nil means asc is not there, does not
// have the service, or does not answer: the page then has no usage block.
func loadUserUsage(ctx *context.Context, u *user_model.User) *userUsage {
	if !setting.Asc.Enabled || !asctrust.HasFeature(ctx, asctrust.FeatureUsage) {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		return nil
	}
	defer release()
	owner, err := asctrust.OwnerOf("user", u.LowerName)
	if err != nil {
		return nil
	}
	page, err := asctrust.LoadUsage(callCtx, conn, owner, "")
	if err != nil {
		log.Debug("admin user: asc usage of %s: %v", u.Name, err)
		return nil
	}
	return &userUsage{Usage: page}
}

// loadUserSessions lists the credentials asc knows for the person (tokens, SSH keys, agent tokens), as the site administrator.
func loadUserSessions(ctx *context.Context, u *user_model.User) *userSessions {
	s := &userSessions{}
	if !setting.Asc.Enabled || !asctrust.HasFeature(ctx, asctrust.FeatureSessions) {
		s.Unsupported = true
		return s
	}
	conn, err := asctrust.Conn()
	if err != nil {
		s.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		return s
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		s.Problem = trustweb.FailureOf("admin sessions credential", err).Localized(ctx)
		return s
	}
	defer release()
	rows, err := asctrust.LoadSessions(callCtx, conn, u.LowerName)
	if err != nil {
		s.Problem = trustweb.FailureOf("admin sessions", err).Localized(ctx)
		return s
	}
	s.Rows = rows
	return s
}

// ------------------------------------------------------------------------------------------------ the actions

// adminUserAction is the start of Reset 2FA, Suspend and Unsuspend: another person, a reason, and a place to write what was done.
// It returns the person and the reason, or writes the answer and returns nil.
func adminUserAction(ctx *context.Context) (*user_model.User, string) {
	u, err := user_model.GetUserByID(ctx, ctx.ParamsInt64(":userid"))
	if err != nil {
		if user_model.IsErrUserNotExist(err) {
			ctx.Redirect(setting.AppSubURL + "/admin/users")
		} else {
			ctx.ServerError("GetUserByID", err)
		}
		return nil, ""
	}
	back := setting.AppSubURL + "/admin/users/" + url.PathEscape(ctx.Params(":userid"))
	if u.ID == ctx.Doer.ID {
		ctx.Flash.Error(ctx.Tr("asc_admin.person_not_yourself"))
		ctx.Redirect(back)
		return nil, ""
	}
	reason := strings.TrimSpace(ctx.FormString("reason"))
	if reason == "" || utf8.RuneCountInString(reason) > adminReasonMax {
		ctx.Flash.Error(ctx.Tr("asc_admin.person_reason_needed", adminReasonMax))
		ctx.Redirect(back)
		return nil, ""
	}
	return u, reason
}

// recordAdminAction writes what an administrator did to a person and why: the server log, a notice the administrators see under
// Notices, and the audit log of asc (action is the host.user.* name asc's catalogue holds).
func recordAdminAction(ctx *context.Context, action, what string, u *user_model.User, reason string) {
	log.Info("admin %s: %s of %s: %s", ctx.Doer.Name, what, u.Name, reason)
	if err := system_model.CreateNotice(ctx, system_model.NoticeTask, "%s of %s by %s: %s", what, u.Name, ctx.Doer.Name, reason); err != nil {
		log.Error("admin user: the notice of %s of %s could not be written: %v", what, u.Name, err)
	}
	asctrust.RecordHostAudit(ctx, asctrust.HostAudit{
		Action: action, Actor: ctx.Doer.Name, Target: u.Name, Request: ctx.Req,
		Details: map[string]string{"reason": reason},
	})
}

// reset2FA takes away the second factors of a person: the authenticator and the security keys.
func reset2FA(ctx *context.Context, u *user_model.User) error {
	tf, err := auth.GetTwoFactorByUID(ctx, u.ID)
	if err != nil && !auth.IsErrTwoFactorNotEnrolled(err) {
		return err
	} else if tf != nil {
		if err := auth.DeleteTwoFactorByID(ctx, tf.ID, u.ID); err != nil {
			return err
		}
	}
	wn, err := auth.GetWebAuthnCredentialsByUID(ctx, u.ID)
	if err != nil {
		return err
	}
	for _, cred := range wn {
		if _, err := auth.DeleteCredential(ctx, cred.ID, u.ID); err != nil {
			return err
		}
	}
	return nil
}

// UserReset2FA serves POST /admin/users/{userid}/reset_2fa with a reason.
func UserReset2FA(ctx *context.Context) {
	u, reason := adminUserAction(ctx)
	if u == nil {
		return
	}
	if err := reset2FA(ctx, u); err != nil {
		ctx.ServerError("reset2FA", err)
		return
	}
	recordAdminAction(ctx, asctrust.AuditHostUser2FAReset, "Reset 2FA", u, reason)
	ctx.Flash.Success(ctx.Tr("asc_admin.person_2fa_reset", u.DisplayName()))
	ctx.Redirect(setting.AppSubURL + "/admin/users/" + url.PathEscape(ctx.Params(":userid")))
}

// UserSuspend serves POST /admin/users/{userid}/suspend and /unsuspend with a reason.
func UserSuspend(ctx *context.Context) {
	u, reason := adminUserAction(ctx)
	if u == nil {
		return
	}
	suspend := ctx.Params(":what") == "suspend"
	if err := user_service.UpdateAuth(ctx, u, &user_service.UpdateAuthOptions{ProhibitLogin: optional.Some(suspend)}); err != nil {
		ctx.ServerError("UpdateAuth", err)
		return
	}
	action, what, msg := asctrust.AuditHostUserUnsuspended, "Unsuspend", "asc_admin.person_unsuspended"
	if suspend {
		action, what, msg = asctrust.AuditHostUserSuspended, "Suspend", "asc_admin.person_suspended"
	}
	recordAdminAction(ctx, action, what, u, reason)
	ctx.Flash.Success(ctx.Tr(msg, u.DisplayName()))
	ctx.Redirect(setting.AppSubURL + "/admin/users/" + url.PathEscape(ctx.Params(":userid")))
}

// UserRevokeSession serves POST /admin/users/{userid}/sessions/revoke: ends one credential asc keeps for the person.
func UserRevokeSession(ctx *context.Context) {
	u, err := user_model.GetUserByID(ctx, ctx.ParamsInt64(":userid"))
	if err != nil {
		ctx.NotFound("GetUserByID", err)
		return
	}
	back := setting.AppSubURL + "/admin/users/" + url.PathEscape(ctx.Params(":userid")) + "?tab=" + userTabSessions
	conn, err := asctrust.Conn()
	if err != nil || !setting.Asc.Enabled {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("admin session credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	if err := asctrust.RevokeSession(callCtx, conn, u.LowerName, ctx.FormString("id"), asctrust.NewOpID()); err != nil {
		ctx.Flash.Error(trustweb.FailureOf("admin session revoke", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	log.Info("admin %s: revoked a credential of %s", ctx.Doer.Name, u.Name)
	ctx.Flash.Success(ctx.Tr("asc_admin.person_session_revoked"))
	ctx.Redirect(back)
}
