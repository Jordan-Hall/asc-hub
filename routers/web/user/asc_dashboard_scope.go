// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/url"
	"slices"
	"strings"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"
)

// The scope chips of the dashboard: Everything, Personal (repositories of the person) and one chip for each organization
// the person works in. The choice filters the whole page and is kept as a setting of the person.

const (
	ascDashScopeAll      = "all"
	ascDashScopePersonal = "personal"
	ascDashScopeKey      = "asc.dash_scope"
	ascDashScopeMax      = 8 // organization chips
)

// AscDashScope is one chip.
type AscDashScope struct {
	Key    string // "all", "personal" or "org:<name>"
	Label  string
	Active bool
	Link   string
}

// ascOwnerOf returns the owner of "owner/name" or of a link such as "/owner/name/pulls/3".
func ascOwnerOf(s string) string {
	s = strings.TrimPrefix(s, setting.AppSubURL)
	s = strings.TrimPrefix(s, "/")
	owner, _, _ := strings.Cut(s, "/")
	return owner
}

// ascScopeKeep reports whether something owned by owner is inside the scope.
func ascScopeKeep(scope, doerName, owner string) bool {
	switch {
	case scope == "" || scope == ascDashScopeAll:
		return true
	case scope == ascDashScopePersonal:
		return strings.EqualFold(owner, doerName)
	case strings.HasPrefix(scope, "org:"):
		return strings.EqualFold(owner, strings.TrimPrefix(scope, "org:"))
	}
	return true
}

// ascDashScopes builds the chips from the owners the person works with (the person's own name is the Personal chip) and
// resolves the scope: the query wins and is remembered, else the remembered one, else Everything. A scope that is not
// offered is Everything.
func ascDashScopes(ctx *context.Context, doer *user_model.User, owners []string, l translation.Locale) (scopes []AscDashScope, scope string) {
	orgs := make([]string, 0, len(owners))
	for _, o := range owners {
		if o != "" && !strings.EqualFold(o, doer.Name) && !slices.ContainsFunc(orgs, func(x string) bool { return strings.EqualFold(x, o) }) {
			orgs = append(orgs, o)
		}
	}
	slices.SortFunc(orgs, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	if len(orgs) > ascDashScopeMax {
		orgs = orgs[:ascDashScopeMax]
	}
	offered := map[string]string{ascDashScopeAll: l.TrString("user.asc_dash.scope_all"), ascDashScopePersonal: l.TrString("user.asc_dash.scope_personal")}
	keys := []string{ascDashScopeAll, ascDashScopePersonal}
	for _, o := range orgs {
		offered["org:"+o] = o
		keys = append(keys, "org:"+o)
	}

	scope = ctx.FormString("scope")
	if _, ok := offered[scope]; ok && scope != "" {
		if scope == ascDashScopeAll {
			_ = user_model.DeleteUserSetting(ctx, doer.ID, ascDashScopeKey)
		} else {
			_ = user_model.SetUserSetting(ctx, doer.ID, ascDashScopeKey, scope)
		}
	} else if saved, _ := user_model.GetUserSetting(ctx, doer.ID, ascDashScopeKey, ""); saved != "" && ctx.FormString("scope") == "" {
		scope = saved
	} else {
		scope = ascDashScopeAll
	}
	if _, ok := offered[scope]; !ok {
		scope = ascDashScopeAll
	}
	for _, k := range keys {
		scopes = append(scopes, AscDashScope{Key: k, Label: offered[k], Active: k == scope, Link: setting.AppSubURL + "/?scope=" + url.QueryEscape(k)})
	}
	return scopes, scope
}
