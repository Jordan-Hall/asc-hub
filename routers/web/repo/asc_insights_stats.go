// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"time"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// When asc serves Insights.GetStats the numbers of the Insights page come from it: asc counts landed changes by who
// wrote them (a person or an agent run), not by the poster of a pull request. The call is made as the viewer with the
// repository read credential, so asc decides again who may see the repository. An asc that does not serve the call, or
// a failed call, leaves the numbers the hub computed from its own pull requests and reviews.

// insKind maps the chosen group of authors to the wire value.
func insKind(who string) ascv1.AuthorKind {
	switch who {
	case insWhoPeople:
		return ascv1.AuthorKind_AUTHOR_KIND_PERSON
	case insWhoAgents:
		return ascv1.AuthorKind_AUTHOR_KIND_AGENT
	}
	return ascv1.AuthorKind_AUTHOR_KIND_UNSPECIFIED
}

// applyInsightsStats puts the answer of asc into the page. Bars are per day, or per week for a year. Medians arrive
// in seconds and are shown in minutes. A weekly or size row without a median is left out.
func applyInsightsStats(pg *insPage, r *ascv1.InsightsStatsResponse) {
	pg.Landed, pg.PrevLanded = int(r.GetLanded()), int(r.GetLandedPrevious())
	pg.Opened, pg.PrevOpened = int(r.GetStacksOpened()), int(r.GetStacksOpenedPrevious())
	pg.LandedDelta, pg.HasLandedDelta = insPercentChange(pg.Landed, pg.PrevLanded)
	pg.MedianMin, pg.HasMedian = int(r.GetMedianReviewSeconds()/60), r.GetMedianReviewSeconds() > 0

	people, agents := int(r.GetLandedByPeople()), int(r.GetLandedByAgents())
	pg.HasAgentPct = people+agents > 0
	pg.AgentPct = 0
	if pg.HasAgentPct {
		pg.AgentPct = agents * 100 / (people + agents)
	}
	pg.AllAgentsApproved = agents > 0 && int(r.GetAgentChangesReviewed()) >= agents

	days := make([]insDay, 0, len(r.GetPerDay()))
	for _, d := range r.GetPerDay() {
		days = append(days, insDay{Start: time.Unix(d.GetDay(), 0).UTC(), People: int(d.GetPeople()), Agents: int(d.GetAgents())})
	}
	if pg.Days > 31 {
		days = insWeeksOf(days)
	}
	pg.Days2 = days
	pg.TotalPeople, pg.TotalAgent, pg.MaxBar = 0, 0, 0
	for _, d := range days {
		pg.TotalPeople += d.People
		pg.TotalAgent += d.Agents
		pg.MaxBar = max(pg.MaxBar, d.People+d.Agents)
	}
	pg.TotalAll = pg.TotalPeople + pg.TotalAgent
	pg.Bars = insBars(days, pg.MaxBar)

	pg.Weeks = pg.Weeks[:0]
	for _, w := range r.GetWeekly() {
		if w.GetMedianReviewSeconds() > 0 {
			pg.Weeks = append(pg.Weeks, insWeek{Start: time.Unix(w.GetWeekStart(), 0).UTC(), Minutes: int(w.GetMedianReviewSeconds() / 60), N: int(w.GetLanded())})
		}
	}
	pg.Trend = ""
	if len(pg.Weeks) >= 2 {
		switch first, last := pg.Weeks[0].Minutes, pg.Weeks[len(pg.Weeks)-1].Minutes; {
		case last < first:
			pg.Trend = "faster"
		case last > first:
			pg.Trend = "slower"
		}
	}
	pg.Sizes = pg.Sizes[:0]
	for _, b := range r.GetBySize() {
		key := map[string]string{"under_50": "under_50", "50_to_300": "50_300", "over_300": "over_300"}[b.GetLabel()]
		if key != "" && b.GetMedianReviewSeconds() > 0 {
			pg.Sizes = append(pg.Sizes, insSizeGroup{Key: key, Minutes: int(b.GetMedianReviewSeconds() / 60), N: int(b.GetChanges())})
		}
	}
	pg.Truncated = r.GetTruncated()
}

// insWeeksOf folds days into runs of seven, oldest first; the last run may be shorter.
func insWeeksOf(days []insDay) []insDay {
	var out []insDay
	for i := 0; i < len(days); i += 7 {
		w := insDay{Start: days[i].Start}
		for _, d := range days[i:min(i+7, len(days))] {
			w.People += d.People
			w.Agents += d.Agents
		}
		out = append(out, w)
	}
	return out
}

// fetchInsightsStats asks asc for the statistics as the viewer. It returns nil when asc is off, the viewer is a guest,
// the repository is not stored in asc, or the call fails.
func fetchInsightsStats(ctx *context.Context, repo *repo_model.Repository, who string, days int) *ascv1.InsightsStatsResponse {
	if ctx.Doer == nil {
		return nil
	}
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return nil
	}
	defer release()
	cctx, cancel := gocontext.WithTimeout(callCtx, asctrust.CallTimeout)
	defer cancel()
	resp, err := ascv1.NewInsightsClient(conn).GetStats(cctx, &ascv1.InsightsStatsRequest{
		Repo: asctrust.RepoRef(name), Days: uint32(days), Kind: insKind(who),
	})
	if err != nil {
		log.Debug("insights: asc statistics of %s: %v", repo.FullName(), err)
		return nil
	}
	return resp
}
