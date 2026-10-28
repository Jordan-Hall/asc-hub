// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package explore

import (
	"fmt"
	"net/http"
	"slices"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/sitemap"
	"forgejo.org/services/context"
)

const (
	// tplExploreRepos explore repositories page template
	tplExploreRepos        base.TplName = "explore/repos"
	relevantReposOnlyParam string       = "only_show_relevant"
)

// RepoSearchOptions when calling search repositories
type RepoSearchOptions struct {
	OwnerID          int64
	Private          bool
	Restricted       bool
	PageSize         int
	OnlyShowRelevant bool
	TplName          base.TplName
}

// RenderRepoSearch render repositories search page
// This function is also used to render the Admin Repository Management page.
func RenderRepoSearch(ctx *context.Context, opts *RepoSearchOptions) {
	// Sitemap index for sitemap paths
	page := int(ctx.ParamsInt64("idx"))
	isSitemap := ctx.Params("idx") != ""
	if page <= 1 {
		page = ctx.FormInt("page")
	}

	if page <= 0 {
		page = 1
	}

	if isSitemap {
		opts.PageSize = setting.UI.SitemapPagingNum
	}

	var (
		repos   []*repo_model.Repository
		count   int64
		err     error
		orderBy db.SearchOrderBy
	)

	sortOrder := ctx.FormString("sort")
	if sortOrder == "" {
		sortOrder = setting.UI.ExploreDefaultSort
	}

	if order, ok := repo_model.OrderByFlatMap[sortOrder]; ok {
		orderBy = order
	} else {
		sortOrder = "recentupdate"
		orderBy = db.SearchOrderByRecentUpdated
	}
	ctx.Data["SortType"] = sortOrder

	keyword := ctx.FormTrim("q")

	ctx.Data["OnlyShowRelevant"] = opts.OnlyShowRelevant

	topicOnly := ctx.FormBool("topic")
	ctx.Data["TopicOnly"] = topicOnly

	language := ctx.FormTrim("language")
	ctx.Data["Language"] = language
	ctx.Data["ExploreLanguages"] = exploreLanguages(ctx, language)
	if rows, trending, err := repo_model.ListAscTrending(ctx, ctx.Doer, 5); err == nil && len(rows) > 0 {
		type trend struct {
			Repo  *repo_model.Repository
			Count int64
		}
		list := make([]trend, 0, len(rows))
		for i, r := range rows {
			list = append(list, trend{Repo: trending[i], Count: r.Stars + r.Forks + r.Activity})
		}
		ctx.Data["TrendingRepos"] = list
	}
	if topics, _, err := repo_model.FindTopics(ctx, &repo_model.FindTopicOptions{ListOptions: db.ListOptions{PageSize: 12, Page: 1}}); err == nil {
		names := make([]string, 0, len(topics))
		for _, t := range topics {
			if t.RepoCount > 0 {
				names = append(names, t.Name)
			}
		}
		ctx.Data["PopularTopics"] = names
	}

	archived := ctx.FormOptionalBool("archived")
	ctx.Data["IsArchived"] = archived

	fork := ctx.FormOptionalBool("fork")
	ctx.Data["IsFork"] = fork

	mirror := ctx.FormOptionalBool("mirror")
	ctx.Data["IsMirror"] = mirror

	template := ctx.FormOptionalBool("template")
	ctx.Data["IsTemplate"] = template

	private := ctx.FormOptionalBool("private")
	ctx.Data["IsPrivate"] = private

	repos, count, err = repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{
			Page:     page,
			PageSize: opts.PageSize,
		},
		Actor:              ctx.Doer,
		OrderBy:            orderBy,
		Private:            opts.Private,
		Keyword:            keyword,
		OwnerID:            opts.OwnerID,
		AllPublic:          true,
		AllLimited:         true,
		TopicOnly:          topicOnly,
		Language:           language,
		IncludeDescription: setting.UI.SearchRepoDescription,
		OnlyShowRelevant:   opts.OnlyShowRelevant,
		Archived:           archived,
		Fork:               fork,
		Mirror:             mirror,
		Template:           template,
		IsPrivate:          private,
	})
	if err != nil {
		ctx.ServerError("SearchRepository", err)
		return
	}
	if isSitemap {
		m := sitemap.NewSitemap()
		for _, item := range repos {
			m.Add(sitemap.URL{URL: item.HTMLURL(), LastMod: item.UpdatedUnix.AsTimePtr()})
		}
		ctx.Resp.Header().Set("Content-Type", "text/xml")
		if _, err := m.WriteTo(ctx.Resp); err != nil {
			log.Error("Failed writing sitemap: %v", err)
		}
		return
	}

	ctx.Data["Keyword"] = keyword
	ctx.Data["Total"] = count
	ctx.Data["Repos"] = repos
	if ctx.Doer != nil && !setting.Repository.DisableStars {
		starred := make(map[int64]bool, len(repos))
		for _, r := range repos {
			starred[r.ID] = repo_model.IsStaring(ctx, ctx.Doer.ID, r.ID)
		}
		ctx.Data["StarredRepos"] = starred
	}
	ctx.Data["IsRepoIndexerEnabled"] = setting.Indexer.RepoIndexerEnabled

	pager := context.NewPagination(int(count), opts.PageSize, page, 5)
	pager.SetDefaultParams(ctx)
	pager.AddParam(ctx, "topic", "TopicOnly")
	pager.AddParam(ctx, "language", "Language")
	pager.AddParamString(relevantReposOnlyParam, fmt.Sprint(opts.OnlyShowRelevant))
	if has, value := archived.Get(); has {
		pager.AddParamString("archived", fmt.Sprint(value))
	}
	if has, value := fork.Get(); has {
		pager.AddParamString("fork", fmt.Sprint(value))
	}
	if has, value := mirror.Get(); has {
		pager.AddParamString("mirror", fmt.Sprint(value))
	}
	if has, value := template.Get(); has {
		pager.AddParamString("template", fmt.Sprint(value))
	}
	if has, value := private.Get(); has {
		pager.AddParamString("private", fmt.Sprint(value))
	}
	ctx.Data["Page"] = pager

	ctx.HTML(http.StatusOK, opts.TplName)
}

