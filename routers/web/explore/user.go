// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package explore

import (
	"bytes"
	gocontext "context"
	"net/http"
	"net/url"
	"strings"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/container"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/sitemap"
	"forgejo.org/modules/structs"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

const (
	// `tplExploreUsers` explore users page template.
	tplExploreUsers base.TplName = "explore/users"
)

var nullByte = []byte{0x00}

func isKeywordValid(keyword string) bool {
	return !bytes.Contains([]byte(keyword), nullByte)
}

// `RenderUserSearch` render user search page.
func RenderUserSearch(ctx *context.Context, opts *user_model.SearchUserOptions, tplName base.TplName) {
	RenderUserSearchWith(ctx, opts, tplName, nil)
}

// RenderUserSearchWith is RenderUserSearch with more data for the page: load is called with the users found, before
// the page is rendered.
func RenderUserSearchWith(ctx *context.Context, opts *user_model.SearchUserOptions, tplName base.TplName, load func([]*user_model.User)) {
	// Sitemap index for sitemap paths.
	opts.Page = int(ctx.ParamsInt64("idx"))
	isSitemap := ctx.Params("idx") != ""
	if opts.Page <= 1 {
		opts.Page = ctx.FormInt("page")
	}
	if opts.Page <= 1 {
		opts.Page = 1
	}

	if isSitemap {
		opts.PageSize = setting.UI.SitemapPagingNum
	}

	var (
		users []*user_model.User
		count int64
		err   error
	)

	sortOrder := ctx.FormString("sort")
	if sortOrder == "" {
		sortOrder = setting.UI.ExploreDefaultSort
	}
	ctx.Data["SortType"] = sortOrder

	orderBy := MapSortOrder(sortOrder)

	if orderBy == "" {
		// In case the `sortType` is not valid, we set it to `recentupdate`.
		sortOrder = "recentupdate"
		ctx.Data["SortType"] = "recentupdate"
		orderBy = MapSortOrder(sortOrder)
	}

	if opts.SupportedSortOrders != nil && !opts.SupportedSortOrders.Contains(sortOrder) {
		ctx.NotFound("unsupported sort order", nil)
		return
	}

	opts.Keyword = ctx.FormTrim("q")
	opts.OrderBy = orderBy
	if len(opts.Keyword) == 0 || isKeywordValid(opts.Keyword) {
		users, count, err = searchUsers(ctx, opts)
		if err != nil {
			if isSitemap || tplName != tplExploreUsers {
				ctx.ServerError("SearchUsers", err)
				return
			}
			// the directory keeps its header, search and filters and says in the list that it could not be loaded, with
			// the request id the log line carries
			log.Error("explore: searching accounts (request %s): %v", ctx.RequestIDForLog(), err)
			ctx.Data["Keyword"] = opts.Keyword
			ctx.Data["ExploreLoadFailed"] = true
			ctx.Data["ExploreRetryHref"] = "?" + ctx.Req.URL.RawQuery
			ctx.HTML(http.StatusInternalServerError, tplName)
			return
		}
	}
	if isSitemap {
		m := sitemap.NewSitemap()
		for _, item := range users {
			m.Add(sitemap.URL{URL: item.HTMLURL(), LastMod: item.UpdatedUnix.AsTimePtr()})
		}
		ctx.Resp.Header().Set("Content-Type", "text/xml")
		if _, err := m.WriteTo(ctx.Resp); err != nil {
			log.Error("Failed writing sitemap: %v", err)
		}
		return
	}

	ctx.Data["Keyword"] = opts.Keyword
	ctx.Data["Total"] = count
	ctx.Data["Users"] = users
	ctx.Data["AscVerifiedOrgs"] = map[int64]string{}
	if ids := orgIDs(users); len(ids) > 0 {
		if doms, err := user_model.VerifiedAscOrgDomains(ctx, ids); err == nil {
			ctx.Data["AscVerifiedOrgs"] = doms
		}
	}
	if tplName == tplExploreUsers {
		loadExploreActivity(ctx, users)
	}
	if opts.Load2FAStatus {
		ctx.Data["UsersTwoFaStatus"] = user_model.UserList(users).GetTwoFaStatus(ctx)
	}
	ctx.Data["ShowUserEmail"] = setting.UI.ShowUserEmail
	ctx.Data["IsRepoIndexerEnabled"] = setting.Indexer.RepoIndexerEnabled

	pager := context.NewPagination(int(count), opts.PageSize, opts.Page, 5)
	pager.SetDefaultParams(ctx)
	for paramKey, paramVal := range opts.ExtraParamStrings {
		pager.AddParamString(paramKey, paramVal)
	}
	ctx.Data["Page"] = pager
	if load != nil {
		load(users)
	}

	ctx.HTML(http.StatusOK, tplName)
}

// searchUsers runs the account search of the directories. Tests replace it.
var searchUsers = user_model.SearchUsers

// exploreUserRowOrgs is how many organizations a row of the people directory names.
const exploreUserRowOrgs = 3

// loadExploreUserRows puts what the rows of the people directory show beside each person into the page data: the
// organizations the person is a public member of (that the viewer may see), and for a signed-in viewer whom they follow.
// A lookup that fails leaves that part out of the row; the list itself is still shown.
func loadExploreUserRows(ctx *context.Context, users []*user_model.User) {
	orgs := make(map[int64][]*organization.Organization, len(users))
	following := make(map[int64]bool, len(users))
	for _, u := range users {
		list, err := db.Find[organization.Organization](ctx, organization.FindOrgOptions{
			ListOptions:    db.ListOptions{PageSize: exploreUserRowOrgs},
			UserID:         u.ID,
			IncludeLimited: ctx.IsSigned,
		})
		if err != nil {
			log.Error("explore: organizations of %s: %v", u.Name, err)
		} else if len(list) > 0 {
			orgs[u.ID] = list
		}
		if ctx.Doer != nil && ctx.Doer.ID != u.ID {
			following[u.ID] = user_model.IsFollowing(ctx, ctx.Doer.ID, u.ID)
		}
	}
	ctx.Data["ExploreUserOrgs"] = orgs
	if ctx.Doer != nil {
		ctx.Data["ExploreFollowing"] = following // may be empty: the template offers Follow only when the map was made
	}
	loadExploreRoles(ctx, users)
}

// ExploreActivity is what a row of the people or organization directory shows of the public activity of the account.
type ExploreActivity struct {
	// Points draws the line of the actions per week (asctrust.Sparkline); empty when the window has no action.
	Points string
	activities_model.AscWeekly
}

// loadExploreActivity puts the public activity of the last weeks of each account into the page data. A person who keeps
// their activity private shows none, unless the viewer may see it (themself, or a site admin). A lookup that fails
// leaves the activity out of the rows.
func loadExploreActivity(ctx *context.Context, users []*user_model.User) {
	var people, orgs []int64
	for _, u := range users {
		switch {
		case u.IsOrganization():
			orgs = append(orgs, u.ID)
		case activities_model.ActivityReadable(u, ctx.Doer):
			people = append(people, u.ID)
		}
	}
	now := time.Now()
	out := make(map[int64]*ExploreActivity, len(users))
	for _, part := range []struct {
		ids  []int64
		load func(gocontext.Context, []int64, time.Time) (map[int64]*activities_model.AscWeekly, error)
	}{{people, activities_model.AscUserActivity}, {orgs, activities_model.AscOrgActivity}} {
		got, err := part.load(ctx, part.ids, now)
		if err != nil {
			log.Error("explore: the activity of the directory (request %s): %v", ctx.RequestIDForLog(), err)
			return
		}
		for id, w := range got {
			out[id] = &ExploreActivity{Points: asctrust.Sparkline(w.Weeks, 80, 28), AscWeekly: *w}
		}
	}
	ctx.Data["ExploreActivity"] = out
	ctx.Data["ExploreActivityWeeks"] = activities_model.AscActivityWeeks
}

// loadExploreRoles puts the role of each person and the "Roles on this hub" counts into the page data. Roles tell who
// runs the hub, so only a signed-in viewer who is not restricted gets them; a visitor sees the Agent chip only. A
// lookup that fails leaves the roles out.
func loadExploreRoles(ctx *context.Context, users []*user_model.User) {
	if ctx.Doer == nil || ctx.Doer.IsRestricted {
		return
	}
	roles, err := user_model.AscRolesOf(ctx, users)
	if err != nil {
		log.Error("explore: the roles of the people directory (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	counts, err := user_model.AscRoleCounts(ctx, ctx.Doer)
	if err != nil {
		log.Error("explore: the role counts of the people directory (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	ctx.Data["ExploreRoles"] = roles
	ctx.Data["ExploreRoleCounts"] = counts
	ctx.Data["ExploreRoleOrder"] = user_model.AscRoles
}

// Users render explore users page.
func Users(ctx *context.Context) {
	if setting.Service.Explore.DisableUsersPage {
		ctx.Redirect(setting.AppSubURL + "/explore")
		return
	}
	ctx.Data["OrganizationsPageIsDisabled"] = setting.Service.Explore.DisableOrganizationsPage
	ctx.Data["CodePageIsDisabled"] = setting.Service.Explore.DisableCodePage
	ctx.Data["Title"] = ctx.Tr("explore")
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreUsers"] = true
	ctx.Data["IsRepoIndexerEnabled"] = setting.Indexer.RepoIndexerEnabled

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

	ctx.Data["InviteHref"] = inviteHref(ctx)
	opts := &user_model.SearchUserOptions{
		Actor:       ctx.Doer,
		Type:        user_model.UserTypeIndividual,
		ListOptions: db.ListOptions{PageSize: setting.UI.ExplorePagingNum},
		IsActive:    optional.Some(true),
		Visible:     []structs.VisibleType{structs.VisibleTypePublic, structs.VisibleTypeLimited, structs.VisibleTypePrivate},

		SupportedSortOrders: supportedSortOrders,
		ExtraParamStrings:   map[string]string{},
	}
	if !readPeopleFilters(ctx, opts) {
		return
	}
	RenderUserSearchWith(ctx, opts, tplExploreUsers, func(users []*user_model.User) { loadExploreUserRows(ctx, users) })
}

// exploreOrgChoices is how many organizations the Organization filter of the people directory suggests.
const exploreOrgChoices = 100

// readPeopleFilters reads the Show and Organization filters of the people directory (board ExploreUsers) into opts
// and the page data. Show is all (people and agents, the default), people or agents. Organization is the name of an
// organization the viewer may see; it keeps its public members, or all of them for a member of it or an admin. An
// organization that does not exist or that the viewer may not see finds no one. It returns false when the page was
// already answered (an error).
func readPeopleFilters(ctx *context.Context, opts *user_model.SearchUserOptions) bool {
	show := ctx.FormTrim("show")
	switch show {
	case "people":
	case "agents":
		opts.Type = user_model.UserTypeBot
	default:
		show = "all"
		opts.Asc.WithAgents = true
	}
	ctx.Data["ExploreShow"] = show
	if show != "all" {
		opts.ExtraParamStrings["show"] = show
	}

	orgName := ctx.FormTrim("org")
	ctx.Data["ExploreOrg"] = orgName
	if orgName != "" {
		opts.ExtraParamStrings["org"] = orgName
		opts.Asc.MemberOf = -1 // finds no one unless the organization is found below
		org, err := organization.GetOrgByName(ctx, orgName)
		switch {
		case err == nil && organization.HasOrgOrUserVisible(ctx, org.AsUser(), ctx.Doer):
			opts.Asc.MemberOf = org.ID
			if ctx.Doer != nil {
				member, err := org.IsOrgMember(ctx, ctx.Doer.ID)
				if err != nil {
					ctx.ServerError("IsOrgMember", err)
					return false
				}
				opts.Asc.AllMembers = member || ctx.Doer.IsAdmin
			}
		case err != nil && !organization.IsErrOrgNotExist(err):
			ctx.ServerError("GetOrgByName", err)
			return false
		}
	}
	ctx.Data["ExploreUsersFiltered"] = show != "all" || orgName != ""

	choices, _, err := user_model.SearchUsers(ctx, &user_model.SearchUserOptions{
		Actor:       ctx.Doer,
		Type:        user_model.UserTypeOrganization,
		ListOptions: db.ListOptions{PageSize: exploreOrgChoices},
		Visible:     []structs.VisibleType{structs.VisibleTypePublic, structs.VisibleTypeLimited, structs.VisibleTypePrivate},
		OrderBy:     db.SearchOrderByAlphabetically,
	})
	if err != nil {
		log.Error("explore: the organizations of the Organization filter (request %s): %v", ctx.RequestIDForLog(), err)
	}
	ctx.Data["ExploreOrgChoices"] = choices
	return true
}

// inviteHref is where Invite people goes on the people page (board ExploreUsers): an administrator adds the
// account, and where anyone may sign up, a signed-in user sends the sign-up link by email. Empty when neither applies.
func inviteHref(ctx *context.Context) string {
	switch {
	case !ctx.IsSigned:
		return ""
	case ctx.Doer.IsAdmin:
		return setting.AppSubURL + "/admin/users/new"
	case setting.Service.DisableRegistration || setting.Service.AllowOnlyExternalRegistration:
		return ""
	}
	subject := ctx.Locale.TrString("explore.invite_subject", setting.AppName)
	body := ctx.Locale.TrString("explore.invite_body", setting.AppName, setting.AppURL+"user/sign_up")
	return "mailto:?subject=" + mailtoEscape(subject) + "&body=" + mailtoEscape(body)
}

// mailtoEscape escapes a mailto header value (RFC 6068): a space is %20, as mail clients show a + as itself.
func mailtoEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// Maps a sort query to a database search order.
//
// We cannot use `models.SearchOrderByXxx`, because there may be a JOIN in the statement, different tables may have the same name columns.
func MapSortOrder(sortOrder string) db.SearchOrderBy {
	switch sortOrder {
	case "newest":
		return "`user`.created_unix DESC"

	case "oldest":
		return "`user`.created_unix ASC"

	case "leastupdate":
		return "`user`.updated_unix ASC"

	case "reversealphabetically":
		return "`user`.name DESC"

	case "lastlogin":
		return "`user`.last_login_unix ASC"

	case "reverselastlogin":
		return "`user`.last_login_unix DESC"

	case "alphabetically":
		return "`user`.name ASC"

	case "recentupdate":
		return "`user`.updated_unix DESC"

	case "mostactive":
		return user_model.AscMostActiveOrder(time.Now())

	default:
		return ""
	}
}

// orgIDs returns the ids of the organizations among users.
func orgIDs(users []*user_model.User) []int64 {
	var ids []int64
	for _, u := range users {
		if u.IsOrganization() {
			ids = append(ids, u.ID)
		}
	}
	return ids
}
