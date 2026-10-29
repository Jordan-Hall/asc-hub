// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"sort"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The Insights page of an asc repository (board Insights): what landed, who wrote it and how fast it was reviewed.
// The numbers come from the pull requests and reviews the hub holds, so they need the pull request unit; a viewer who
// may read only the code gets the git pulse of Forgejo, never the review times or the names of stacks. An agent is a
// pull request whose poster is a bot user. Nothing here is estimated: a figure with no data is left out, and a size
// the hub cannot count leaves that pull request out of the size groups only.

const (
	insWhoEveryone = "everyone"
	insWhoPeople   = "people"
	insWhoAgents   = "agents"

	// insMaxIssues bounds the pull requests read for one page.
	insMaxIssues = 2000
	// insMaxSized bounds the pull requests whose size is counted with git.
	insMaxSized = 60
)

// insPull is what the page needs of one pull request.
type insPull struct {
	ID          int64
	Created     time.Time
	Merged      time.Time // zero when not merged
	Agent       bool
	Approved    bool // a person approved it
	FirstReview time.Time
	Size        int
	HasSize     bool
	Root        bool // the bottom change of a stack, or a change that is not stacked
	Name        string
	Parent      int64
}

// insDay is one bar of the chart: a day, or a week for a year.
type insDay struct {
	Start  time.Time
	People int
	Agents int
}

// insWeek is the median wait for a first review of the pull requests opened in a week.
type insWeek struct {
	Start   time.Time
	Minutes int
	N       int
}

// insSizeGroup is the median wait for a first review by the size of the change.
type insSizeGroup struct {
	Key     string // under_50, 50_300, over_300
	Minutes int
	N       int
}

// insStack is a stack that landed lately.
type insStack struct {
	Name    string
	Changes int
	When    time.Time
}

// insBar is one bar of the chart in a 600 by 130 box: people at the bottom, agents above them.
type insBar struct {
	X, W   int
	PY, PH int
	AY, AH int
	People int
	Agents int
	Start  time.Time
}

// insChartW and insChartH are the size of the chart box; the plot leaves the top ten units free for the direct labels.
const (
	insChartW = 600
	insChartH = 130
	insPlotH  = 120
)

// insBars lays the days out as bars. The tallest bar fills the plot; a bar with a count is at least 2 units high so a
// small number can be seen.
func insBars(days []insDay, most int) []insBar {
	if len(days) == 0 {
		return nil
	}
	slot := insChartW / len(days)
	w := max(slot*7/10, 1)
	bars := make([]insBar, len(days))
	for i, d := range days {
		h := func(n int) int {
			if n == 0 || most == 0 {
				return 0
			}
			return max(n*insPlotH/most, 2)
		}
		ph, ah := h(d.People), h(d.Agents)
		b := insBar{X: i*slot + (slot-w)/2, W: w, PH: ph, AH: ah, People: d.People, Agents: d.Agents, Start: d.Start}
		b.PY = insChartH - ph
		b.AY = b.PY - ah
		bars[i] = b
	}
	return bars
}

// insPage is the data of the template.
type insPage struct {
	Who, Range              string
	Days                    int
	Landed, PrevLanded      int
	Opened, PrevOpened      int
	LandedDelta             int // percent against the period before, valid when PrevLanded > 0
	HasLandedDelta          bool
	MedianMin               int
	HasMedian               bool
	AgentPct                int
	HasAgentPct             bool
	AllAgentsApproved       bool
	Days2                   []insDay
	Bars                    []insBar
	MaxBar                  int
	TotalPeople, TotalAgent int
	TotalAll                int
	Weeks                   []insWeek
	Sizes                   []insSizeGroup
	Recent                  []insStack
	Waiting                 []*stackRow
	Trend                   string // faster, slower or ""
	Truncated               bool   // asc cut the data: the numbers are lower bounds
}

