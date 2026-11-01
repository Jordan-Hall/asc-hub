// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"fmt"
	"slices"
	"strconv"

	"forgejo.org/models/auth"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"

	"github.com/markbates/goth"
)

// probeOAuthCallback finishes an admin's test sign-in of an OAuth source: the claims go back to the admin page and
// nobody is signed in or made. It says whether the callback was a test.
func probeOAuthCallback(ctx *context.Context, source *auth.Source) bool {
	testing, _ := ctx.Session.Get(auth.ProbeOAuthSessionKey).(int64)
	if testing == 0 {
		return false
	}
	_ = ctx.Session.Delete(auth.ProbeOAuthSessionKey)
	if testing != source.ID || ctx.Doer == nil || !ctx.Doer.IsAdmin {
		return false
	}
	res := auth.ProbeResult{SourceID: source.ID}
	if gothUser, err := oAuth2FetchUser(ctx, source, ctx.Req, ctx.Resp); err != nil {
		res.Error = err.Error()
	} else {
		res.Claims = gothClaims(gothUser)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		ctx.ServerError("json.Marshal", err)
		return true
	}
	if err := ctx.Session.Set(auth.ProbeResultSessionKey, string(raw)); err != nil {
		ctx.ServerError("Session.Set", err)
		return true
	}
	ctx.Redirect(setting.AppSubURL + "/admin/auths/" + strconv.FormatInt(source.ID, 10) + "#auth-test")
	return true
}

// gothClaims lists what the provider said: the usual fields, then every raw claim by name.
func gothClaims(u goth.User) []auth.ProbeClaim {
	out := []auth.ProbeClaim{
		{Name: "user_id", Value: u.UserID},
		{Name: "email", Value: u.Email},
		{Name: "nickname", Value: u.NickName},
		{Name: "name", Value: u.Name},
		{Name: "first_name", Value: u.FirstName},
		{Name: "last_name", Value: u.LastName},
		{Name: "avatar_url", Value: u.AvatarURL},
	}
	keys := make([]string, 0, len(u.RawData))
	for k := range u.RawData {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := u.RawData[k]
		s, ok := v.(string)
		if !ok {
			if b, err := json.Marshal(v); err == nil {
				s = string(b)
			} else {
				s = fmt.Sprint(v)
			}
		}
		out = append(out, auth.ProbeClaim{Name: "raw." + k, Value: s})
	}
	return out
}
