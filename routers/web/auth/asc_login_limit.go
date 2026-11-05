// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forgejo.org/modules/cache"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/web/middleware"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

// The wait after wrong passwords (board Onboarding): after [security] LOGIN_MAX_ATTEMPTS wrong passwords for one
// name from one address, that name is refused from that address for LOGIN_WAIT, and the sign-in page names the wait.
// Counting per name and address keeps a stranger from locking a person out from elsewhere. The counter lives in the
// cache, so hub processes that share the cache share it; a sign-in that succeeds clears it.

// ascLoginKey is the cache key of a name signing in from an address. The name is the one typed (a username or an
// email), folded to lower case; the address drops its port.
func ascLoginKey(name, remoteAddr string) string {
	ip := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		ip = host
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(name)) + "\x00" + ip))
	return "asc-login:" + hex.EncodeToString(sum[:16])
}

// ascCacheInt reads a number from the cache: the memory cache keeps what was put, Redis and Memcache give a string.
func ascCacheInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	case []byte:
		i, err := strconv.ParseInt(string(n), 10, 64)
		return i, err == nil
	}
	return 0, false
}

// ascLoginWait is how long the name must still wait from this address; 0 when it may try.
func ascLoginWait(key string, now time.Time) time.Duration {
	c := cache.GetCache()
	if setting.LoginMaxAttempts == 0 || c == nil {
		return 0
	}
	until, ok := ascCacheInt(c.Get(key + ":until"))
	if !ok {
		return 0
	}
	return max(time.Unix(until, 0).Sub(now), 0)
}

// ascLoginFailed counts a wrong password and gives the wait it starts: LOGIN_WAIT at the last attempt, else 0.
func ascLoginFailed(key string, now time.Time) time.Duration {
	c := cache.GetCache()
	if setting.LoginMaxAttempts == 0 || c == nil {
		return 0
	}
	ttl := int64(setting.LoginWait / time.Second)
	var err error
	if c.IsExist(key) {
		err = c.Incr(key)
	} else {
		err = c.Put(key, 1, ttl)
	}
	if err != nil {
		log.Error("asc login: count a wrong password: %v", err)
		return 0
	}
	if n, _ := ascCacheInt(c.Get(key)); n < int64(setting.LoginMaxAttempts) {
		return 0
	}
	if err := c.Put(key+":until", now.Add(setting.LoginWait).Unix(), ttl); err != nil {
		log.Error("asc login: start the wait: %v", err)
		return 0
	}
	_ = c.Delete(key)
	return setting.LoginWait
}

// ascLoginSucceeded clears the count of the name from this address.
func ascLoginSucceeded(key string) {
	if c := cache.GetCache(); c != nil {
		_ = c.Delete(key)
	}
}

// ascWaitMinutes is a wait in whole minutes, rounded up, at least one.
func ascWaitMinutes(d time.Duration) int {
	return max(int((d+time.Minute-1)/time.Minute), 1)
}

// ascWrongPassword is the message of a wrong name or password, which says when the wait starts.
func ascWrongPassword(ctx *context.Context) template.HTML {
	if setting.LoginMaxAttempts == 0 {
		return ctx.Tr("form.username_password_incorrect")
	}
	m := ascWaitMinutes(setting.LoginWait)
	return ctx.Locale.TrN(m, "asc_auth.signin_wrong_1", "asc_auth.signin_wrong_n", setting.LoginMaxAttempts, m)
}

// ascRenderLoginWait answers a sign-in that must wait: 429 with Retry-After, and the sign-in page naming the wait.
func ascRenderLoginWait(ctx *context.Context, form *forms.SignInForm, wait time.Duration) {
	m := ascWaitMinutes(wait)
	ctx.Resp.Header().Set("Retry-After", strconv.Itoa(int((wait+time.Second-1)/time.Second)))
	middleware.AssignForm(form, ctx.Data)
	ctx.Flash.Error(ctx.Locale.TrN(m, "asc_auth.signin_wait_1", "asc_auth.signin_wait_n", m), true)
	ctx.HTML(http.StatusTooManyRequests, tplSignIn)
}
