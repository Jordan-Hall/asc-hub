// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascratelimit"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
)

const (
	ascPaletteMinQuery = 2
	ascPaletteMaxQuery = 80
	ascPaletteEach     = 5
)

// AscPaletteItem is one result of the command palette; Href is always a path of this hub.
type AscPaletteItem struct {
	Title string `json:"title"`
	Sub   string `json:"sub,omitempty"`
	Href  string `json:"href"`
}

// AscPaletteGroup is a heading and its results.
type AscPaletteGroup struct {
	Key   string           `json:"key"` // changes, people or repos
	Label string           `json:"label"`
	Items []AscPaletteItem `json:"items"`
}

// AscPalette serves GET /-/asc/palette?q=: what the palette shows while a person types. Changes and repositories are
// those the person may read, people are those the person may see; a visitor who is not signed in gets the public ones.
func AscPalette(ctx *context.Context) {
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	if !ascPaletteLimiter.Allow(ascPaletteKey(ctx)) {
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
	if g := ascPaletteChanges(ctx, q); len(g.Items) > 0 {
		groups = append(groups, g)
	}
	// A visitor who is not signed in gets the groups the explore pages would show, no more.
	if ascPaletteExploreOpen(ctx) {
		if g := ascPaletteRepos(ctx, q); len(g.Items) > 0 {
			groups = append(groups, g)
		}
		if !setting.Service.Explore.DisableUsersPage {
			if g := ascPalettePeople(ctx, q); len(g.Items) > 0 {
				groups = append(groups, g)
			}
		}
	}
	ctx.JSON(http.StatusOK, map[string]any{"groups": groups})
}

// ascPaletteLimiter bounds the calls of one person (or one address) while they type: the client waits before it asks,
// this is the limit that holds when a client does not.
var ascPaletteLimiter = ascratelimit.New(40, 10*time.Second)

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
