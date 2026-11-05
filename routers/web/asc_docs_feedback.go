// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"time"

	"forgejo.org/models/system"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascdocs"
	"forgejo.org/services/context"
)

// ascDocsFeedbackLimiter allows ten answers a minute for one address.
var ascDocsFeedbackLimiter = ascratelimit.New(10, time.Minute)

// AscDocsFeedbackPost serves POST /-/docs/{slug}/feedback: one answer to "Was this page helpful?" (board Docs), yes
// or no. It stores no reader, is limited per address, and goes back to the page, where the answer is thanked.
func AscDocsFeedbackPost(ctx *context.Context) {
	page, _, ok := ascdocs.Get(ctx.PathParamRaw("slug"))
	if !ok {
		ctx.NotFound("docs page", nil)
		return
	}
	var helpful bool
	switch ctx.FormString("helpful") {
	case "yes":
		helpful = true
	case "no":
	default:
		ctx.Error(http.StatusBadRequest, "helpful is yes or no")
		return
	}
	if !ascDocsFeedbackLimiter.Allow(ascratelimit.ClientKey(ctx.RemoteAddr())) {
		ctx.Resp.Header().Set("Retry-After", "60")
		ctx.Error(http.StatusTooManyRequests, "too many answers")
		return
	}
	if err := system.AddAscDocsFeedback(ctx, page.Slug, helpful); err != nil {
		ctx.ServerError("AddAscDocsFeedback", err)
		return
	}
	ctx.Redirect(setting.AppSubURL + "/-/docs/" + page.Slug + "?feedback=thanks#site-feedback")
}

// ascDocsFeedback puts the feedback state of a page into the data: thanked after an answer, and the counts for a site
// administrator.
func ascDocsFeedback(ctx *context.Context, slug string) error {
	ctx.Data["DocsFeedbackThanks"] = ctx.FormString("feedback") == "thanks"
	if !ctx.IsSigned || !ctx.Doer.IsAdmin {
		return nil
	}
	c, err := system.CountAscDocsFeedback(ctx, slug)
	ctx.Data["DocsFeedbackCount"] = c
	return err
}
