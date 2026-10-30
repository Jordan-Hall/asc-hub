// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"encoding/json"
	"fmt"

	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	hubcache "forgejo.org/modules/cache"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The "Who writes the code" and "Contributors" cards of the repository home (board Repo) from Insights.GetStats and
// Insights.ListContributors: the author mix of the changes that landed in the last 30 days, how many agent changes a
// person approved, the median time to a first review, and who landed the most in the last 12 weeks, an agent marked as
// an agent. The answers are expensive for asc, so the hub keeps them for a minute; they are about one repository and
// the same for everyone who may read it, and the home page already needs code read.

const (
	homeInsightsDays      = 30
	homeContributorDays   = 12 * 7
	homeInsightsCacheSecs = 60
)

// HomeAuthors is the "Who writes the code" card.
type HomeAuthors struct {
	Days          int
	Landed        int
	People        int
	Agents        int
	PeoplePct     int
	AgentsPct     int
	NoReview      bool // the viewer may not read pull requests: the review numbers are left out
	ReviewedPct   int  // of the landed agent changes, those a person approved; meaningful when Agents > 0
	MedianMinutes int64
	Truncated     bool
}

// homeAuthorsOf turns the answer of GetStats into the card, or nil when nothing landed in the window.
func homeAuthorsOf(r *ascv1.InsightsStatsResponse, days int) *HomeAuthors {
	if r == nil {
		return nil
	}
	people, agents := int(r.GetLandedByPeople()), int(r.GetLandedByAgents())
	if people+agents == 0 {
		return nil
	}
	a := &HomeAuthors{Days: days, Landed: int(r.GetLanded()), People: people, Agents: agents, Truncated: r.GetTruncated()}
	a.PeoplePct = (people*100 + (people+agents)/2) / (people + agents)
	a.AgentsPct = 100 - a.PeoplePct
	if agents > 0 {
		reviewed := min(int(r.GetAgentChangesReviewed()), agents)
		a.ReviewedPct = (reviewed*100 + agents/2) / agents
	}
	if s := r.GetMedianReviewSeconds(); s > 0 {
		a.MedianMinutes = (s + 59) / 60
	}
	return a
}

// homeContributorsOf turns the answer of ListContributors into the rows of the card: the people and agents who landed
// something, most first, at most limit; an entry that landed nothing (a reviewer only) is left out.
func homeContributorsOf(r *ascv1.ListContributorsResponse, limit int) []HomeContributor {
	var out []HomeContributor
	for _, c := range r.GetContributors() {
		if c.GetLanded() == 0 || c.GetName() == "" {
			continue
		}
		out = append(out, HomeContributor{Name: c.GetName(), Count: int(c.GetLanded()), Agent: c.GetKind() == ascv1.AuthorKind_AUTHOR_KIND_AGENT})
		if len(out) == limit {
			break
		}
	}
	if len(out) > 0 {
		most := out[0].Count
		for i := range out {
			out[i].Pct = out[i].Count * 100 / most
		}
	}
	return out
}

// homeInsights is what the hub keeps of the two answers.
type homeInsights struct {
	Authors      *HomeAuthors
	Contributors []HomeContributor
}

// prepareHomeInsights fills HomeAuthors and, when asc knows who landed what, HomeContributors (replacing the list that
// comes from git). It leaves everything as it is when asc is off, the repository is not stored in asc or asc answers
// with an error.
func prepareHomeInsights(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled {
		return
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return
	}
	key := fmt.Sprintf("asc:home-insights:%d", repo.ID)
	if c := hubcache.GetCache(); c != nil {
		if raw, ok := c.Get(key).(string); ok {
			var hi homeInsights
			if json.Unmarshal([]byte(raw), &hi) == nil {
				applyHomeInsights(ctx, &hi)
				return
			}
		}
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	callCtx := asctrust.System(ctx)
	release := func() {}
	if ctx.IsSigned && ctx.Doer != nil {
		if c, rel, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo); err == nil {
			callCtx, release = c, rel
		}
	}
	defer release()
	client := ascv1.NewInsightsClient(conn)
	ref := &ascv1.RepoRef{Name: name}
	hi := &homeInsights{}
	if st, err := client.GetStats(callCtx, &ascv1.InsightsStatsRequest{Repo: ref, Days: homeInsightsDays}); err != nil {
		log.Warn("home: insights stats of %s: %v", repo.FullName(), err)
	} else {
		hi.Authors = homeAuthorsOf(st, homeInsightsDays)
	}
	if cs, err := client.ListContributors(callCtx, &ascv1.ListContributorsRequest{Repo: ref, Days: homeContributorDays}); err != nil {
		log.Warn("home: insights contributors of %s: %v", repo.FullName(), err)
	} else {
		hi.Contributors = homeContributorsOf(cs, homeContributorLimit)
	}
	if c := hubcache.GetCache(); c != nil {
		if raw, err := json.Marshal(hi); err == nil {
			_ = c.Put(key, string(raw), homeInsightsCacheSecs)
		}
	}
	applyHomeInsights(ctx, hi)
}

// homeAuthorsFor is the card as one viewer may see it: who approved and how long a review took are review data, so
// a viewer who may not read pull requests gets the card without them. The cached card is not changed.
func homeAuthorsFor(a *HomeAuthors, canReadPulls bool) *HomeAuthors {
	c := *a
	if !canReadPulls {
		c.NoReview, c.ReviewedPct, c.MedianMinutes = true, 0, 0
	}
	return &c
}

func applyHomeInsights(ctx *context.Context, hi *homeInsights) {
	if hi.Authors != nil {
		ctx.Data["HomeAuthors"] = homeAuthorsFor(hi.Authors, ctx.Repo.CanRead(unit.TypePullRequests))
	}
	if len(hi.Contributors) > 0 {
		rows := make([]HomeContributor, len(hi.Contributors))
		copy(rows, hi.Contributors)
		for i := range rows {
			if !rows[i].Agent {
				if u, err := user_model.GetUserByName(ctx, rows[i].Name); err == nil {
					rows[i].User = u
				}
			}
		}
		ctx.Data["HomeContributors"] = rows
	}
}
