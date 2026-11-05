// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package method

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	source_db "forgejo.org/services/auth/source/db"
)

// The wait after wrong passwords (board Onboarding). After [security] LOGIN_MAX_ATTEMPTS wrong passwords for one
// account from one address, that account is refused from that address for LOGIN_WAIT; an address that sends
// ascLoginAddrFactor times as many wrong passwords, whatever the names, waits too. The account is the person the name
// typed resolves to (the username or any activated email of theirs), else the name itself, so every spelling counts
// the same. Counting per account and address keeps a stranger from locking a person out from elsewhere. Every
// password sign-in goes through UserSignInFrom: the sign-in page, linking an account, connecting an OpenID and basic
// authentication. The tables are in memory, per process.

// ascLoginAddrFactor is how many times LOGIN_MAX_ATTEMPTS one address may get wrong, across names, before it waits.
const ascLoginAddrFactor = 10

var (
	loginAccounts  = ascratelimit.NewLockout()
	loginAddresses = ascratelimit.NewLockout()
)

// ErrLoginWait is the answer to a sign-in that must wait.
type ErrLoginWait struct {
	Wait time.Duration
}

func (e ErrLoginWait) Error() string {
	return fmt.Sprintf("too many wrong passwords: try again in %s", e.Wait.Round(time.Second))
}

// ResetLoginWaits forgets every count, for tests.
func ResetLoginWaits() {
	loginAccounts, loginAddresses = ascratelimit.NewLockout(), ascratelimit.NewLockout()
}

// loginAccount is the account a typed name counts against.
func loginAccount(ctx context.Context, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(name, "@") {
		email := user_model.EmailAddress{LowerEmail: name}
		if has, err := db.GetEngine(ctx).Get(&email); err == nil && has {
			return "uid:" + strconv.FormatInt(email.UID, 10)
		}
	} else if u, err := user_model.GetUserByName(ctx, name); err == nil {
		return "uid:" + strconv.FormatInt(u.ID, 10)
	}
	return "name:" + name
}

// isWrongCredential says whether a sign-in failed on the name or the password (and not on the server).
func isWrongCredential(err error) bool {
	return errors.Is(err, util.ErrNotExist) || errors.Is(err, util.ErrInvalidArgument) ||
		user_model.IsErrUserNotExist(err) || user_model.IsErrEmailAddressNotExist(err) ||
		errors.As(err, &source_db.ErrUserPasswordInvalid{}) || errors.As(err, &source_db.ErrUserPasswordNotSet{})
}

// UserSignInFrom is UserSignIn for a sign-in sent from remoteAddr, with the wait after wrong passwords: it answers
// ErrLoginWait instead of checking the password while the account or the address waits, and when this wrong password
// starts the wait.
func UserSignInFrom(ctx context.Context, username, password, remoteAddr string) (*user_model.User, *auth_model.Source, error) {
	limit, wait := setting.LoginMaxAttempts, setting.LoginWait
	addr := ascratelimit.ClientKey(remoteAddr)
	if left := loginAddresses.Left(addr); left > 0 {
		return nil, nil, ErrLoginWait{Wait: left}
	}
	account := loginAccount(ctx, username) + "\x00" + addr
	if left := loginAccounts.Begin(account, limit, wait); left > 0 {
		return nil, nil, ErrLoginWait{Wait: left}
	}
	u, source, err := UserSignIn(ctx, username, password)
	if err == nil || !isWrongCredential(err) {
		// the password was right (a refused account is only told so with the right password), or the server failed
		if err == nil || user_model.IsErrUserProhibitLogin(err) {
			loginAccounts.Clear(account)
		}
		return u, source, err
	}
	left := max(loginAccounts.Left(account), loginAddresses.Fail(addr, limit*ascLoginAddrFactor, wait))
	if left > 0 {
		return nil, nil, ErrLoginWait{Wait: left}
	}
	return nil, nil, err
}
