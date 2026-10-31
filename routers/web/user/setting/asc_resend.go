// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"fmt"
	"strconv"
	"time"

	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The wait between two confirmation mails (the cache key "MailResendLimit_<name>", kept for mailResendWait seconds). The
// hub used to refuse a second request without a word; the page now says when the person may ask again, so the time of
// the last mail is kept next to the limit.
const (
	mailResendWait   = 180
	mailResendAtKey  = "MailResendAt_"
	mailResendLimKey = "MailResendLimit_"
)

// markMailResend records that a confirmation mail was just sent to the account name (lower case).
func markMailResend(ctx *context.Context, lowerName string, now time.Time) {
	if err := ctx.Cache.Put(mailResendLimKey+lowerName, lowerName, mailResendWait); err != nil {
		log.Error("Set cache(MailResendLimit) fail: %v", err)
	}
	if err := ctx.Cache.Put(mailResendAtKey+lowerName, strconv.FormatInt(now.Unix(), 10), mailResendWait); err != nil {
		log.Error("Set cache(MailResendAt) fail: %v", err)
	}
}

// mailResendRemaining is how long the account must still wait before a new confirmation mail; ok is false when it need
// not wait. A limit whose time was not recorded (set by an older hub) is the whole wait.
func mailResendRemaining(ctx *context.Context, lowerName string, now time.Time) (wait time.Duration, ok bool) {
	if !ctx.Cache.IsExist(mailResendLimKey + lowerName) {
		return 0, false
	}
	wait = mailResendWait * time.Second
	if v := ctx.Cache.Get(mailResendAtKey + lowerName); v != nil {
		if at, err := strconv.ParseInt(fmt.Sprint(v), 10, 64); err == nil {
			wait = max(time.Second, time.Duration(mailResendWait)*time.Second-now.Sub(time.Unix(at, 0)))
			wait = min(wait, mailResendWait*time.Second)
		}
	}
	return wait.Round(time.Second), true
}

// mailResendText says the wait in words: "45 seconds" or "2 minutes".
func mailResendText(ctx *context.Context, wait time.Duration) string {
	secs := int(wait.Seconds())
	if secs < 60 {
		n := int64(max(secs, 1))
		return string(ctx.Locale.TrN(n, "settings.resend_wait_seconds_one", "settings.resend_wait_seconds_other", n))
	}
	n := int64((secs + 59) / 60)
	return string(ctx.Locale.TrN(n, "settings.resend_wait_minutes_one", "settings.resend_wait_minutes_other", n))
}
