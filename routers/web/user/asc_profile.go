// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"cmp"
	gocontext "context"
	"net/url"
	"slices"
	"strings"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"
)

// The profile page after the design board "Profile": a header band, tabs, what the person is working on now,
// the contributions of the last year as a grid of squares, recent activity and the repositories the person
// works in. Everything is read for the viewer: a pull request or a repository is only listed when the viewer
// may read its pull requests, a private repository never shows its name to someone who cannot read it, and
// the contribution counts come from activity that the activity query already limits to what the viewer can
// see (activities_model.GetUserHeatmapDataByUser).

const (
	// ascHeatWeeks is the number of columns of the grid: 53 weeks cover a year of 365 days plus the days of
	// the week the year starts in.
	ascHeatWeeks = 53
	// ascHeatLevels is the number of colours above "no activity".
	ascHeatLevels = 4

	ascProfileWorkMax  = 5
	ascProfileReposMax = 5
	// ascProfileScan bounds the pull requests looked at to find ascProfileWorkMax readable ones.
	ascProfileScan = 40
	// ascProfileActivityMax is how many activities the overview shows.
	ascProfileActivityMax = 8
	// ascProfileFocusMax is how many Focus chips the overview shows, drawn from the topics and the main language
	// of at most ascProfileFocusRepos readable repositories the person changes most.
	ascProfileFocusMax   = 6
	ascProfileFocusRepos = 12
)

// AscHeatDay is one square of the grid.
type AscHeatDay struct {
	Level int    // 0 for no activity, else 1 to ascHeatLevels
	Label string // "3 contributions on Mon, 12 Oct 2026"
	Blank bool   // a day outside the grid's range: nothing is drawn
	Agent bool   // most of the day's changes came from agent runs the person started: a violet square
	Link  string // the activity of that day, when the grid links its days
}

// AscHeatMonth is a month label above the grid, at the first column that starts in that month.
type AscHeatMonth struct {
	Name string
	Col  int // 1 based grid column
}

// AscHeatRow is a day with activity, for the table that is the alternative to the grid.
type AscHeatRow struct {
	Date   string // "Mon, 12 Oct 2026"
	Link   string
	Count  int64
	Agents int64
}

// AscHeatYear is an entry of the year switch; Year 0 is the last twelve months.
type AscHeatYear struct {
	Year    int
	Link    string
	Current bool
}

// AscHeatSummary is the line under the grid: changes, agent changes, reviews and the median review turnaround.
type AscHeatSummary struct {
	Changes      int64
	AgentChanges int64
	Reviews      int64
	Turnaround   string // empty when the person gave no review in the range
}

// AscHeat is the contribution grid: Weeks[column][row], row 0 is Sunday.
type AscHeat struct {
	Weeks   [][]AscHeatDay
	Months  []AscHeatMonth
	Total   int64
	Days    int   // days that have activity
	Agents  int64 // of Total, the changes of agent runs the person started
	Rows    []AscHeatRow
	Year    int // 0 for the last twelve months
	Years   []AscHeatYear
	Summary *AscHeatSummary
}

// ascHeatLevel maps a day's count to a colour step relative to the busiest day, so a quiet profile still
// shows its days: the busiest day is level 4 and any activity is at least level 1.
func ascHeatLevel(count, max int64) int {
	if count <= 0 || max <= 0 {
		return 0
	}
	lvl := int((count*ascHeatLevels + max - 1) / max) // ceil(count/max * levels)
	if lvl < 1 {
		lvl = 1
	}
	if lvl > ascHeatLevels {
		lvl = ascHeatLevels
	}
	return lvl
}

// ascHeatDay is the day of t in loc, at midnight.
func ascHeatDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// ascHeatSum sums the activity by day of loc ("2006-01-02"), keeping the days from first to last.
func ascHeatSum(data []*activities_model.UserHeatmapData, first, last time.Time, loc *time.Location) map[string]int64 {
	perDay := map[string]int64{}
	for _, d := range data {
		if d == nil || d.Contributions <= 0 {
			continue
		}
		day := ascHeatDay(d.Timestamp.AsTime(), loc)
		if day.Before(first) || day.After(last) {
			continue
		}
		perDay[day.Format("2006-01-02")] += d.Contributions
	}
	return perDay
}

