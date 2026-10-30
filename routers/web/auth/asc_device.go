// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

// The device authorisation grant of the asc command line (RFC 8628). The terminal calls AscDeviceCode and polls
// AscDeviceToken; the person opens AscDevicePage in a browser session, types or confirms the code, and approves
// or denies it. Approving makes a personal access token for the person, handed to the terminal once.

const (
	tplAscDevice base.TplName = "user/auth/asc_device"

	ascDeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
)

// ascLimiter allows n events per window for each key. It guards three surfaces: asking for a device code and polling
// for the token (both unauthenticated), and typing user codes that do not exist. It is bounded: every window it
// drops the keys with no recent event, and it never holds more than maxKeys keys (the key with the oldest last event
// goes first), so a flood of distinct keys cannot grow it without limit.
type ascLimiter struct {
	mu        sync.Mutex
	n         int
	maxKeys   int
	window    time.Duration
	hits      map[string][]time.Time
	lastSweep time.Time
	now       func() time.Time
}

func newAscLimiter(n int, window time.Duration) *ascLimiter {
	return &ascLimiter{n: n, maxKeys: 10000, window: window, hits: map[string][]time.Time{}, now: time.Now}
}

// blocked reports whether the key used up its events in the window.
func (l *ascLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.n
}

// add counts one event for the key.
func (l *ascLimiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep()
	if _, known := l.hits[key]; !known && len(l.hits) >= l.maxKeys {
		l.evictOldest()
	}
	l.hits[key] = append(l.recent(key), l.now())
}

// size is the number of keys held.
func (l *ascLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hits)
}

func (l *ascLimiter) recent(key string) []time.Time {
	cut := l.now().Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.hits, key)
	}
	return kept
}

// sweep drops every key without a recent event, at most once per window.
func (l *ascLimiter) sweep() {
	now := l.now()
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	l.lastSweep = now
	for key := range l.hits {
		l.recent(key)
	}
}

func (l *ascLimiter) evictOldest() {
	var oldest string
	var oldestAt time.Time
	for key, hits := range l.hits {
		last := hits[len(hits)-1]
		if oldest == "" || last.Before(oldestAt) {
			oldest, oldestAt = key, last
		}
	}
	delete(l.hits, oldest)
}

var (
	ascDeviceAsks     = newAscLimiter(20, time.Minute)
	ascDevicePolls    = newAscLimiter(120, time.Minute)
	ascDeviceBadCodes = newAscLimiter(10, 5*time.Minute) // per account
	ascDeviceBadByIP  = newAscLimiter(30, 5*time.Minute) // per client address
)

// ascClientIP is the address of the client without its port (an IPv6 address stands for its /64). The proxy middleware has already replaced the remote
// address with the forwarded one when the request came from a trusted proxy, so this is the real client, and one
// client's connections (each with its own port) share one bucket.
func ascClientIP(ctx *context.Context) string {
	addr := ctx.RemoteAddr()
	host := strings.Trim(addr, "[]")
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() != nil {
		return host
	}
	// A person or a host controls a whole IPv6 /64, so one address of a /64 must not be a fresh bucket each time.
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func ascDeviceError(ctx *context.Context, status int, code, description string) {
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.JSON(status, map[string]string{"error": code, "error_description": description})
}

// AscDeviceCode serves POST /login/device/code: the terminal asks to be authorised.
func AscDeviceCode(ctx *context.Context) {
	ip := ascClientIP(ctx)
	if ascDeviceAsks.blocked(ip) {
		ctx.Resp.Header().Set("Retry-After", "60")
		ascDeviceError(ctx, http.StatusTooManyRequests, "slow_down", "too many requests, wait a minute")
		return
	}
	ascDeviceAsks.add(ip)
	scope, err := auth_model.ParseAscDeviceScope(ctx.FormString("scope"))
	if err != nil {
		ascDeviceError(ctx, http.StatusBadRequest, "invalid_scope", "scope must name only repository, changes or runs (read or write), read:organization or read:user")
		return
	}
	row, deviceCode, err := auth_model.CreateAscDeviceCode(ctx, ctx.FormString("client_name"), ip)
	if err != nil {
		if errors.Is(err, auth_model.ErrAscDeviceCodeTooMany) {
			ctx.Resp.Header().Set("Retry-After", "60")
			ascDeviceError(ctx, http.StatusTooManyRequests, "slow_down", "too many authorisations are waiting, try again later")
			return
		}
		if errors.Is(err, auth_model.ErrAscDeviceCodeBadDevice) {
			ascDeviceError(ctx, http.StatusBadRequest, "invalid_request", "client_name is not valid")
			return
		}
		log.Error("asc device code: %v", err)
		ascDeviceError(ctx, http.StatusInternalServerError, "server_error", "could not start the authorisation")
		return
	}
	if err := row.SetScope(ctx, scope); err != nil {
		log.Error("asc device code scope: %v", err)
		ascDeviceError(ctx, http.StatusInternalServerError, "server_error", "could not start the authorisation")
		return
	}
	verify := setting.AppURL + "login/device"
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 auth_model.FormatAscUserCode(row.UserCode),
		"verification_uri":          verify,
		"verification_uri_complete": verify + "?user_code=" + auth_model.FormatAscUserCode(row.UserCode),
		"expires_in":                int(auth_model.AscDeviceCodeLifetime.Seconds()),
		"interval":                  int(auth_model.AscDeviceCodeInterval.Seconds()),
	})
}

