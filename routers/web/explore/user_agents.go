// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	"net/url"
	"strings"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascagentdir"
	"forgejo.org/services/ascpresence"
	"forgejo.org/services/context"
)

const (
	// exploreAgentRowsAll is how many agent rows the people directory shows when it shows people and agents.
	exploreAgentRowsAll = 5
	// exploreActiveNow is how many people and how many agents the "Who is active now" card names.
	exploreActiveNow = 5
	// activeNowActionWithin is how recent the last public action of a person must be to be named as what they do now.
	activeNowActionWithin = time.Hour
)

// exploreUserShows are the chips of the people directory, in order.
var exploreUserShows = []string{"all", "people", "agents", "maintainers", "online"}

// rolesVisible reports whether the viewer gets the roles of the people directory (and so the Maintainers chip): roles
// tell who runs the hub, so a signed-in viewer who is not restricted.
func rolesVisible(ctx *context.Context) bool {
	return ctx.Doer != nil && !ctx.Doer.IsRestricted
}

// peopleChips are the chips of the people directory: links that keep the search, the order and the organization.
func peopleChips(ctx *context.Context, show string) []ExploreOrgChip {
	chips := make([]ExploreOrgChip, 0, len(exploreUserShows))
	for _, key := range exploreUserShows {
		if key == "maintainers" && !rolesVisible(ctx) {
			continue
		}
		q := url.Values{}
		if key != "all" {
			q.Set("show", key)
		}
		for _, name := range []string{"q", "sort", "org"} {
			if v := ctx.FormTrim(name); v != "" {
				q.Set(name, v)
			}
		}
		href := "?"
		if len(q) > 0 {
			href += q.Encode()
		}
		chips = append(chips, ExploreOrgChip{Key: key, Href: href, Current: key == show})
	}
	return chips
}

// ActivePerson is a person of the "Who is active now" card: online, and the repository of their last public action
// when it is recent.
type ActivePerson struct {
	User *user_model.User
	Repo *repo_model.Repository
	When timeutil.TimeStamp
}

// ExploreActiveNow is the "Who is active now" card of the people directory.
type ExploreActiveNow struct {
	People      []*ActivePerson
	PeopleTotal int64
	Agents      []*ascagentdir.Agent
}

// loadExploreAgentsAndActive puts the agent rows (ExploreAgents) and the "Who is active now" card (ExploreActiveNow)
// into the page data of the people directory. The agents are those that ran in the asc repositories the viewer can
// read (services/ascagentdir), for a viewer with agent features on; the rows are on the first page of the
// directory when it shows agents, at most exploreAgentRowsAll when it also shows people, and keep the search and
// the organization. A lookup that fails leaves its part out.
func loadExploreAgentsAndActive(ctx *context.Context, opts *user_model.SearchUserOptions) {
	agents := exploreAgents(ctx, opts.Asc.MemberOf)
	show, _ := ctx.Data["ExploreShow"].(string)
	if (show == "all" || show == "agents") && opts.Page <= 1 {
		rows := matchAgents(agents, opts.Keyword)
		total := len(rows)
		if show == "all" && len(rows) > exploreAgentRowsAll {
			rows = rows[:exploreAgentRowsAll]
			q := url.Values{"show": {"agents"}}
			for _, name := range []string{"q", "sort", "org"} {
				if v := ctx.FormTrim(name); v != "" {
					q.Set(name, v)
				}
			}
			ctx.Data["ExploreAgentsAllHref"] = "?" + q.Encode()
		}
		if len(rows) > 0 {
			ctx.Data["ExploreAgents"] = rows
			ctx.Data["ExploreAgentsTotal"] = total
		}
	}
	ctx.Data["ExploreActiveNow"] = activeNow(ctx, agents)
}

// exploreAgents is the agent directory of the viewer, of one organization when memberOf is set (none when it is
// negative: an organization the viewer may not see).
func exploreAgents(ctx *context.Context, memberOf int64) []*ascagentdir.Agent {
	if memberOf < 0 || !ascagentdir.Enabled(ctx, ctx.Doer) {
		return nil
	}
	repos, err := ascagentdir.Repos(ctx, ctx.Doer, memberOf, false)
	if err != nil {
		log.Error("explore: the asc repositories of the agent directory: %v", err)
		return nil
	}
	agents, err := ascagentdir.Summarize(ctx, repos, "")
	if err != nil {
		log.Warn("explore: the agent directory (request %s): %v", ctx.RequestIDForLog(), err)
		return nil
	}
	return agents
}

// matchAgents keeps the agents whose name or owner contains the keyword, ignoring case; "@owner/name" matches too.
func matchAgents(agents []*ascagentdir.Agent, keyword string) []*ascagentdir.Agent {
	k := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(keyword), "@"))
	if k == "" {
		return agents
	}
	var out []*ascagentdir.Agent
	for _, a := range agents {
		if strings.Contains(strings.ToLower(a.Owner.Name+"/"+a.Name), k) {
			out = append(out, a)
		}
	}
	return out
}

// activeNow is the "Who is active now" card: the people the viewer sees as online with what they last did in public,
// and the agents with runs going.
func activeNow(ctx *context.Context, agents []*ascagentdir.Agent) *ExploreActiveNow {
	out := &ExploreActiveNow{}
	for _, a := range agents {
		if a.Active > 0 && len(out.Agents) < exploreActiveNow {
			out.Agents = append(out.Agents, a)
		}
	}
	users, total, err := user_model.SearchUsers(ctx, &user_model.SearchUserOptions{
		Actor:       ctx.Doer,
		Type:        user_model.UserTypeIndividual,
		ListOptions: db.ListOptions{Page: 1, PageSize: exploreActiveNow},
		IsActive:    optional.Some(true),
		Visible:     []structs.VisibleType{structs.VisibleTypePublic, structs.VisibleTypeLimited, structs.VisibleTypePrivate},
		OrderBy:     db.SearchOrderByAlphabetically,
		Asc:         user_model.AscUserFilter{Only: ascpresence.OnlineCond(ctx.Doer)},
	})
	if err != nil {
		log.Error("explore: who is online (request %s): %v", ctx.RequestIDForLog(), err)
		return out
	}
	out.PeopleTotal = total
	since := timeutil.TimeStamp(time.Now().Add(-activeNowActionWithin).Unix())
	for _, u := range users {
		p := &ActivePerson{User: u}
		feed, _, err := activities_model.GetFeeds(ctx, activities_model.GetFeedsOptions{
			ListOptions:     db.ListOptions{Page: 1, PageSize: 1},
			RequestedUser:   u,
			Actor:           ctx.Doer,
			OnlyPerformedBy: true,
		})
		if err != nil {
			log.Error("explore: the last action of %s: %v", u.Name, err)
		} else if len(feed) > 0 && feed[0].CreatedUnix >= since && feed[0].Repo != nil {
			p.Repo, p.When = feed[0].Repo, feed[0].CreatedUnix
		}
		out.People = append(out.People, p)
	}
	return out
}