// ascHeatBuild lays the activity of the last year out as columns of weeks ending with the week of now. The
// days are cut in loc. The data is grouped in 15 minutes; entries older than the grid are dropped.
func ascHeatBuild(data []*activities_model.UserHeatmapData, now time.Time, loc *time.Location, l translation.Locale) *AscHeat {
	if loc == nil {
		loc = time.UTC
	}
	today := ascHeatDay(now, loc)
	// the grid starts on the Sunday ascHeatWeeks-1 weeks before the Sunday of this week
	start := today.AddDate(0, 0, -int(today.Weekday())-7*(ascHeatWeeks-1))
	return ascHeatLayout(ascHeatSum(data, start, today, loc), nil, start, today, l, nil)
}

// ascHeatLayout lays the days first to last (midnights of one location) out as columns of weeks, from the Sunday
// of the week of first to the Saturday of the week of last; a day outside the range is blank. A day counts its
// activity and the changes of agent runs (agents); link, when not nil, gives the address each day opens.
func ascHeatLayout(perDay, agents map[string]int64, first, last time.Time, l translation.Locale, link func(day string) string) *AscHeat {
	h := &AscHeat{}
	var max int64
	count := func(key string) int64 { return perDay[key] + agents[key] }
	for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		c := count(key)
		if c <= 0 {
			continue
		}
		h.Total += c
		h.Agents += agents[key]
		h.Days++
		max = max64(max, c)
	}
	start := first.AddDate(0, 0, -int(first.Weekday()))
	end := last.AddDate(0, 0, 6-int(last.Weekday()))
	weeks := int(end.Sub(start).Hours()/24+0.5)/7 + 1
	lastMonth := time.Month(0)
	for w := 0; w < weeks; w++ {
		col := make([]AscHeatDay, 7)
		for r := 0; r < 7; r++ {
			day := start.AddDate(0, 0, w*7+r)
			if day.Before(first) || day.After(last) {
				col[r] = AscHeatDay{Blank: true}
				continue
			}
			key := day.Format("2006-01-02")
			c, a := count(key), agents[key]
			when := day.Format("Mon, 2 Jan 2006")
			label := l.TrString("user.asc_profile.no_contributions", when)
			if c > 0 {
				label = string(l.TrN(int(c), "user.asc_profile.contributions_one", "user.asc_profile.contributions_n", c, when))
				if a > 0 {
					label = l.TrString("user.asc_profile.by_agents", label, a)
				}
			}
			cell := AscHeatDay{Level: ascHeatLevel(c, max), Label: label, Agent: a > 0 && a*2 >= c}
			if link != nil && c > 0 {
				cell.Link = link(key)
			}
			col[r] = cell
		}
		h.Weeks = append(h.Weeks, col)
		// the label of a month goes above the first column that starts in it
		top := start.AddDate(0, 0, w*7)
		if top.Before(first) {
			top = first
		}
		if top.Month() != lastMonth && !top.After(last) {
			if w < weeks-1 || top.Day() <= 7 {
				h.Months = append(h.Months, AscHeatMonth{Name: top.Format("Jan"), Col: w + 1})
			}
			lastMonth = top.Month()
		}
	}
	// a label too close to the next one would overlap it: keep one per two columns at least
	var kept []AscHeatMonth
	for _, m := range h.Months {
		if len(kept) > 0 && m.Col-kept[len(kept)-1].Col < 3 {
			continue
		}
		kept = append(kept, m)
	}
	h.Months = kept
	// the table: the days with activity, newest first
	for day := last; !day.Before(first); day = day.AddDate(0, 0, -1) {
		key := day.Format("2006-01-02")
		if c := count(key); c > 0 {
			row := AscHeatRow{Date: day.Format("Mon, 2 Jan 2006"), Count: c, Agents: agents[key]}
			if link != nil {
				row.Link = link(key)
			}
			h.Rows = append(h.Rows, row)
		}
	}
	return h
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// AscProfileWork is one pull request the person is working on.
type AscProfileWork struct {
	Title      string
	Repo       string
	Link       string
	Tone       string // amber, green, red or grey
	State      string // "Draft", "Ready", "Conflict" or empty for a change in review
	StateTone  string
	Detail     string // "stack of 3 - change 2 in review - last pushed 40 min ago" without the time
	Updated    time.Time
	StackTotal int
}

