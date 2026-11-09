// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package explore

import (
	"net/url"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/container"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
	"forgejo.org/services/context"
)

// Organizations render explore organizations page
func Organizations(ctx *context.Context) {
	if setting.Service.Explore.DisableOrganizationsPage {
		ctx.Redirect(setting.AppSubURL + "/explore")
		return
	}

	ctx.Data["UsersPageIsDisabled"] = setting.Service.Explore.DisableUsersPage
	ctx.Data["CodePageIsDisabled"] = setting.Service.Explore.DisableCodePage
	ctx.Data["Title"] = ctx.Tr("explore")
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreOrganizations"] = true
	ctx.Data["IsRepoIndexerEnabled"] = setting.Indexer.RepoIndexerEnabled

	visibleTypes := []structs.VisibleType{structs.VisibleTypePublic}
	if ctx.Doer != nil {
		visibleTypes = append(visibleTypes, structs.VisibleTypeLimited, structs.VisibleTypePrivate)
	}

	supportedSortOrders := container.SetOf(
		"newest",
		"oldest",
		"alphabetically",
		"reversealphabetically",
		"mostactive",
	)
	sortOrder := ctx.FormString("sort")
	if sortOrder == "" {
		if supportedSortOrders.Contains(setting.UI.ExploreDefaultSort) {
			sortOrder = setting.UI.ExploreDefaultSort
		} else {
			sortOrder = "newest"
		}
		ctx.SetFormString("sort", sortOrder)
	}

	opts := &user_model.SearchUserOptions{
		Actor:       ctx.Doer,
		Type:        user_model.UserTypeOrganization,
		ListOptions: db.ListOptions{PageSize: setting.UI.ExplorePagingNum},
		Visible:     visibleTypes,

		SupportedSortOrders: supportedSortOrders,
		ExtraParamStrings:   map[string]string{},
	}
	readOrgFilters(ctx, opts)
	RenderUserSearch(ctx, opts, tplExploreUsers)
}

// ExploreOrgChip is a chip of the organization directory: a link to the list with that one filter.
type ExploreOrgChip struct {
	Key     string
	Href    string
	Current bool
}

// exploreOrgOnly is the chips of the organization directory, in order. "member" is for a signed-in viewer only.
var exploreOrgOnly = []string{"all", "verified", "agents", "member"}

// readOrgFilters reads the chips (only=all|verified|agents|member) and the Size (size=small|medium|large) of the
// organization directory into the search, and puts what the filter bar shows into the page data. An unknown value is
// All or Any.
func readOrgFilters(ctx *context.Context, opts *user_model.SearchUserOptions) {
	only := ctx.FormTrim("only")
	if only == "member" && ctx.Doer == nil {
		only = ""
	}
	switch only {
	case "verified":
		opts.AscOrg.Verified = true
	case "agents":
		opts.AscOrg.WithAgents = true
	case "member":
		opts.AscOrg.ViewerMember = true
	default:
		only = "all"
	}
	opts.AscOrg.Viewer = ctx.Doer
	opts.AscOrg.Size = user_model.ParseAscOrgSize(ctx.FormTrim("size"))
	if only != "all" {
		opts.ExtraParamStrings["only"] = only
	}
	if opts.AscOrg.Size != user_model.AscOrgSizeAny {
		opts.ExtraParamStrings["size"] = string(opts.AscOrg.Size)
	}

	chips := make([]ExploreOrgChip, 0, len(exploreOrgOnly))
	for _, key := range exploreOrgOnly {
		if key == "member" && ctx.Doer == nil {
			continue
		}
		q := url.Values{}
		if key != "all" {
			q.Set("only", key)
		}
		for _, name := range []string{"q", "sort", "size"} {
			if v := ctx.FormTrim(name); v != "" {
				q.Set(name, v)
			}
		}
		href := "?"
		if len(q) > 0 {
			href += q.Encode()
		}
		chips = append(chips, ExploreOrgChip{Key: key, Href: href, Current: key == only})
	}
	ctx.Data["ExploreOrgOnly"] = only
	ctx.Data["ExploreOrgChips"] = chips
	ctx.Data["ExploreOrgSize"] = string(opts.AscOrg.Size)
	ctx.Data["ExploreOrgSizes"] = user_model.AscOrgSizes
	ctx.Data["ExploreOrgsFiltered"] = only != "all" || opts.AscOrg.Size != user_model.AscOrgSizeAny
}
