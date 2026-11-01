// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package method

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web/middleware"
	"forgejo.org/services/auth"
)

// errBrowserSessionRevoked: the person signed this browser out from another one.
var errBrowserSessionRevoked = errors.New("the session was signed out from another browser")

// Ensure the struct implements the interface.
var (
	_ auth.Method = &Session{}
)

// Session checks if there is a user uid stored in the session and returns the user
// object for that uid.
type Session struct{}

// Verify checks if there is a user uid stored in the session and returns the user
// object for that uid.
// Returns nil if there is no user uid stored in the session.
func (s *Session) Verify(req *http.Request, w http.ResponseWriter, sess auth.SessionStore) auth.MethodOutput {
	if sess == nil {
		return &auth.AuthenticationNotAttempted{InteractiveReauthenticationPossible: true}
	}

	// Get user ID
	uid := sess.Get("uid")
	if uid == nil {
		return &auth.AuthenticationNotAttempted{InteractiveReauthenticationPossible: true}
	}
	log.Trace("Session Authorization: Found user[%d]", uid)

	id, ok := uid.(int64)
	if !ok {
		return &auth.AuthenticationNotAttempted{InteractiveReauthenticationPossible: true}
	}

	// Get user object
	user, err := user_model.GetUserByID(req.Context(), id)
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			// Return the err as-is to keep current signed-in session, in case the err is something like context.Canceled. Otherwise non-existing user (nil, nil) will make the caller clear the signed-in session.
			return &auth.AuthenticationError{Error: fmt.Errorf("session auth GetUserByID: %w", err)}
		}
		return &auth.AuthenticationAttemptedIncorrectCredential{Error: err}
	}

	// The registry of signed-in browsers: it lists this one on the security page and says when it was signed out
	// from another.
	if withID, ok := sess.(interface{ ID() string }); ok {
		rememberKey, _, _ := strings.Cut(middleware.GetSiteCookie(req, setting.CookieRememberName), ":")
		revoked, err := auth_model.TouchBrowserSession(req.Context(), user.ID, withID.ID(), rememberKey, req.UserAgent(), remoteHost(req))
		if err != nil {
			return &auth.AuthenticationError{Error: fmt.Errorf("session auth TouchBrowserSession: %w", err)}
		}
		if revoked {
			// signed out from another browser: this one is signed out as if it had done it itself
			log.Trace("Session Authorization: %v", errBrowserSessionRevoked)
			_ = sess.Delete("uid")
			return &auth.AuthenticationNotAttempted{InteractiveReauthenticationPossible: true}
		}
	}

	// Local passwords kept for the admins only: a person who has no other way in than a password kept here is
	// signed out, whatever way the session began (a password, a reset, a sign-up or a remember-me cookie).
	if user.LoginSource == 0 && !user.IsAdmin && auth_model.LocalPasswordsAdminsOnly(req.Context()) {
		linked, err := db.GetEngine(req.Context()).Exist(&user_model.ExternalLoginUser{UserID: user.ID})
		if err != nil {
			return &auth.AuthenticationError{Error: fmt.Errorf("session auth ExternalLoginUser: %w", err)}
		}
		if !linked {
			log.Trace("Session Authorization: local passwords are for admins only")
			_ = sess.Delete("uid")
			return &auth.AuthenticationNotAttempted{InteractiveReauthenticationPossible: true}
		}
	}

	log.Trace("Session Authorization: Logged in user %-v", user)
	return &auth.AuthenticationSuccess{Result: &sessionAuthenticationResult{user: user}}
}

// remoteHost is the address of the request without its port.
func remoteHost(req *http.Request) string {
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return host
	}
	return req.RemoteAddr
}