// AscProfileRepo is one repository of the card "Repositories".
type AscProfileRepo struct {
	FullName string
	Link     string
	Changes  int64
}

// ascProfileWork lists the open pull requests of user that viewer may read, newest change first.
func ascProfileWork(ctx gocontext.Context, user, viewer *user_model.User, l translation.Locale) []AscProfileWork {
	return ascProfileWorkN(ctx, user, viewer, l, ascProfileWorkMax)
}

// ascProfileWorkN is ascProfileWork with the number of rows to find.
func ascProfileWorkN(ctx gocontext.Context, user, viewer *user_model.User, l translation.Locale, limit int) []AscProfileWork {
	var issues []*issues_model.Issue
	err := db.GetEngine(ctx).
		Where("is_pull = ? AND poster_id = ? AND is_closed = ?", true, user.ID, false).
		OrderBy("updated_unix DESC").Limit(max(ascProfileScan, limit*4)).Find(&issues)
	if err != nil {
		log.Error("asc profile: pull requests of user %d: %v", user.ID, err)
		return nil
	}
	var out []AscProfileWork
	for _, issue := range issues {
		if len(out) >= limit {
			break
		}
		if issue.LoadRepo(ctx) != nil || issue.LoadPullRequest(ctx) != nil {
			continue
		}
		pr := issue.PullRequest
		if pr.LoadBaseRepo(ctx) != nil || !ascCanReadPulls(ctx, viewer, pr.BaseRepo) {
			continue
		}
		w := AscProfileWork{
			Title:   issue.Title,
			Repo:    pr.BaseRepo.FullName(),
			Link:    ascPullLink(pr),
			Updated: issue.UpdatedUnix.AsTime(),
			Tone:    "amber",
		}
		switch {
		case pr.IsWorkInProgress(ctx):
			w.Tone, w.State, w.StateTone = "grey", l.TrString("user.asc_dash.draft"), "grey"
		case pr.Status == issues_model.PullRequestStatusConflict:
			w.Tone, w.State, w.StateTone = "red", l.TrString("user.asc_profile.conflict"), "red"
		case ascInboxApproved(ctx, pr):
			w.Tone, w.State, w.StateTone = "green", l.TrString("user.asc_profile.ready"), "green"
		}
		if idx, total := ascStackPosition(ctx, pr.ID); total > 1 {
			w.StackTotal = total
			w.Detail = l.TrString("user.asc_profile.stack_detail", total, idx)
		}
		out = append(out, w)
	}
	return out
}

// ascCanReadPulls reports whether viewer (nil for a visitor who is not signed in) may read the pull requests
// of repo.
func ascCanReadPulls(ctx gocontext.Context, viewer *user_model.User, repo *repo_model.Repository) bool {
	if repo == nil {
		return false
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, viewer)
	return err == nil && perm.CanRead(unit.TypePullRequests)
}

// AscProfileFocus is a subject the person works on (a Focus chip): a topic or the main language of the
// repositories they change, linked to the repositories with it.
type AscProfileFocus struct {
	Name string
	Link string
}

