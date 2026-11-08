// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	gocontext "context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"
)

const (
	ascPaletteMinQuery = 2
	ascPaletteMaxQuery = 80
	ascPaletteEach     = 5
	// ascPaletteCodeTimeout bounds the symbol lookup of one keystroke; a slow asc serve leaves the Code group out.
	ascPaletteCodeTimeout = 1500 * time.Millisecond
)

// ascPaletteCodeSlots bounds the symbol lookups that run at once over the whole hub; past it the Code group is left out.
var ascPaletteCodeSlots = make(chan struct{}, 8)

// AscPaletteItem is one result of the command palette; Href is always a path of this hub.
type AscPaletteItem struct {
	Title string `json:"title"`
	Sub   string `json:"sub,omitempty"`
	Href  string `json:"href"`
}

// AscPaletteGroup is a heading and its results.
type AscPaletteGroup struct {
	Key   string           `json:"key"` // commands, changes, code, people or repos
	Label string           `json:"label"`
	Items []AscPaletteItem `json:"items"`
}

// AscPalette serves GET /-/asc/palette?q=&at=: what the palette shows while a person types. Changes and repositories
// are those the person may read, people are those the person may see; a visitor who is not signed in gets the public
// ones. Code is the symbol definitions asc finds in the repositories the person may read, each with its path and
// line, while the asc integration is on. At is the path of the page the palette is open on, for the commands of that
// repository or change; "verb" is true when q starts with a command verb, and the palette then shows its commands.
func AscPalette(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	if !ascPaletteLimiterFor(ctx).Allow(ascPaletteKey(ctx)) {
		ctx.Resp.Header().Set("Retry-After", "10")
		ctx.JSON(http.StatusTooManyRequests, map[string]any{"groups": []AscPaletteGroup{}})
		return
	}
	q := strings.TrimSpace(ctx.FormString("q"))
	if len(q) < ascPaletteMinQuery || len(q) > ascPaletteMaxQuery {
		ctx.JSON(http.StatusOK, map[string]any{"groups": []AscPaletteGroup{}})
		return
	}
	groups := []AscPaletteGroup{}
	cmds, verb := ascPaletteCommands(ctx, q, ctx.FormString("at"))
	if len(cmds.Items) > 0 {
		groups = append(groups, cmds)
	}
	if g := ascPaletteChanges(ctx, q); len(g.Items) > 0 {
		groups = append(groups, g)
	}
	// A visitor who is not signed in gets the groups the explore pages would show, no more.
	if ascPaletteExploreOpen(ctx) {
		if g := ascPaletteCode(ctx, q); len(g.Items) > 0 {
			groups = append(groups, g)
		}
		if g := ascPaletteRepos(ctx, q); len(g.Items) > 0 {
			groups = append(groups, g)
		}
		if !setting.Service.Explore.DisableUsersPage {
			if g := ascPalettePeople(ctx, q); len(g.Items) > 0 {
				groups = append(groups, g)
			}
		}
	}
	ctx.JSON(http.StatusOK, map[string]any{"groups": groups, "verb": verb})
}

// ascPaletteLimiter bounds the calls of one person (or one address) while they type: the client waits before it asks,
// this is the limit that holds when a client does not.
var (
	ascPaletteLimiter     = ascratelimit.New(40, 10*time.Second) // visitors, by address (an IPv6 address by its /64)
	ascPaletteUserLimiter = ascratelimit.New(40, 10*time.Second) // people who are signed in, by account: a flood of visitors cannot push them out
)

func ascPaletteLimiterFor(ctx *context.Context) *ascratelimit.Limiter {
	if ctx.IsSigned {
		return ascPaletteUserLimiter
	}
	return ascPaletteLimiter
}

func ascPaletteKey(ctx *context.Context) string {
	if ctx.IsSigned {
		return "u" + strconv.FormatInt(ctx.Doer.ID, 10)
	}
	return "ip" + ascratelimit.ClientKey(ctx.RemoteAddr())
}

// ascPaletteExploreOpen is false for a visitor who is not signed in where the explore pages ask for a sign in.
func ascPaletteExploreOpen(ctx *context.Context) bool {
	return ctx.IsSigned || !(setting.Service.RequireSignInView || setting.Service.Explore.RequireSigninView)
}

