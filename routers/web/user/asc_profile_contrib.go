// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/url"
	"slices"
	"strconv"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/translation"
	"forgejo.org/services/context"

	"xorm.io/builder"
)

// The contribution grid of a profile (design board "Profile") is worked out from the hub's own data: the activity
// of the person, the pull requests the person opened, the agent runs the person started whose change the hub opened
// as a pull request (pull_request_asc_run, a violet square) and the reviews the person gave. Every count is limited
// to the repositories whose pull requests the viewer may read, and nothing is counted when the person keeps their
// activity private from the viewer.

const (
	// ascContribYears is how many calendar years the year switch offers, this year included.
	ascContribYears = 10
	// ascContribScan bounds the rows read for one count.
	ascContribScan = 10000
)

// ascContribWindow is the range of days a year of the switch covers, as midnights of loc: year 0 is the last
// twelve months (the 53 weeks of the dashboard grid), another year runs from 1 January to 31 December, or to today
// for this year.
func ascContribWindow(year int, now time.Time, loc *time.Location) (first, last time.Time) {
	today := ascHeatDay(now, loc)
	if year == 0 {
		return today.AddDate(0, 0, -int(today.Weekday())-7*(ascHeatWeeks-1)), today
	}
	first = time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	last = time.Date(year, time.December, 31, 0, 0, 0, 0, loc)
	if last.After(today) {
		last = today
	}
	return first, last
}

// ascContribYear reads ?year= : a calendar year the switch offers, else 0 (the last twelve months).
func ascContribYear(raw string, person *user_model.User, now time.Time, loc *time.Location) int {
	y, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	if slices.Contains(ascContribYearList(person, now, loc), y) {
		return y
	}
	return 0
}

// ascContribYearList is the calendar years of the switch, newest first: this year back to the year the person
// joined, at most ascContribYears.
func ascContribYearList(person *user_model.User, now time.Time, loc *time.Location) []int {
	this := now.In(loc).Year()
	from := this - ascContribYears + 1
	if person != nil && person.CreatedUnix > 0 {
		from = max(from, person.CreatedUnix.AsTimeInLocation(loc).Year())
	}
	from = min(from, this)
	years := make([]int, 0, this-from+1)
	for y := this; y >= from; y-- {
		years = append(years, y)
	}
	return years
}

// ascContribLink is the address of the profile tab with the given query values.
func ascContribLink(person *user_model.User, tab string, values url.Values) string {
	if tab != "" {
		values.Set("tab", tab)
	}
	if len(values) == 0 {
		return person.HomeLink()
	}
	return person.HomeLink() + "?" + values.Encode()
}

// ascProfileHeat builds the contribution grid of the person for the viewer, for the year of ?year= ; tab is the
// profile tab the year switch stays on ("" for the overview). It returns nil when the viewer may not read the
// person's activity.
func ascProfileHeat(ctx *context.Context, tab string) (*AscHeat, error) {
	person := ctx.ContextUser
	if !activities_model.ActivityReadable(person, ctx.Doer) {
		return nil, nil
	}
	loc := setting.DefaultUILocation
	now := time.Now()
	year := ascContribYear(ctx.FormString("year"), person, now, loc)
	first, last := ascContribWindow(year, now, loc)
	from, until := timeutil.TimeStamp(first.Unix()), timeutil.TimeStamp(last.AddDate(0, 0, 1).Unix())

	data, err := activities_model.GetUserHeatmapDataByUserRange(ctx, person, ctx.Doer, from, until)
	if err != nil {
		return nil, err
	}
	counts, err := ascContribCounts(ctx, person, ctx.Doer, from, until, loc)
	if err != nil {
		return nil, err
	}
	perDay := ascHeatSum(data, first, last, loc)
	// a pull request of an agent run that the person posted is in their activity already: it counts once, as
	// the agent's
	for day, n := range counts.agentOwn {
		perDay[day] = max(perDay[day]-n, 0)
	}
	link := func(day string) string {
		return ascContribLink(person, "activity", url.Values{"date": {day}})
	}
	h := ascHeatLayout(perDay, counts.agents, first, last, ctx.Locale, link)
	h.Year = year
	h.Years = append(h.Years, AscHeatYear{Year: 0, Link: ascContribLink(person, tab, url.Values{}), Current: year == 0})
	for _, y := range ascContribYearList(person, now, loc) {
		h.Years = append(h.Years, AscHeatYear{Year: y, Link: ascContribLink(person, tab, url.Values{"year": {strconv.Itoa(y)}}), Current: y == year})
	}
	h.Summary = &AscHeatSummary{
		Changes:      counts.changes,
		AgentChanges: counts.agentChanges,
		Reviews:      counts.reviews,
		Turnaround:   ascTurnaround(counts.turnaround, ctx.Locale),
	}
	return h, nil
}

