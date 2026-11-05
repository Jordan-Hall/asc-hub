// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/http"

	"forgejo.org/models/auth"
	user_model "forgejo.org/models/user"
	wa "forgejo.org/modules/auth/webauthn"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/context"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Sign in with a passkey (board Onboarding): the browser offers the passkeys it holds for this hub, the person picks
// one and unlocks it (a fingerprint, a face or the PIN of the device), and the hub signs them in without a name or a
// password. The key must verify the person: that check stands for both the password and the second factor. Any
// security key registered while ASC is on is asked to be a passkey when the device can keep one.

const ascPasskeySession = "ascPasskeyAssertion"

// ascPasskeyOn is whether the sign in page offers passkeys.
func ascPasskeyOn() bool {
	return setting.Asc.Enabled && setting.Service.EnableInternalSignIn
}

// AscPasskeyAssertion answers `GET /user/webauthn/passkey/assertion`: the challenge of a discoverable sign in, which
// names no account, so the browser lists every passkey it holds for the hub.
func AscPasskeyAssertion(ctx *context.Context) {
	if !ascPasskeyOn() {
		ctx.NotFound("AscPasskeyAssertion", nil)
		return
	}
	assertion, sessionData, err := wa.WebAuthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		ctx.ServerError("webauthn.BeginDiscoverableLogin", err)
		return
	}
	if err := ctx.Session.Set(ascPasskeySession, sessionData); err != nil {
		ctx.ServerError("Session.Set", err)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, assertion)
}

// errAscPasskeyUnknown is a passkey whose account is gone or whose user handle is not one the hub gave.
var errAscPasskeyUnknown = errors.New("no account for this passkey")

// ascPasskeyUser finds the account of a passkey from its user handle (the id the hub gave at registration) and checks
// that the credential is one of that account's.
func ascPasskeyUser(ctx *context.Context, found **user_model.User, cred **auth.WebAuthnCredential) webauthn.DiscoverableUserHandler {
	return func(rawID, userHandle []byte) (webauthn.User, error) {
		id, n := binary.Varint(userHandle)
		if n <= 0 || id <= 0 || !bytes.Equal((*wa.User)(&user_model.User{ID: id}).WebAuthnID(), userHandle) {
			return nil, errAscPasskeyUnknown
		}
		u, err := user_model.GetUserByID(ctx, id)
		if err != nil {
			return nil, errAscPasskeyUnknown
		}
		c, err := auth.GetWebAuthnCredentialByCredID(ctx, u.ID, rawID)
		if err != nil {
			return nil, errAscPasskeyUnknown
		}
		*found, *cred = u, c
		return (*wa.User)(u), nil
	}
}

// AscPasskeyLoginPost answers `POST /user/webauthn/passkey/login`: it checks the signed challenge and signs the person
// in. The query remember=on keeps them signed in, as the box of the form does.
func AscPasskeyLoginPost(ctx *context.Context) {
	if !ascPasskeyOn() {
		ctx.NotFound("AscPasskeyLoginPost", nil)
		return
	}
	sessionData, ok := ctx.Session.Get(ascPasskeySession).(*webauthn.SessionData)
	if !ok || sessionData == nil {
		ctx.Status(http.StatusForbidden)
		return
	}
	// a challenge is good for one answer. It is overwritten, not deleted: a session store does not save a session whose
	// last value was deleted, and the challenge is often the only value of a session that is not signed in.
	if err := ctx.Session.Set(ascPasskeySession, false); err != nil {
		ctx.ServerError("Session.Set", err)
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponse(ctx.Req)
	if err != nil {
		log.Info("Failed passkey sign in from %s: %v", ctx.RemoteAddr(), err)
		ctx.Status(http.StatusForbidden)
		return
	}
	var u *user_model.User
	var dbCred *auth.WebAuthnCredential
	_, cred, err := wa.WebAuthn.ValidatePasskeyLogin(ascPasskeyUser(ctx, &u, &dbCred), *sessionData, parsed)
	if err != nil {
		log.Info("Failed passkey sign in from %s: %v", ctx.RemoteAddr(), err)
		ctx.Status(http.StatusForbidden)
		return
	}
	if cred.Authenticator.CloneWarning {
		log.Info("Failed passkey sign in for %s from %s: cloned credential", u.Name, ctx.RemoteAddr())
		ctx.Status(http.StatusForbidden)
		return
	}
	if !u.IsActive || u.ProhibitLogin || !u.IsIndividual() {
		log.Info("Failed passkey sign in for %s from %s: the account may not sign in", u.Name, ctx.RemoteAddr())
		ctx.Status(http.StatusForbidden)
		return
	}
	// the password form refuses an account whose sign in source is turned off: so does a passkey
	source, err := auth.GetSourceByID(ctx, u.LoginSource)
	if err != nil || !source.IsActive {
		log.Info("Failed passkey sign in for %s from %s: the sign in source is off or gone", u.Name, ctx.RemoteAddr())
		ctx.Status(http.StatusForbidden)
		return
	}

	if dbCred.Legacy {
		dbCred.BackupEligible = parsed.Response.AuthenticatorData.Flags.HasBackupEligible()
		dbCred.BackupState = parsed.Response.AuthenticatorData.Flags.HasBackupState()
		dbCred.Legacy = false
		if err := dbCred.UpdateFromLegacy(ctx); err != nil {
			ctx.ServerError("UpdateFromLegacy", err)
			return
		}
	}
	dbCred.SignCount = cred.Authenticator.SignCount
	if err := dbCred.UpdateSignCount(ctx); err != nil {
		ctx.ServerError("UpdateSignCount", err)
		return
	}

	redirect := handleSignInFull(ctx, u, ctx.Req.URL.Query().Get("remember") == "on", false)
	if ctx.Written() {
		return
	}
	if redirect == "" {
		redirect = setting.AppSubURL + "/"
	}
	// the key was just used: an action that needs it again, like deleting the account, takes this for a while
	if err := ctx.Session.Set(auth.SignInSessionKey, int64(timeutil.TimeStampNow())); err != nil {
		ctx.ServerError("Session.Set", err)
		return
	}
	ctx.JSONRedirect(redirect)
}
