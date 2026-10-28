// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
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
)

// AscHeatDay is one square of the grid.
type AscHeatDay struct {
	Level int    // 0 for no activity, else 1 to ascHeatLevels
	Label string // "3 contributions on Mon, 12 Oct 2026"
	Blank bool   // a day after today: nothing is drawn
}

// AscHeatMonth is a month label above the grid, at the first column that starts in that month.
type AscHeatMonth struct {
	Name string
	Col  int // 1 based grid column
}

// AscHeat is the contribution grid: Weeks[column][row], row 0 is Sunday.
type AscHeat struct {
	Weeks  [][]AscHeatDay
	Months []AscHeatMonth
	Total  int64
	Days   int // days that have activity
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

// ascHeatBuild lays the activity of the last year out as columns of weeks ending with the week of now. The
// days are cut in loc. The data is grouped in 15 minutes; entries older than the grid are dropped.
func ascHeatBuild(data []*activities_model.UserHeatmapData, now time.Time, loc *time.Location, l translation.Locale) *AscHeat {
	if loc == nil {
		loc = time.UTC
	}
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	// the grid starts on the Sunday ascHeatWeeks-1 weeks before the Sunday of this week
	thisSunday := today.AddDate(0, 0, -int(today.Weekday()))
	start := thisSunday.AddDate(0, 0, -7*(ascHeatWeeks-1))

	perDay := map[string]int64{}
	var total int64
	for _, d := range data {
		if d == nil || d.Contributions <= 0 {
			continue
		}
		t := d.Timestamp.AsTime().In(loc)
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		if day.Before(start) || day.After(today) {
			continue
		}
		perDay[day.Format("2006-01-02")] += d.Contributions
		total += d.Contributions
	}
	var max int64
	for _, c := range perDay {
		if c > max {
			max = c
		}
	}

	h := &AscHeat{Total: total, Days: len(perDay)}
	lastMonth := time.Month(0)
	for w := 0; w < ascHeatWeeks; w++ {
		col := make([]AscHeatDay, 7)
		for r := 0; r < 7; r++ {
			day := start.AddDate(0, 0, w*7+r)
			if day.After(today) {
				col[r] = AscHeatDay{Blank: true}
				continue
			}
			c := perDay[day.Format("2006-01-02")]
			label := l.TrString("user.asc_profile.no_contributions", day.Format("Mon, 2 Jan 2006"))
			if c > 0 {
				label = string(l.TrN(int(c), "user.asc_profile.contributions_one", "user.asc_profile.contributions_n", c, day.Format("Mon, 2 Jan 2006")))
			}
			col[r] = AscHeatDay{Level: ascHeatLevel(c, max), Label: label}
		}
		h.Weeks = append(h.Weeks, col)
		// the label of a month goes above the first column that starts in it
		first := start.AddDate(0, 0, w*7)
		if first.Month() != lastMonth && !first.After(today) {
			if w < ascHeatWeeks-1 || first.Day() <= 7 {
				h.Months = append(h.Months, AscHeatMonth{Name: first.Format("Jan"), Col: w + 1})
			}
			lastMonth = first.Month()
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
	return h
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

// ascProfileRepos lists the repositories where user wrote the most pull requests, as far as viewer may read them.
func ascProfileRepos(ctx gocontext.Context, user, viewer *user_model.User) []AscProfileRepo {
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
		return nil
	}
	var out []AscProfileRepo
	for _, r := range rows {
		if len(out) == ascProfileReposMax {
			break
		}
		repo, err := repo_model.GetRepositoryByID(ctx, r.RepoID)
		if err != nil || !ascCanReadPulls(ctx, viewer, repo) {
			continue
		}
		out = append(out, AscProfileRepo{FullName: repo.FullName(), Link: repo.Link(), Changes: r.N})
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
		data, err := activities_model.GetUserHeatmapDataByUser(ctx, person, ctx.Doer)
		if err != nil {
			ctx.ServerError("GetUserHeatmapDataByUser", err)
			return
		}
		ctx.Data["AscHeat"] = ascHeatBuild(data, time.Now(), setting.DefaultUILocation, ctx.Locale)
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
	ctx.Data["AscProfileRepos"] = ascProfileRepos(ctx, person, ctx.Doer)
	ctx.Data["AscPins"] = ascProfilePins(ctx, person, ctx.Doer)
}
