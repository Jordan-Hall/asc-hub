// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package method

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	auth_model "forgejo.org/models/auth"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	source_db "forgejo.org/services/auth/source/db"
)

// The wait after wrong passwords (board Onboarding). After [security] LOGIN_MAX_ATTEMPTS wrong passwords for one
// name from one address, that name is refused from that address for LOGIN_WAIT; an address that sends
// ascLoginAddrFactor times as many wrong passwords, whatever the names, waits too. The name counts as typed (trimmed,
// in lower case) and is not looked up: a count shared by a username and an email would tell a stranger which email
// belongs to which account. The address count bounds the tries across the names of one person. Both counts are taken
// before the password is checked, so tries sent at the same time cannot pass them; a try that succeeds or that the
// server fails gives its address try back. Counting per name and address keeps a stranger from locking a person out
// from elsewhere. Every
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

// loginName is the key a typed name counts against.
func loginName(name string) string {
	return "name:" + strings.ToLower(strings.TrimSpace(name))
}

// isWrongCredential says whether a sign-in failed on the name or the password (and not on the server).
func isWrongCredential(err error) bool {
	return errors.Is(err, util.ErrNotExist) || errors.Is(err, util.ErrInvalidArgument) ||
		user_model.IsErrUserNotExist(err) || user_model.IsErrEmailAddressNotExist(err) ||
		errors.As(err, &source_db.ErrUserPasswordInvalid{}) || errors.As(err, &source_db.ErrUserPasswordNotSet{})
}

// UserSignInFrom is UserSignIn for a sign-in sent from remoteAddr, with the wait after wrong passwords: it answers
// ErrLoginWait instead of checking the password while the name or the address waits, and when this wrong password
// starts the wait.
func UserSignInFrom(ctx context.Context, username, password, remoteAddr string) (*user_model.User, *auth_model.Source, error) {
	limit, wait := setting.LoginMaxAttempts, setting.LoginWait
	addr := ascratelimit.ClientKey(remoteAddr)
	left, addrTry := loginAddresses.Begin(addr, limit*ascLoginAddrFactor, wait)
	if left > 0 {
		return nil, nil, ErrLoginWait{Wait: left}
	}
	account := loginName(username) + "\x00" + addr
	left, accountTry := loginAccounts.Begin(account, limit, wait)
	if left > 0 {
		loginAddresses.Release(addrTry)
		return nil, nil, ErrLoginWait{Wait: left}
	}
	u, source, err := UserSignIn(ctx, username, password)
	if err == nil || !isWrongCredential(err) {
		// the password was right (a refused account is only told so with the right password), or the server failed
		loginAddresses.Release(addrTry)
		if err == nil || user_model.IsErrUserProhibitLogin(err) {
			loginAccounts.Clear(account)
		} else {
			loginAccounts.Release(accountTry)
		}
		return u, source, err
	}
	left = max(loginAccounts.Left(account), loginAddresses.Left(addr))
	if left > 0 {
		return nil, nil, ErrLoginWait{Wait: left}
	}
	return nil, nil, err
}