// AscDeviceToken serves POST /login/device/token: the terminal polls for the answer.
func AscDeviceToken(ctx *context.Context) {
	if ctx.FormString("grant_type") != ascDeviceGrantType {
		ascDeviceError(ctx, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be "+ascDeviceGrantType)
		return
	}
	ip := ascClientIP(ctx)
	if ascDevicePolls.blocked(ip) {
		ctx.Resp.Header().Set("Retry-After", "60")
		ascDeviceError(ctx, http.StatusTooManyRequests, "slow_down", "too many requests, wait a minute")
		return
	}
	ascDevicePolls.add(ip)
	code := strings.TrimSpace(ctx.FormString("device_code"))
	if code == "" {
		ascDeviceError(ctx, http.StatusBadRequest, "invalid_request", "device_code is missing")
		return
	}
	token, err := auth_model.ExchangeAscDeviceCode(ctx, code, auth_model.AscDeviceDefaultScope)
	switch {
	case err == nil:
		ctx.Resp.Header().Set("Cache-Control", "no-store")
		ctx.JSON(http.StatusOK, map[string]any{"access_token": token.Token, "token_type": "bearer", "scope": strings.ReplaceAll(string(token.Scope), ",", " "), "expires_in": int(time.Until(token.ExpiresUnix.AsTime()).Seconds())})
	case errors.Is(err, auth_model.ErrAscDeviceCodePending):
		ascDeviceError(ctx, http.StatusBadRequest, "authorization_pending", "the person has not answered yet")
	case errors.Is(err, auth_model.ErrAscDeviceCodeSlowDown):
		ascDeviceError(ctx, http.StatusBadRequest, "slow_down", "poll no faster than the interval")
	case errors.Is(err, auth_model.ErrAscDeviceCodeDenied):
		ascDeviceError(ctx, http.StatusBadRequest, "access_denied", "the person refused")
	case errors.Is(err, auth_model.ErrAscDeviceCodeExpired):
		ascDeviceError(ctx, http.StatusBadRequest, "expired_token", "the code expired, start again")
	case errors.Is(err, auth_model.ErrAscDeviceCodeNotFound):
		ascDeviceError(ctx, http.StatusBadRequest, "invalid_grant", "the device code is not valid")
	default:
		log.Error("asc device token: %v", err)
		ascDeviceError(ctx, http.StatusInternalServerError, "server_error", "could not exchange the code")
	}
}

// AscDevicePage serves GET /login/device: the person types the code, or lands here with it in the address and sees
// who asked.
func AscDevicePage(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_device.title")
	typed := ctx.FormString("user_code")
	if typed == "" {
		ctx.Data["DeviceStep"] = "enter"
		ctx.HTML(http.StatusOK, tplAscDevice)
		return
	}
	row, ok := ascDeviceLookup(ctx, typed)
	if !ok {
		return
	}
	if row.Status != auth_model.AscDeviceCodePending {
		ascDeviceShowDecided(ctx, row)
		return
	}
	ctx.Data["DeviceStep"] = "confirm"
	ctx.Data["DeviceCode"] = auth_model.FormatAscUserCode(row.UserCode)
	ctx.Data["DeviceName"] = row.DeviceName
	var scopes []string
	for _, sc := range row.EffectiveScope().StringSlice() {
		scopes = append(scopes, ctx.Locale.TrString("asc_device.scope_"+strings.ReplaceAll(sc, ":", "_")))
	}
	ctx.Data["DeviceScopes"] = scopes
	ctx.Data["DeviceTokenDays"] = int(auth_model.AscDeviceTokenLifetime.Hours() / 24)
	ctx.Data["DeviceMinutes"] = max(1, int(time.Until(row.ExpiresUnix.AsTime()).Minutes()+0.5))
	ctx.HTML(http.StatusOK, tplAscDevice)
}

// AscDevicePost serves POST /login/device: the person approves or denies the code.
func AscDevicePost(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("asc_device.title")
	row, ok := ascDeviceLookup(ctx, ctx.FormString("user_code"))
	if !ok {
		return
	}
	approve := ctx.FormString("action") == "approve"
	if !approve && ctx.FormString("action") != "deny" {
		ctx.Error(http.StatusBadRequest)
		return
	}
	if err := row.Decide(ctx, ctx.Doer.ID, approve); err != nil {
		switch {
		case errors.Is(err, auth_model.ErrAscDeviceCodeExpired):
			ascDeviceFail(ctx, "expired")
		case errors.Is(err, auth_model.ErrAscDeviceCodeDecided):
			ascDeviceFail(ctx, "decided")
		default:
			ctx.ServerError("AscDevicePost", err)
		}
		return
	}
	if approve {
		ctx.Data["DeviceStep"] = "approved"
	} else {
		ctx.Data["DeviceStep"] = "denied"
	}
	ctx.Data["DeviceName"] = row.DeviceName
	ctx.HTML(http.StatusOK, tplAscDevice)
}

// ascDeviceLookup finds the code a person typed. A wrong code is counted: after ten in five minutes the person
// waits, so the short code cannot be guessed.
func ascDeviceLookup(ctx *context.Context, typed string) (*auth_model.AscDeviceCode, bool) {
	key, ip := ctx.Doer.LowerName, ascClientIP(ctx)
	if ascDeviceBadCodes.blocked(key) || ascDeviceBadByIP.blocked(ip) {
		ctx.Resp.Header().Set("Retry-After", "300")
		ascDeviceFail(ctx, "limited")
		return nil, false
	}
	row, err := auth_model.GetAscDeviceCodeByUserCode(ctx, typed)
	switch {
	case err == nil:
		return row, true
	case errors.Is(err, auth_model.ErrAscDeviceCodeExpired):
		ascDeviceFail(ctx, "expired")
	case errors.Is(err, auth_model.ErrAscDeviceCodeNotFound):
		ascDeviceBadCodes.add(key)
		ascDeviceBadByIP.add(ip)
		ctx.Data["DeviceTyped"] = typed
		ascDeviceFail(ctx, "unknown")
	default:
		ctx.ServerError("ascDeviceLookup", err)
	}
	return nil, false
}

func ascDeviceShowDecided(ctx *context.Context, row *auth_model.AscDeviceCode) {
	if row.Status == auth_model.AscDeviceCodeDenied {
		ascDeviceFail(ctx, "denied")
		return
	}
	ascDeviceFail(ctx, "decided")
}

// ascDeviceFail renders the code entry again with the reason in words. The status stays 200 for a person's typing
// mistake and is 429 for the wait, 410 for an expired code.
func ascDeviceFail(ctx *context.Context, reason string) {
	ctx.Data["DeviceStep"] = "enter"
	ctx.Data["DeviceProblem"] = reason
	status := http.StatusOK
	switch reason {
	case "limited":
		status = http.StatusTooManyRequests
	case "expired":
		status = http.StatusGone
	}
	ctx.HTML(status, tplAscDevice)
}