// ascProfileRepos lists the repositories where user wrote the most pull requests, as far as viewer may read them,
// and the Focus of user: the topics and main languages of those repositories, weighted by the pull requests
// there, the heaviest first.
func ascProfileRepos(ctx gocontext.Context, user, viewer *user_model.User) ([]AscProfileRepo, []AscProfileFocus) {
	var rows []struct {
		RepoID int64
		N      int64
	}
	err := db.GetEngine(ctx).Table("issue").
		Select("repo_id, count(*) AS n").
		Where("is_pull = ? AND poster_id = ?", true, user.ID).
		GroupBy("repo_id").OrderBy("n DESC, repo_id").Limit(ascProfileScan).Find(&rows)
	if err != nil {
		log.Error("asc profile: repositories of user %d: %v", user.ID, err)
		return nil, nil
	}
	var out []AscProfileRepo
	focus := ascFocusTally{}
	readable := 0
	for _, r := range rows {
		if len(out) == ascProfileReposMax && readable == ascProfileFocusRepos {
			break
		}
		repo, err := repo_model.GetRepositoryByID(ctx, r.RepoID)
		if err != nil || !ascCanReadPulls(ctx, viewer, repo) {
			continue
		}
		if len(out) < ascProfileReposMax {
			out = append(out, AscProfileRepo{FullName: repo.FullName(), Link: repo.Link(), Changes: r.N})
		}
		if readable == ascProfileFocusRepos {
			continue
		}
		readable++
		for _, t := range repo.Topics {
			focus.add(t, setting.AppSubURL+"/explore/repos?q="+url.QueryEscape(t)+"&topic=1", r.N)
		}
		langs, err := repo_model.GetTopLanguageStats(ctx, repo, 1)
		if err != nil {
			log.Error("asc profile: languages of repository %d: %v", repo.ID, err)
			continue
		}
		for _, l := range langs {
			if l.Language == "other" { // the rest of the languages together, not a subject
				continue
			}
			focus.add(l.Language, setting.AppSubURL+"/explore/repos?language="+url.QueryEscape(l.Language), r.N)
		}
	}
	return out, focus.top(ascProfileFocusMax)
}

// ascFocusTally adds up the weight of each Focus subject; names that differ only in case are one subject, the
// first link seen kept.
type ascFocusTally map[string]*ascFocusWeight

type ascFocusWeight struct {
	AscProfileFocus
	weight int64
}

func (f ascFocusTally) add(name, link string, weight int64) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := strings.ToLower(name)
	if w, ok := f[key]; ok {
		w.weight += weight
		return
	}
	f[key] = &ascFocusWeight{AscProfileFocus{Name: name, Link: link}, weight}
}

// top is at most n subjects, the heaviest first, then by name.
func (f ascFocusTally) top(n int) []AscProfileFocus {
	all := make([]*ascFocusWeight, 0, len(f))
	for _, w := range f {
		all = append(all, w)
	}
	slices.SortFunc(all, func(a, b *ascFocusWeight) int {
		return cmp.Or(cmp.Compare(b.weight, a.weight), strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
	})
	var out []AscProfileFocus
	for _, w := range all[:min(n, len(all))] {
		out = append(out, w.AscProfileFocus)
	}
	return out
}

// ascProfileOverview prepares what the overview tab of a person's profile shows, for the viewer. Activity that
// the person keeps private, and the pull requests and repositories drawn from it, are left out for everyone
// except the person and administrators.
func ascProfileOverview(ctx *context.Context) {
	person := ctx.ContextUser
	if !activities_model.ActivityReadable(person, ctx.Doer) {
		return
	}
	if setting.Service.EnableUserHeatmap {
		heat, err := ascProfileHeat(ctx, "")
		if err != nil {
			ctx.ServerError("ascProfileHeat", err)
			return
		}
		ctx.Data["AscHeat"] = heat
	}
	showPrivate := ctx.IsSigned && (ctx.Doer.IsAdmin || ctx.Doer.ID == person.ID)
	feeds, _, err := activities_model.GetFeeds(ctx, activities_model.GetFeedsOptions{
		RequestedUser:   person,
		Actor:           ctx.Doer,
		IncludePrivate:  showPrivate,
		OnlyPerformedBy: true,
		ListOptions:     db.ListOptions{PageSize: ascProfileActivityMax, Page: 1},
	})
	if err != nil {
		ctx.ServerError("GetFeeds", err)
		return
	}
	ctx.Data["Feeds"] = feeds
	ctx.Data["AscProfileWork"] = ascProfileWork(ctx, person, ctx.Doer, ctx.Locale)
	ctx.Data["AscProfileRepos"], ctx.Data["AscProfileFocus"] = ascProfileRepos(ctx, person, ctx.Doer)
	ctx.Data["AscPins"] = ascProfilePins(ctx, person, ctx.Doer)
	ctx.Data["AscProfileAgents"] = ascProfileAgents(ctx)
	ctx.Data["AscProfileKnows"] = ascProfileKnows(ctx, person, ctx.Doer)
	ascProfilePinForm(ctx)
}