// insMedian is the median of minutes (the mean of the two middle values for an even count); ok is false for none.
func insMedian(ms []int) (int, bool) {
	if len(ms) == 0 {
		return 0, false
	}
	s := append([]int(nil), ms...)
	sort.Ints(s)
	if len(s)%2 == 1 {
		return s[len(s)/2], true
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2, true
}

// insKeep reports whether a pull request belongs to the chosen group of authors.
func insKeep(p *insPull, who string) bool {
	switch who {
	case insWhoPeople:
		return !p.Agent
	case insWhoAgents:
		return p.Agent
	}
	return true
}

// insBuckets counts the landed pull requests per bucket from start for n buckets of step each.
func insBuckets(pulls []*insPull, start time.Time, n int, step time.Duration) []insDay {
	days := make([]insDay, n)
	for i := range days {
		days[i].Start = start.Add(time.Duration(i) * step)
	}
	for _, p := range pulls {
		if p.Merged.IsZero() || p.Merged.Before(start) {
			continue
		}
		i := int(p.Merged.Sub(start) / step)
		if i < 0 || i >= n {
			continue
		}
		if p.Agent {
			days[i].Agents++
		} else {
			days[i].People++
		}
	}
	return days
}

// insWaitMinutes is the minutes from opening to the first review, or false when there is no review.
func insWaitMinutes(p *insPull) (int, bool) {
	if p.FirstReview.IsZero() || p.FirstReview.Before(p.Created) {
		return 0, false
	}
	return int(p.FirstReview.Sub(p.Created) / time.Minute), true
}

// insWeekly gives the median wait for a first review of the pull requests opened in each of the last n weeks, oldest first.
func insWeekly(pulls []*insPull, now time.Time, n int) []insWeek {
	const week = 7 * 24 * time.Hour
	start := now.Add(-time.Duration(n) * week)
	buckets := make([][]int, n)
	for _, p := range pulls {
		if p.Created.Before(start) {
			continue
		}
		i := int(p.Created.Sub(start) / week)
		if i < 0 || i >= n {
			continue
		}
		if m, ok := insWaitMinutes(p); ok {
			buckets[i] = append(buckets[i], m)
		}
	}
	weeks := make([]insWeek, 0, n)
	for i, b := range buckets {
		m, ok := insMedian(b)
		if !ok {
			continue
		}
		weeks = append(weeks, insWeek{Start: start.Add(time.Duration(i) * week), Minutes: m, N: len(b)})
	}
	return weeks
}

// insSizes gives the median wait for a first review by the size of the change: under 50 lines, 50 to 300, over 300.
func insSizes(pulls []*insPull) []insSizeGroup {
	groups := []struct {
		key  string
		from int
		to   int // exclusive; 0 is open ended
		ms   []int
	}{{key: "under_50", from: 0, to: 50}, {key: "50_300", from: 50, to: 301}, {key: "over_300", from: 301}}
	for _, p := range pulls {
		if !p.HasSize {
			continue
		}
		m, ok := insWaitMinutes(p)
		if !ok {
			continue
		}
		for i := range groups {
			g := &groups[i]
			if p.Size >= g.from && (g.to == 0 || p.Size < g.to) {
				g.ms = append(g.ms, m)
				break
			}
		}
	}
	var out []insSizeGroup
	for _, g := range groups {
		if med, ok := insMedian(g.ms); ok {
			out = append(out, insSizeGroup{Key: g.key, Minutes: med, N: len(g.ms)})
		}
	}
	return out
}

// insRecentStacks groups pull requests merged since from by the bottom change of their stack, newest first.
func insRecentStacks(pulls []*insPull, from time.Time, limit int) []insStack {
	byID := map[int64]*insPull{}
	for _, p := range pulls {
		byID[p.ID] = p
	}
	rootOf := func(p *insPull) *insPull {
		for range 64 {
			par, ok := byID[p.Parent]
			if !ok || p.Parent == 0 {
				return p
			}
			p = par
		}
		return p
	}
	groups := map[int64]*insStack{}
	for _, p := range pulls {
		if p.Merged.IsZero() || p.Merged.Before(from) {
			continue
		}
		r := rootOf(p)
		g := groups[r.ID]
		if g == nil {
			g = &insStack{Name: r.Name}
			groups[r.ID] = g
		}
		g.Changes++
		if p.Merged.After(g.When) {
			g.When = p.Merged
		}
	}
	out := make([]insStack, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].When.Equal(out[j].When) {
			return out[i].When.After(out[j].When)
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// insPercentChange is the change from prev to cur in percent, rounded; false when there is no earlier figure to compare.
func insPercentChange(cur, prev int) (int, bool) {
	if prev <= 0 {
		return 0, false
	}
	d := float64(cur-prev) * 100 / float64(prev)
	if d < 0 {
		return -int(-d + 0.5), true
	}
	return int(d + 0.5), true
}

// insRange maps the query to a number of days.
func insRange(s string) (key string, days int) {
	switch s {
	case "7d":
		return "7d", 7
	case "year":
		return "year", 365
	}
	return "30d", 30
}

// insLoad reads the pull requests that were opened or closed since from, with what the page needs of each.
func insLoad(ctx *context.Context, repo *repo_model.Repository, from time.Time) ([]*insPull, error) {
	var issues []*issues_model.Issue
	err := db.GetEngine(ctx).Where("repo_id = ? AND is_pull = ?", repo.ID, true).
		And("created_unix >= ? OR (is_closed = ? AND closed_unix >= ?)", from.Unix(), true, from.Unix()).
		OrderBy("id DESC").Limit(insMaxIssues).Find(&issues)
	if err != nil || len(issues) == 0 {
		return nil, err
	}
	ids := make([]int64, 0, len(issues))
	for _, is := range issues {
		ids = append(ids, is.ID)
	}
	var prs []*issues_model.PullRequest
	if err := db.GetEngine(ctx).In("issue_id", ids).Find(&prs); err != nil {
		return nil, err
	}
	byIssue := map[int64]*issues_model.PullRequest{}
	var pullIDs []int64
	for _, pr := range prs {
		byIssue[pr.IssueID] = pr
		pullIDs = append(pullIDs, pr.ID)
	}
	links := map[int64]*issues_model.PullRequestAscStack{}
	var rows []*issues_model.PullRequestAscStack
	if err := db.GetEngine(ctx).Where("repo_id = ?", repo.ID).In("pull_id", pullIDs).Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		links[r.PullID] = r
	}
	var reviews []*issues_model.Review
	if err := db.GetEngine(ctx).In("issue_id", ids).
		In("type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeComment, issues_model.ReviewTypeReject).Find(&reviews); err != nil {
		return nil, err
	}
	userIDs := map[int64]bool{}
	for _, is := range issues {
		userIDs[is.PosterID] = true
	}
	for _, r := range reviews {
		userIDs[r.ReviewerID] = true
	}
	idList := make([]int64, 0, len(userIDs))
	for id := range userIDs {
		idList = append(idList, id)
	}
	userList, err := user_model.GetUsersByIDs(ctx, idList)
	if err != nil {
		return nil, err
	}
	users := map[int64]*user_model.User{}
	for _, u := range userList {
		users[u.ID] = u
	}
	isBot := func(id int64) bool { u := users[id]; return u != nil && u.IsBot() }

	byIssueReviews := map[int64][]*issues_model.Review{}
	for _, r := range reviews {
		byIssueReviews[r.IssueID] = append(byIssueReviews[r.IssueID], r)
	}
	var out []*insPull
	for _, is := range issues {
		pr := byIssue[is.ID]
		if pr == nil {
			continue
		}
		p := &insPull{ID: pr.ID, Created: is.CreatedUnix.AsTime(), Name: pr.HeadBranch, Agent: isBot(is.PosterID), Root: true}
		if pr.HasMerged {
			p.Merged = pr.MergedUnix.AsTime()
		}
		if l := links[pr.ID]; l != nil {
			p.Parent = l.StackParentID
			p.Root = l.StackParentID == 0
		}
		for _, r := range byIssueReviews[is.ID] {
			if r.ReviewerID == is.PosterID {
				continue
			}
			t := r.CreatedUnix.AsTime()
			if p.FirstReview.IsZero() || t.Before(p.FirstReview) {
				p.FirstReview = t
			}
			if r.Type == issues_model.ReviewTypeApprove && !r.Dismissed && !isBot(r.ReviewerID) {
				p.Approved = true
			}
		}
		if ctx.Repo.GitRepo != nil && pr.HasMerged && pr.MergeBase != "" && pr.MergedCommitID != "" && len(out) < insMaxSized {
			if _, add, del, err := ctx.Repo.GitRepo.GetShortStat(pr.MergeBase, pr.MergedCommitID, false); err == nil {
				p.Size, p.HasSize = add+del, true
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// prepareAscInsights adds the Insights data to the activity page of an asc repository.
func prepareAscInsights(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if repo.GetStorageBackend() != "asc" || !repo.CanEnablePulls() || !ctx.Repo.CanRead(unit.TypePullRequests) {
		return
	}
	who := ctx.FormString("who")
	if who != insWhoPeople && who != insWhoAgents {
		who = insWhoEveryone
	}
	key, days := insRange(ctx.FormString("range"))
	now := time.Now()
	from := now.AddDate(0, 0, -2*days) // the period and the one before it
	pulls, err := insLoad(ctx, repo, from)
	if err != nil {
		log.Error("insights: pulls of %s: %v", repo.FullName(), err)
		ctx.Data["AscInsightsFailed"] = true
		ctx.Data["AscInsightsRequestID"] = requestID(ctx.Req)
		return
	}
	var all, mine []*insPull
	for _, p := range pulls {
		all = append(all, p)
		if insKeep(p, who) {
			mine = append(mine, p)
		}
	}
	pageStart := now.AddDate(0, 0, -days)
	pg := &insPage{Who: who, Range: key, Days: days}
	var waits []int
	var landedAgents, landedTotal int
	approvedAll := true
	for _, p := range mine {
		if !p.Merged.IsZero() && !p.Merged.Before(pageStart) {
			pg.Landed++
		} else if !p.Merged.IsZero() && !p.Merged.Before(from) {
			pg.PrevLanded++
		}
		if p.Root && !p.Created.Before(pageStart) {
			pg.Opened++
		} else if p.Root && !p.Created.Before(from) {
			pg.PrevOpened++
		}
		if !p.Created.Before(pageStart) {
			if m, ok := insWaitMinutes(p); ok {
				waits = append(waits, m)
			}
		}
	}
	for _, p := range all {
		if p.Merged.IsZero() || p.Merged.Before(pageStart) {
			continue
		}
		landedTotal++
		if p.Agent {
			landedAgents++
			approvedAll = approvedAll && p.Approved
		}
	}
	pg.LandedDelta, pg.HasLandedDelta = insPercentChange(pg.Landed, pg.PrevLanded)
	pg.MedianMin, pg.HasMedian = insMedian(waits)
	if landedTotal > 0 {
		pg.AgentPct, pg.HasAgentPct = landedAgents*100/landedTotal, true
		pg.AllAgentsApproved = landedAgents > 0 && approvedAll
	}
	step, n := 24*time.Hour, days
	if key == "year" {
		step, n = 7*24*time.Hour, 52
	}
	start := now.Add(-time.Duration(n) * step)
	pg.Days2 = insBuckets(mine, start, n, step)
	for _, d := range pg.Days2 {
		pg.TotalPeople += d.People
		pg.TotalAgent += d.Agents
		pg.MaxBar = max(pg.MaxBar, d.People+d.Agents)
	}
	pg.TotalAll = pg.TotalPeople + pg.TotalAgent
	pg.Bars = insBars(pg.Days2, pg.MaxBar)
	pg.Weeks = insWeekly(mine, now, 8)
	if len(pg.Weeks) >= 2 {
		first, last := pg.Weeks[0].Minutes, pg.Weeks[len(pg.Weeks)-1].Minutes
		switch {
		case last < first:
			pg.Trend = "faster"
		case last > first:
			pg.Trend = "slower"
		}
	}
	pg.Sizes = insSizes(mine)
	if r := fetchInsightsStats(ctx, repo, who, days); r != nil {
		applyInsightsStats(pg, r)
	}
	pg.Recent = insRecentStacks(mine, now.AddDate(0, 0, -14), 5)

	if changes, landed, err := loadStackChanges(ctx, repo, ctx.Repo.GitRepo, ctx.Doer); err == nil {
		rows := buildStackRows(changes, landed, repo.Link(), ctx.Doer)
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Updated < rows[j].Updated })
		for _, r := range rows {
			if (who == insWhoAgents && !r.Agent) || (who == insWhoPeople && r.Agent) {
				continue
			}
			pg.Waiting = append(pg.Waiting, r)
			if len(pg.Waiting) == 5 {
				break
			}
		}
	}
	ctx.Data["AscInsights"] = pg
	ctx.Data["AscInsightsLink"] = fmt.Sprintf("%s/activity", repo.Link())
}