func ascPaletteChanges(ctx *context.Context, q string) AscPaletteGroup {
	g := AscPaletteGroup{Key: "changes", Label: ctx.Locale.TrString("asc_shell.palette_scope_changes")}
	var rows []struct {
		Name  string
		Index int64
		Owner string
		Repo  string
	}
	err := db.GetEngine(ctx).Table("issue").
		Select("issue.name AS name, issue.`index` AS `index`, repository.owner_name AS owner, repository.lower_name AS repo").
		Join("INNER", "repository", "repository.id = issue.repo_id").
		Where("issue.is_pull = ?", true).
		And(repo_model.AccessibleRepositoryCondition(ctx.Doer, unit.TypePullRequests)).
		And(db.BuildCaseInsensitiveLike("issue.name", q)).
		OrderBy("issue.updated_unix DESC").Limit(ascPaletteEach).Find(&rows)
	if err != nil {
		log.Error("asc palette: changes: %v", err)
		return g
	}
	for _, r := range rows {
		full := r.Owner + "/" + r.Repo
		g.Items = append(g.Items, AscPaletteItem{Title: r.Name, Sub: full + " #" + strconv.FormatInt(r.Index, 10), Href: setting.AppSubURL + "/" + full + "/pulls/" + strconv.FormatInt(r.Index, 10)})
	}
	return g
}

// ascPaletteCode looks q up as a symbol name over the repositories the person may read, definitions only. Any
// failure (no asc, busy, slow, a refused name) leaves the group empty: the "Search code" link still finds it.
func ascPaletteCode(ctx *context.Context, q string) AscPaletteGroup {
	g := AscPaletteGroup{Key: "code", Label: ctx.Locale.TrString("asc_shell.palette_scope_code")}
	// The same switches as the code search page: asc on, code not off for the site, the explore code page not disabled.
	if !setting.Asc.Enabled || unit.TypeCode.UnitGlobalDisabled() || setting.Service.Explore.DisableCodePage || !ascsearch.Plain(q, ascsearch.MaxName) {
		return g
	}
	select {
	case ascPaletteCodeSlots <- struct{}{}:
		defer func() { <-ascPaletteCodeSlots }()
	default:
		return g
	}
	svc, err := ascsearch.Default()
	if err != nil {
		log.Debug("asc palette: code: %v", err)
		return g
	}
	resolved, err := ascsearch.Resolve(ctx, ctx.Doer, nil, ascsearch.Selector{})
	if err != nil || len(resolved.Repos) == 0 {
		if err != nil {
			log.Error("asc palette: code: %v", err)
		}
		return g
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascPaletteCodeTimeout)
	defer cancel()
	res, err := svc.Symbols(callCtx, resolved.Repos, ascsearch.SymbolQuery{
		Name: q, Match: ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, DefinitionsOnly: true, Limit: ascPaletteEach,
	})
	if err != nil {
		log.Debug("asc palette: code: %v", err)
		return g
	}
	for _, h := range res.Hits {
		r, ok := resolved.ByName(h.Repo)
		if !ok {
			continue // asc named a repository Forgejo did not hand it
		}
		s := h.Symbol
		line := strconv.FormatUint(uint64(s.Line), 10)
		sub := r.FullName + " · " + s.Path + ":" + line
		if s.Kind != "" {
			sub = s.Kind + " · " + sub
		}
		href := r.Link + "/src/branch/" + util.PathEscapeSegments(r.Branch) + "/" + util.PathEscapeSegments(s.Path)
		if s.Line > 0 {
			href += "#L" + line
		}
		g.Items = append(g.Items, AscPaletteItem{Title: s.Name, Sub: sub, Href: href})
		if len(g.Items) == ascPaletteEach {
			break
		}
	}
	return g
}

func ascPaletteRepos(ctx *context.Context, q string) AscPaletteGroup {
	g := AscPaletteGroup{Key: "repos", Label: ctx.Locale.TrString("asc_shell.palette_scope_repos")}
	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: ascPaletteEach},
		Actor:       ctx.Doer,
		Keyword:     q,
		Private:     ctx.IsSigned,
		OrderBy:     db.SearchOrderByRecentUpdated,
	})
	if err != nil {
		log.Error("asc palette: repositories: %v", err)
		return g
	}
	for _, r := range repos {
		g.Items = append(g.Items, AscPaletteItem{Title: r.FullName(), Sub: r.Description, Href: setting.AppSubURL + "/" + r.FullName()})
	}
	return g
}

func ascPalettePeople(ctx *context.Context, q string) AscPaletteGroup {
	g := AscPaletteGroup{Key: "people", Label: ctx.Locale.TrString("asc_shell.palette_scope_people")}
	users, _, err := user_model.SearchUsers(ctx, &user_model.SearchUserOptions{
		Actor:       ctx.Doer,
		Keyword:     q,
		Type:        user_model.UserTypeIndividual,
		ListOptions: db.ListOptions{Page: 1, PageSize: ascPaletteEach},
		OrderBy:     "name ASC",
		IsActive:    optional.Some(true),
	})
	if err != nil {
		log.Error("asc palette: people: %v", err)
		return g
	}
	for _, u := range users {
		g.Items = append(g.Items, AscPaletteItem{Title: u.Name, Sub: u.FullName, Href: setting.AppSubURL + "/" + u.Name})
	}
	return g
}