// Repos render explore repositories page
func Repos(ctx *context.Context) {
	ctx.Data["UsersPageIsDisabled"] = setting.Service.Explore.DisableUsersPage
	ctx.Data["OrganizationsPageIsDisabled"] = setting.Service.Explore.DisableOrganizationsPage
	ctx.Data["CodePageIsDisabled"] = setting.Service.Explore.DisableCodePage
	ctx.Data["Title"] = ctx.Tr("explore")
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreRepositories"] = true
	ctx.Data["IsRepoIndexerEnabled"] = setting.Indexer.RepoIndexerEnabled

	var ownerID int64
	if ctx.Doer != nil && !ctx.Doer.IsAdmin {
		ownerID = ctx.Doer.ID
	}

	onlyShowRelevant := setting.UI.OnlyShowRelevantRepos

	_ = ctx.Req.ParseForm() // parse the form first, to prepare the ctx.Req.Form field
	if len(ctx.Req.Form[relevantReposOnlyParam]) != 0 {
		onlyShowRelevant = ctx.FormBool(relevantReposOnlyParam)
	}

	RenderRepoSearch(ctx, &RepoSearchOptions{
		PageSize:         setting.UI.ExplorePagingNum,
		OwnerID:          ownerID,
		Private:          ctx.Doer != nil,
		TplName:          tplExploreRepos,
		OnlyShowRelevant: onlyShowRelevant,
	})
}

// exploreLanguages lists the primary languages of the repositories the viewer can see, for the Language filter, with
// the language the visitor already chose kept in the list. It uses the access rule of the repository search itself
// (public repositories of public owners, plus what the signed-in viewer is allowed to read), so a language that only
// a hidden repository has is never offered. At most 60 names.
func exploreLanguages(ctx *context.Context, chosen string) []string {
	var names []string
	err := db.GetEngine(ctx).Table("language_stat").
		Join("INNER", "repository", "`repository`.id = `language_stat`.repo_id").
		Where(repo_model.AccessibleRepositoryCondition(ctx.Doer, unit.TypeInvalid)).
		And("`language_stat`.is_primary = ? AND `language_stat`.language != ?", true, "other").
		Distinct("`language_stat`.language").OrderBy("`language_stat`.language").Limit(60).Find(&names)
	if err != nil {
		log.Error("listing the languages of the explore filter: %v", err)
		return nil
	}
	if chosen != "" && !slices.Contains(names, chosen) {
		names = append(names, chosen)
	}
	return names
}