// ascContrib is what ascContribCounts finds in a range.
type ascContrib struct {
	agents       map[string]int64 // per day, the pull requests of agent runs the person started
	agentOwn     map[string]int64 // per day, those of them the person posted
	changes      int64            // pull requests the person opened, agent runs left out
	agentChanges int64
	reviews      int64
	turnaround   []int64 // per pull request the person reviewed, seconds from its opening to their first review
}

// ascReadablePulls limits a query on issue to the repositories whose pull requests doer may read.
func ascReadablePulls(doer *user_model.User) builder.Cond {
	return builder.In("issue.repo_id", builder.Select("id").From("repository").Where(repo_model.AccessibleRepositoryCondition(doer, unit.TypePullRequests)))
}

// ascContribCounts counts the person's changes, agent changes and reviews in [from, until) that doer may read;
// days are cut in loc.
func ascContribCounts(ctx *context.Context, person, doer *user_model.User, from, until timeutil.TimeStamp, loc *time.Location) (*ascContrib, error) {
	c := &ascContrib{agents: map[string]int64{}, agentOwn: map[string]int64{}}
	e := db.GetEngine(ctx)
	inRange := builder.Gte{"issue.created_unix": from}.And(builder.Lt{"issue.created_unix": until})

	var runs []struct {
		Created  timeutil.TimeStamp `xorm:"created_unix"`
		PosterID int64              `xorm:"poster_id"`
	}
	if err := e.Table("pull_request_asc_run").
		Join("INNER", "pull_request", "pull_request.id = pull_request_asc_run.pull_id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where(builder.Eq{"LOWER(pull_request_asc_run.started_by)": person.LowerName}.And(inRange, ascReadablePulls(doer))).
		Select("issue.created_unix, issue.poster_id").
		Limit(ascContribScan).
		Find(&runs); err != nil {
		return nil, err
	}
	for _, r := range runs {
		day := ascHeatDay(r.Created.AsTime(), loc).Format("2006-01-02")
		c.agents[day]++
		if r.PosterID == person.ID {
			c.agentOwn[day]++
		}
	}
	c.agentChanges = int64(len(runs))

	changes, err := e.Table("issue").
		Where(builder.Eq{"issue.poster_id": person.ID, "issue.is_pull": true}.And(inRange, ascReadablePulls(doer))).
		And(builder.NotIn("issue.id", builder.Select("pull_request.issue_id").From("pull_request").
			Join("INNER", "pull_request_asc_run", "pull_request_asc_run.pull_id = pull_request.id"))).
		Count()
	if err != nil {
		return nil, err
	}
	c.changes = changes

	given := builder.Eq{"review.reviewer_id": person.ID}.
		And(builder.In("review.type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeComment, issues_model.ReviewTypeReject)).
		And(builder.Gte{"review.created_unix": from}, builder.Lt{"review.created_unix": until}).
		And(ascReadablePulls(doer))
	reviews, err := e.Table("review").Join("INNER", "issue", "issue.id = review.issue_id").Where(given).Count()
	if err != nil {
		return nil, err
	}
	c.reviews = reviews

	var firsts []struct {
		Opened timeutil.TimeStamp `xorm:"opened"`
		First  timeutil.TimeStamp `xorm:"first"`
	}
	if err := e.Table("review").Join("INNER", "issue", "issue.id = review.issue_id").
		Where(given.And(builder.Neq{"issue.poster_id": person.ID})).
		Select("issue.created_unix AS opened, MIN(review.created_unix) AS first").
		GroupBy("review.issue_id, issue.created_unix").
		Limit(ascContribScan).
		Find(&firsts); err != nil {
		return nil, err
	}
	for _, f := range firsts {
		c.turnaround = append(c.turnaround, max(int64(f.First-f.Opened), 0))
	}
	return c, nil
}

// ascTurnaround is the median of the turnarounds in seconds, in minutes, hours or days; "" when there are none.
func ascTurnaround(secs []int64, l translation.Locale) string {
	if len(secs) == 0 {
		return ""
	}
	s := slices.Clone(secs)
	slices.Sort(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	switch {
	case m < 3600:
		n := max(m/60, 1)
		return l.TrString("user.asc_profile.turn_minutes", n)
	case m < 48*3600:
		return l.TrString("user.asc_profile.turn_hours", m/3600)
	default:
		return l.TrString("user.asc_profile.turn_days", m/86400)
	}
}
