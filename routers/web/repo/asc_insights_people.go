// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"sort"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// The People board (InsightsPeople): who landed the changes, who reviewed, who writes the code. The numbers come from
// Insights.ListContributors and Insights.GetStats, called as the viewer, so asc decides again who may see the
// repository. A person is linked to the profile only when the hub knows the account and the viewer may see it; an
// agent links to the runs of the repository. Nothing is shown for a viewer who may not read pull requests, because the
// review counts are review data.

// insPerson is one row of the table.
type insPerson struct {
	Name      string
	Agent     bool
	RunBy     string
	User      *user_model.User // nil when the account is unknown or not visible to the viewer
	Landed    int
	Reviews   int
	MedianMin int
	HasMedian bool
	Spark     string
	SharePct  int
}

// insPeoplePage is the data of the template.
type insPeoplePage struct {
	Rows        []insPerson
	ReviewLoad  []insPerson // people who gave reviews, most first
	AgentPct    int
	PeoplePct   int
	HasAgentPct bool
	AllApproved bool
	Range       string
	Truncated   bool
}

// personRows maps the answer of asc to rows. A share is a fraction of 1; the bar is a whole percent.
func personRows(in []*ascv1.Contributor) []insPerson {
	rows := make([]insPerson, 0, len(in))
	for _, c := range in {
		counts := make([]int, 0, len(c.GetWeeklyLanded()))
		for _, n := range c.GetWeeklyLanded() {
			counts = append(counts, int(n))
		}
		share := int(c.GetShare()*100 + 0.5)
		rows = append(rows, insPerson{
			Name: c.GetName(), Agent: c.GetKind() == ascv1.AuthorKind_AUTHOR_KIND_AGENT, RunBy: c.GetRunBy(),
			Landed: int(c.GetLanded()), Reviews: int(c.GetReviewsGiven()),
			MedianMin: int(c.GetMedianReviewSeconds() / 60), HasMedian: c.GetMedianReviewSeconds() > 0,
			Spark: sparkPoints(counts), SharePct: min(max(share, 0), 100),
		})
	}
	return rows
}

// reviewLoadOf lists the people who reviewed, most reviews first, ties by name. Agents never review and are left out.
func reviewLoadOf(rows []insPerson) []insPerson {
	var out []insPerson
	for _, r := range rows {
		if !r.Agent && r.Reviews > 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Reviews != out[j].Reviews {
			return out[i].Reviews > out[j].Reviews
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// prepareAscPeople adds the contributors table to the Contributors page of an asc repository.
func prepareAscPeople(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if repo.GetStorageBackend() != "asc" || !repo.CanEnablePulls() || !ctx.Repo.CanRead(unit.TypePullRequests) || ctx.Doer == nil {
		return
	}
	key, days := insRange(ctx.FormString("range"))
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return
	}
	defer release()
	cctx, cancel := gocontext.WithTimeout(callCtx, asctrust.CallTimeout)
	defer cancel()
	resp, err := ascv1.NewInsightsClient(conn).ListContributors(cctx, &ascv1.ListContributorsRequest{Repo: asctrust.RepoRef(name), Days: uint32(days)})
	if err != nil {
		log.Debug("people: asc contributors of %s: %v", repo.FullName(), err)
		return
	}
	pg := &insPeoplePage{Range: key, Truncated: resp.GetTruncated()}
	pg.Rows = personRows(resp.GetContributors())
	linkPeople(ctx, repo, pg.Rows, resp.GetContributors())
	pg.ReviewLoad = reviewLoadOf(pg.Rows)
	if st := fetchInsightsStats(ctx, repo, insWhoEveryone, days); st != nil {
		people, agents := int(st.GetLandedByPeople()), int(st.GetLandedByAgents())
		if people+agents > 0 {
			pg.HasAgentPct, pg.AgentPct = true, agents*100/(people+agents)
			pg.PeoplePct = 100 - pg.AgentPct
			pg.AllApproved = agents > 0 && int(st.GetAgentChangesReviewed()) >= agents
		}
	}
	ctx.Data["AscPeople"] = pg
}

// linkPeople attaches the hub account to a person row when the hub knows it and the viewer may see it.
func linkPeople(ctx *context.Context, repo *repo_model.Repository, rows []insPerson, in []*ascv1.Contributor) {
	for i := range rows {
		if rows[i].Agent || in[i].GetUserId() == 0 {
			continue
		}
		u, err := user_model.GetUserByID(ctx, int64(in[i].GetUserId()))
		if err != nil || !user_model.IsUserVisibleToViewer(ctx, u, ctx.Doer) {
			continue
		}
		rows[i].User = u
	}
}
