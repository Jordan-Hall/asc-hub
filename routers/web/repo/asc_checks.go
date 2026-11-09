// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"

	actions_service "forgejo.org/services/actions"

	"google.golang.org/grpc"
)

// The checks pages (docs/BUILDOUT.md section 8, the Checks board).
//
//	GET  /{owner}/{repo}/asc/checks                       failure groups, the flaky tests
//	GET  /{owner}/{repo}/asc/checks/groups/{id}           one group: where it happened, the history strip
//	POST /{owner}/{repo}/asc/checks/groups/{id}/state     acknowledge, quarantine, resolve, reopen
//	POST /{owner}/{repo}/asc/checks/rerun                 re-run a run of this repository
//	GET  /{owner}/{repo}/pulls/{index}/asc/checks         the checks of one version of a change
//	POST /{owner}/{repo}/pulls/{index}/asc/checks/rerun   re-run the failed runs, or all runs, of that version
//
// Reading needs the right to read the code, as for the rest of the repository; the reads are made with the hub's
// own credential because Forgejo has decided before the handler runs. A change of state is made as the person, and
// asc decides: quarantine needs a maintainer and a policy that allows it. Re-running needs write access to the
// repository's Actions and uses Forgejo's own re-run, so it takes the same checks as the button of the Actions page.
//
// Results belong to the exact version being read. A required check without a result on this version is shown as
// "no result for this version", never as passing. The text of a failure comes from a runner: it is cut, stripped
// of control characters, and escaped by the template.
const (
	tplAscChecks     base.TplName = "repo/asc_checks"
	tplAscCheckGroup base.TplName = "repo/asc_check_group"
	tplAscPullChecks base.TplName = "repo/pulls/asc_checks"

	maxChecksForm = 16 << 10
)

// ascChecksPage is the template data under .AscChecks.
type ascChecksPage struct {
	Data        *asctrust.ChecksPage
	View        string
	Views       []ascChecksView
	State       string
	States      []ascChecksState
	Flaky       bool
	Job         string
	Test        string
	Window      uint32
	Jobs        []asctrust.JobStat
	FlakyRows   []asctrust.FlakyRow
	Trends      []asctrust.TrendRow
	NextURL     string
	Problem     string
	Unsupported bool
	ChecksURL   string
	CanWrite    bool
	CanRerun    bool
	// Side is the number of groups in each state and the quarantined groups (nil: not read).
	Side *asctrust.ChecksSide
	// Main is the state of the default branch: green, failing, running or "" when it has no result.
	Main     string
	MainName string
	// Retry is true when Problem is a failure to reach asc, which trying again can fix.
	Retry bool
	// Rows are the failure groups with what the list offers on each; Open is the one shown expanded, nil for none.
	Rows []ascGroupRowView
	Open *ascGroupOpen
	// AllowQuarantine is true when the viewer may write and the policy allows quarantine ([ci] allow_quarantine).
	AllowQuarantine bool
	// ListQuery is the query of the list without open and act: the forms of a row carry it, so a change of state
	// comes back to the same list.
	ListQuery string
	// Filtered is true when a job, a test or "flaky only" narrows the list.
	Filtered  bool
	TrendsURL string
	// WindowLinks choose the window of every view; Stats are the four stat cards over it (nil: asc could not say).
	WindowLinks []ascWindowLink
	Stats       []ascStatCard
}

// ascWindowLink is one choice of the window: the same view and filters over Days days.
type ascWindowLink struct {
	Days   uint32
	URL    string
	Active bool
}

// ascStatCard is one stat card of the checks page: its label is asc_trust.checks.stat_<Key>, Sub the line under the
// value and Spark the trend of the window day by day (asctrust.Sparkline, "" when it has nothing to draw).
type ascStatCard struct {
	Key       string
	Value     string
	Tone      string
	Sub       string
	Spark     string
	SparkTone string
}

// ascGroupRowView is one failure group of the list.
type ascGroupRowView struct {
	asctrust.GroupRow
	// Op is the operation id of the state forms of the row.
	Op string
	// Expanded is true for the group shown with its log; ToggleURL shows or hides it.
	Expanded  bool
	ToggleURL string
	// AckURL and QuarantineURL open the group with its note field and that action first; "" when not offered.
	AckURL        string
	QuarantineURL string
	CanResolve    bool
	CanReopen     bool
}

// Dot is the tone of the dot at the start of the row.
func (r ascGroupRowView) Dot() string {
	switch r.StateKey {
	case "open":
		return "err"
	case "resolved":
		return "ok"
	}
	return "warn"
}

// ascGroupOpen is the expanded group of the list: an excerpt of its newest log, where it happened, what to do with it.
type ascGroupOpen struct {
	ID          uint64
	Lines       []ascLogLine
	LogOf       *ascOccurrenceView
	Occurrences []ascOccurrenceView
	// More is true when the group has more occurrences than the list shows; the group page has them all.
	More bool
	// Act is the action the person chose on the row ("acknowledged", "quarantined"), shown first; "" for none.
	Act        string
	IssueQuery template.URL
	// Op is the operation id of the form, the one of the row.
	Op string
}

// ascLogLine is one numbered line of a log excerpt.
type ascLogLine struct {
	N    int
	Text string
}

// The expanded group shows this many occurrences and this many lines of the newest log.
const (
	ascOpenOccurrences = 5
	ascOpenLogLines    = 40
)

// ascLogLines numbers the lines of a log excerpt and keeps the last limit of them, where a failure ends.
func ascLogLines(msg string, limit int) []ascLogLine {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return nil
	}
	lines := strings.Split(msg, "\n")
	first := 0
	if len(lines) > limit {
		first = len(lines) - limit
	}
	out := make([]ascLogLine, 0, len(lines)-first)
	for i := first; i < len(lines); i++ {
		out = append(out, ascLogLine{N: i + 1, Text: strings.TrimRight(lines[i], "\r")})
	}
	return out
}

// ascListBack is the list query a state form sent back, with only the keys of the list; anything else is dropped.
func ascListBack(raw string) url.Values {
	in, err := url.ParseQuery(raw)
	out := url.Values{}
	if err != nil {
		return out
	}
	for _, k := range []string{"state", "job", "test", "flaky", "page"} {
		if v := in.Get(k); v != "" && len(v) <= 512 {
			out.Set(k, v)
		}
	}
	return out
}

// ascOccurrenceViews links the occurrences to the runs of this repository and names their jobs.
func ascOccurrenceViews(ctx *context.Context, repo *repo_model.Repository, rows []asctrust.OccurrenceRow) []ascOccurrenceView {
	out := make([]ascOccurrenceView, 0, len(rows))
	for _, o := range rows {
		view := ascOccurrenceView{OccurrenceRow: o}
		// A run id from asc is only a link when the run belongs to this repository.
		if o.RunID > 0 && o.RunID < 1<<62 {
			if run, err := actions_model.GetRunByID(ctx, int64(o.RunID)); err == nil && run.RepoID == repo.ID {
				view.RunURL = repo.Link() + "/actions/runs/" + strconv.FormatInt(run.Index, 10)
				view.RunID = run.ID
				if o.JobID > 0 && o.JobID < 1<<62 {
					if job, err := actions_model.GetRunJobByID(ctx, int64(o.JobID)); err == nil && job.RunID == run.ID {
						view.JobName = job.Name
					}
				}
			}
		}
		out = append(out, view)
	}
	return out
}

type ascChecksState struct {
	Key, Label string
	Active     bool
	// Count is the number of groups in the state, "" when it is not known; a long list reads "100+".
	Count string
}

// ascCountLabel is the text of a group count: "3", or "100+" when there are more.
func ascCountLabel(c asctrust.GroupCount) string {
	if c.More {
		return strconv.Itoa(c.N) + "+"
	}
	return strconv.Itoa(c.N)
}

// ascMainState is the state of the newest commit of the default branch: green, failing, running, or "" when it has
// no result or cannot be read.
func ascMainState(ctx *context.Context, repo *repo_model.Repository) string {
	if ctx.Repo.GitRepo == nil || repo.DefaultBranch == "" {
		return ""
	}
	sha, err := ctx.Repo.GitRepo.GetBranchCommitID(repo.DefaultBranch)
	if err != nil {
		return ""
	}
	statuses, _, err := git_model.GetLatestCommitStatus(ctx, repo.ID, sha, db.ListOptionsAll)
	if err != nil {
		return ""
	}
	worst := git_model.CalcCommitStatus(statuses)
	if worst == nil {
		return ""
	}
	switch {
	case worst.State.IsSuccess():
		return "green"
	case worst.State.IsFailure() || worst.State.IsError():
		return "failing"
	case worst.State.IsPending():
		return "running"
	}
	return ""
}

// ascChecksView is one tab of the CI failures page.
type ascChecksView struct {
	Key    string
	URL    string
	Active bool
}

// The views of the CI failures page: the failing jobs and failure groups, the flaky tests, the trend of the jobs.
const (
	ascChecksViewFailures = "failures"
	ascChecksViewFlaky    = "flaky"
	ascChecksViewTrends   = "trends"
)

// ascChecksWindows are the windows of a history, in days. An address may still name 180 (asctrust.WindowDays).
var ascChecksWindows = []uint32{7, 14, 30, 90}

// ascChecksWindowLinks are the windows of the header: the address of the page with its window replaced and the page of
// the list, the expanded group and the action left out.
func ascChecksWindowLinks(base string, query url.Values, window uint32) []ascWindowLink {
	out := make([]ascWindowLink, 0, len(ascChecksWindows))
	for _, w := range ascChecksWindows {
		q := url.Values{}
		for k, v := range query {
			switch k {
			case "page", "open", "act", "window":
			default:
				q[k] = v
			}
		}
		q.Set("window", strconv.FormatUint(uint64(w), 10))
		out = append(out, ascWindowLink{Days: w, URL: base + "?" + q.Encode(), Active: w == window})
	}
	return out
}

// ascPermilleText writes a rate in per mille as a percentage with one decimal when it has one: 942 is "94.2", 100 is "10".
func ascPermilleText(p uint32) string {
	if p%10 == 0 {
		return strconv.FormatUint(uint64(p/10), 10)
	}
	return strconv.FormatUint(uint64(p/10), 10) + "." + strconv.FormatUint(uint64(p%10), 10)
}

// ascCarry fills the days without a value with the value of the day before (the first known value for the days before
// it), so a quiet day does not draw a fall to zero. known says which days have a value.
func ascCarry(values []uint32, known []bool) []uint32 {
	out := make([]uint32, len(values))
	first := -1
	for i := range values {
		if known[i] {
			first = i
			break
		}
	}
	if first < 0 {
		return nil
	}
	last := values[first]
	for i := range values {
		if known[i] {
			last = values[i]
		}
		out[i] = last
	}
	return out
}

// ascChecksStats builds the four stat cards from the statistics of the window.
func ascChecksStats(ctx *context.Context, s *asctrust.CiStats) []ascStatCard {
	n := len(s.Daily)
	pass, durations := make([]uint32, n), make([]uint32, n)
	hasRuns, hasDuration := make([]bool, n), make([]bool, n)
	groups, flips := make([]uint32, n), make([]uint32, n)
	for i, d := range s.Daily {
		if d.Runs > 0 {
			pass[i], hasRuns[i] = d.Passed*1000/d.Runs, true
		}
		durations[i], hasDuration[i] = d.MedianSeconds, d.MedianSeconds > 0
		groups[i], flips[i] = d.GroupsNew, d.Flips
	}
	const w, h = 80, 28

	rate := ascStatCard{Key: "pass", Value: "\u2014", Spark: asctrust.Sparkline(ascCarry(pass, hasRuns), w, h)}
	switch {
	case s.Runs == 0:
		rate.Sub = ctx.Locale.TrString("asc_trust.checks.stat_no_runs", s.Days)
	default:
		rate.Value = ascPermilleText(s.PassPermille) + "%"
		switch {
		case s.PassPermille >= 900:
			rate.Tone = "ok"
		case s.PassPermille >= 700:
			rate.Tone = "warn"
		default:
			rate.Tone = "err"
		}
		diff := int(s.PassPermille) - int(s.PreviousPassPermille)
		switch {
		case s.PreviousRuns == 0:
			rate.Sub = ctx.Locale.TrString("asc_trust.checks.stat_no_previous", s.Days)
		case diff > 0:
			rate.Sub = ctx.Locale.TrString("asc_trust.checks.stat_pass_up", ascPermilleText(uint32(diff)), s.Days)
		case diff < 0:
			rate.Sub = ctx.Locale.TrString("asc_trust.checks.stat_pass_down", ascPermilleText(uint32(-diff)), s.Days)
		default:
			rate.Sub = ctx.Locale.TrString("asc_trust.checks.stat_pass_same", s.Days)
		}
	}

	open := ascStatCard{Key: "groups", Value: strconv.FormatUint(uint64(s.GroupsOpen), 10), SparkTone: "err", Spark: asctrust.Sparkline(groups, w, h)}
	if s.GroupsOpen > 0 {
		open.Tone = "err"
	}
	if s.GroupsNewHour == 0 {
		open.Sub = ctx.Locale.TrString("asc_trust.checks.stat_new_hour_none")
	} else {
		open.Sub = ctx.Locale.TrString("asc_trust.checks.stat_new_hour", s.GroupsNewHour)
	}

	flaky := ascStatCard{
		Key: "flaky", Value: strconv.FormatUint(uint64(s.Flipping), 10), SparkTone: "warn", Spark: asctrust.Sparkline(flips, w, h),
		Sub: ctx.Locale.TrString("asc_trust.checks.stat_above_threshold", s.Flaky, ascPermilleText(s.ThresholdPermille)+"%"),
	}

	median := ascStatCard{Key: "duration", Value: "\u2014", Spark: asctrust.Sparkline(ascCarry(durations, hasDuration), w, h)}
	if s.MedianSeconds > 0 {
		median.Value = asctrust.DurationText(s.MedianSeconds)
	}
	if s.SlowestJob != "" {
		median.Sub = ctx.Locale.TrString("asc_trust.checks.stat_slowest", s.SlowestJob, asctrust.DurationText(s.SlowestJobSeconds))
	} else {
		median.Sub = ctx.Locale.TrString("asc_trust.checks.stat_no_durations")
	}
	return []ascStatCard{rate, open, flaky, median}
}

func ascChecksURL(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/asc/checks"
}

// ascChecksViewURL is the link of a tab; the window carries over from tab to tab.
func ascChecksViewURL(base, view string, window uint32, windowGiven bool) string {
	q := url.Values{}
	if view != ascChecksViewFailures {
		q.Set("view", view)
	}
	if windowGiven {
		q.Set("window", strconv.FormatUint(uint64(window), 10))
	}
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}

// AscChecks serves GET /{owner}/{repo}/asc/checks.
func AscChecks(ctx *context.Context) {
	repo, name, ok := trustweb.ReaderRepo(ctx)
	if !ok {
		return
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.checks.title")
	ctx.Data["PageIsAscChecks"] = true
	p := &ascChecksPage{
		ChecksURL: ascChecksURL(repo), View: ctx.FormString("view"), State: ctx.FormString("state"), Flaky: ctx.FormBool("flaky"),
		Job: ctx.FormString("job"), Test: ctx.FormString("test"),
	}
	p.CanWrite = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode)
	p.CanRerun = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeActions)
	ctx.Data["AscChecks"] = p

	if p.View == "" {
		p.View = ascChecksViewFailures
	}
	window, wok := asctrust.WindowDays(ctx.FormString("window"))
	_, windowGiven := ctx.Req.Form["window"]
	switch p.View {
	case ascChecksViewFailures, ascChecksViewFlaky, ascChecksViewTrends:
	default:
		wok = false
	}
	for _, v := range []string{ascChecksViewFailures, ascChecksViewFlaky, ascChecksViewTrends} {
		p.Views = append(p.Views, ascChecksView{Key: v, Active: v == p.View, URL: ascChecksViewURL(p.ChecksURL, v, window, windowGiven && wok)})
	}
	if !wok {
		p.Problem = ctx.Locale.TrString("asc_trust.checks.err_window")
		ctx.HTML(http.StatusBadRequest, tplAscChecks)
		return
	}
	p.Window = window
	if !asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscChecks)
		return
	}
	p.WindowLinks = ascChecksWindowLinks(p.ChecksURL, ctx.Req.URL.Query(), window)
	// The tab of the default view (no state in the address) is the "open" tab, not a tab of its own: an empty
	// key made its label the half key "asc_trust.checks.state." and listed "Open" twice.
	for _, s := range ascGroupStateOptions() {
		p.States = append(p.States, ascChecksState{Key: s.Key, Label: s.Key, Active: s.Key == p.State || (p.State == "" && s.Key == "open")})
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc checks: no connection to asc serve: %v", err)
		p.Problem, p.Retry = ctx.Locale.TrString("asc_trust.err.unavailable"), true
		ctx.HTML(http.StatusBadGateway, tplAscChecks)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("checks credential", err)
		p.Problem, p.Retry = f.Localized(ctx), true
		ctx.HTML(f.Status, tplAscChecks)
		return
	}
	defer release()
	fail := func(op string, err error) {
		f := trustweb.FailureOf(op, err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc checks", nil)
			return
		}
		p.Problem, p.Retry, p.Stats = f.Localized(ctx), true, nil
		ctx.HTML(f.Status, tplAscChecks)
	}
	// The stat cards are advice: an asc that cannot count them (or predates Ci.Stats) leaves them out.
	if stats, err := asctrust.LoadCiStats(callCtx, conn, name, window); err != nil {
		log.Warn("asc checks: statistics of %s: %v", repo.FullName(), err)
	} else {
		p.Stats = ascChecksStats(ctx, stats)
	}
	switch p.View {
	case ascChecksViewFlaky:
		page, err := asctrust.LoadFlaky(callCtx, conn, name, window, ctx.FormString("page"))
		if err != nil {
			fail("checks flaky", err)
			return
		}
		p.FlakyRows = page.Rows
		if page.Next != "" {
			p.NextURL = p.ChecksURL + "?" + url.Values{"view": {ascChecksViewFlaky}, "window": {strconv.FormatUint(uint64(window), 10)}, "page": {page.Next}}.Encode()
		}
	case ascChecksViewTrends:
		trends, err := asctrust.LoadTrends(callCtx, conn, name, window)
		if err != nil {
			fail("checks trends", err)
			return
		}
		p.Trends = trends
	default:
		query := asctrust.ChecksQuery{
			State: p.State, FlakyOnly: p.Flaky, Job: p.Job, Test: p.Test, PageToken: ctx.FormString("page"), SkipFlaky: true,
		}
		data, err := asctrust.LoadChecks(callCtx, conn, name, query, window)
		if err != nil {
			fail("checks load", err)
			return
		}
		p.Data = data
		// The counts on the state tabs and the side cards are advice: a failure leaves them out.
		if side, err := asctrust.LoadChecksSide(callCtx, conn, name, query); err != nil {
			log.Warn("asc checks: state counts of %s: %v", repo.FullName(), err)
		} else {
			p.Side = side
			for i := range p.States {
				if c, ok := side.Counts[p.States[i].Key]; ok {
					p.States[i].Count = ascCountLabel(c)
				}
			}
		}
		p.Main = ascMainState(ctx, repo)
		p.Jobs = asctrust.JobStats(data.Groups.Rows)
		list := url.Values{}
		if p.State != "" {
			list.Set("state", p.State)
		}
		if p.Flaky {
			list.Set("flaky", "1")
		}
		if p.Job != "" {
			list.Set("job", p.Job)
		}
		if p.Test != "" {
			list.Set("test", p.Test)
		}
		p.Filtered = p.Flaky || p.Job != "" || p.Test != ""
		p.TrendsURL = ascChecksViewURL(p.ChecksURL, ascChecksViewTrends, window, windowGiven)
		if data.Groups.Next != "" {
			q := url.Values{"page": {data.Groups.Next}}
			for k, v := range list {
				q[k] = v
			}
			p.NextURL = p.ChecksURL + "?" + q.Encode()
		}
		if page := ctx.FormString("page"); page != "" {
			list.Set("page", page)
		}
		p.ListQuery = list.Encode()
		ascChecksRows(ctx, repo, callCtx, conn, name, p, data.Groups.Rows, list)
	}
	ctx.HTML(http.StatusOK, tplAscChecks)
}

// ascChecksRows builds the rows of the failure groups and the expanded group: the one named by ?open=, else the
// newest group of the first page; ?open=0 shows none. A failure to read the expanded group leaves it closed.
func ascChecksRows(ctx *context.Context, repo *repo_model.Repository, callCtx gocontext.Context, conn grpc.ClientConnInterface, name string, p *ascChecksPage, rows []asctrust.GroupRow, list url.Values) {
	if len(rows) == 0 {
		return
	}
	if p.CanWrite {
		p.AllowQuarantine = asctrust.QuarantineAllowed(callCtx, conn, name)
	}
	var open uint64
	if raw, given := ctx.Req.Form["open"]; given {
		open, _ = strconv.ParseUint(raw[0], 10, 64)
	} else if list.Get("page") == "" {
		open = rows[0].ID
	}
	act := ctx.FormString("act")
	if act != "acknowledged" && act != "quarantined" {
		act = ""
	}
	link := func(id uint64, a, anchor string) string {
		q := url.Values{}
		for k, v := range list {
			q[k] = v
		}
		q.Set("open", strconv.FormatUint(id, 10))
		if a != "" {
			q.Set("act", a)
		}
		return p.ChecksURL + "?" + q.Encode() + anchor
	}
	for _, g := range rows {
		row := ascGroupRowView{GroupRow: g, Expanded: g.ID == open}
		id := strconv.FormatUint(g.ID, 10)
		if row.Expanded {
			row.ToggleURL = link(0, "", "#asc-group-"+id)
		} else {
			row.ToggleURL = link(g.ID, "", "#asc-group-"+id)
		}
		if p.CanWrite {
			row.Op = asctrust.NewOpID()
			if g.StateKey != "acknowledged" {
				row.AckURL = link(g.ID, "acknowledged", "#asc-group-"+id+"-note")
			}
			if p.AllowQuarantine && g.StateKey != "quarantined" {
				row.QuarantineURL = link(g.ID, "quarantined", "#asc-group-"+id+"-note")
			}
			row.CanResolve = g.StateKey != "resolved"
			row.CanReopen = g.StateKey != "open"
		}
		p.Rows = append(p.Rows, row)
	}
	if open == 0 {
		return
	}
	for i := range p.Rows {
		if p.Rows[i].ID != open {
			continue
		}
		detail, err := asctrust.LoadGroupDetail(callCtx, conn, name, open, "", ascOpenOccurrences+1)
		if err != nil {
			log.Warn("asc checks: group %d of %s: %v", open, repo.FullName(), err)
			p.Rows[i].Expanded = false
			p.Rows[i].ToggleURL = link(open, "", "#asc-group-"+strconv.FormatUint(open, 10))
			return
		}
		occ := detail.Occurrences
		o := &ascGroupOpen{ID: open, More: len(occ) > ascOpenOccurrences || detail.Next != ""}
		if len(occ) > ascOpenOccurrences {
			occ = occ[:ascOpenOccurrences]
		}
		o.Occurrences = ascOccurrenceViews(ctx, repo, occ)
		for j := range o.Occurrences {
			if o.Occurrences[j].JobName == "" {
				o.Occurrences[j].JobName = detail.Group.JobName
			}
		}
		for j := range o.Occurrences {
			if lines := ascLogLines(o.Occurrences[j].Message, ascOpenLogLines); lines != nil {
				o.Lines, o.LogOf = lines, &o.Occurrences[j]
				break
			}
		}
		if p.CanWrite {
			o.Op, o.Act = p.Rows[i].Op, act
			if act == "quarantined" && !p.AllowQuarantine {
				o.Act = ""
			}
			o.IssueQuery = asctrust.GroupIssueQuery(ctx.Locale.TrString("asc_trust.checks.issue_title", firstNonEmptyString(detail.Group.TestName, detail.Group.Title)), ascGroupIssueBody(ctx, repo.OwnerName, repo.Name, detail.Group.ID))
		}
		p.Open = o
		return
	}
}

func ascGroupStateOptions() []struct {
	Key   string
	Label string
} {
	var out []struct {
		Key   string
		Label string
	}
	for _, s := range asctrust.GroupStates {
		out = append(out, struct {
			Key   string
			Label string
		}{s.Key, s.Label})
	}
	return out
}

// ascGroupPage is the template data under .AscCheckGroup.
type ascGroupPage struct {
	Detail      *asctrust.GroupDetail
	History     *asctrust.History
	Occurrences []ascOccurrenceView
	Op          string
	Problem     string
	Unsupported bool
	ChecksURL   string
	GroupURL    string
	NextURL     string
	CanWrite    bool
	CanRerun    bool
	// AllowQuarantine is the policy flag [ci] allow_quarantine; without it the quarantine button is not offered.
	AllowQuarantine bool
	Window          uint32
	IssueQuery      template.URL
}

type ascOccurrenceView struct {
	asctrust.OccurrenceRow
	RunURL string
	RunID  int64
	// JobName is the name of the job of the run, "" when the run is not one of this repository.
	JobName string
}

// AscCheckGroup serves GET /{owner}/{repo}/asc/checks/groups/{id}.
func AscCheckGroup(ctx *context.Context) {
	repo, name, ok := trustweb.ReaderRepo(ctx)
	if !ok {
		return
	}
	id := ctx.ParamsInt64(":id")
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.checks.group_title")
	ctx.Data["PageIsAscChecks"] = true
	p := &ascGroupPage{ChecksURL: ascChecksURL(repo), Op: asctrust.NewOpID()}
	p.GroupURL = p.ChecksURL + "/groups/" + strconv.FormatInt(id, 10)
	p.CanWrite = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode)
	p.CanRerun = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeActions)
	ctx.Data["AscCheckGroup"] = p
	if id <= 0 {
		ctx.NotFound("asc check group", nil)
		return
	}
	window, wok := asctrust.WindowDays(ctx.FormString("window"))
	if !wok {
		p.Problem = ctx.Locale.TrString("asc_trust.checks.err_window")
		ctx.HTML(http.StatusBadRequest, tplAscCheckGroup)
		return
	}
	p.Window = window
	if !asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscCheckGroup)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscCheckGroup)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("checks credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscCheckGroup)
		return
	}
	defer release()
	detail, hist, err := asctrust.LoadGroup(callCtx, conn, name, uint64(id), ctx.FormString("page"), window)
	if err != nil {
		f := trustweb.FailureOf("checks group", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc check group", nil)
			return
		}
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscCheckGroup)
		return
	}
	p.Detail, p.History = detail, hist
	p.AllowQuarantine = p.CanWrite && asctrust.QuarantineAllowed(callCtx, conn, name)
	p.IssueQuery = asctrust.GroupIssueQuery(ctx.Locale.TrString("asc_trust.checks.issue_title", firstNonEmptyString(detail.Group.TestName, detail.Group.Title)), ascGroupIssueBody(ctx, repo.OwnerName, repo.Name, detail.Group.ID))
	p.Occurrences = ascOccurrenceViews(ctx, repo, detail.Occurrences)
	if detail.Next != "" {
		p.NextURL = p.GroupURL + "?" + url.Values{"page": {detail.Next}}.Encode()
	}
	ctx.HTML(http.StatusOK, tplAscCheckGroup)
}

func firstNonEmptyString(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}

// AscCheckGroupState serves POST /{owner}/{repo}/asc/checks/groups/{id}/state with the fields state, note and op.
func AscCheckGroupState(ctx *context.Context) {
	repo, name, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	id := ctx.ParamsInt64(":id")
	back := ascChecksURL(repo) + "/groups/" + strconv.FormatInt(id, 10)
	if id <= 0 {
		ctx.NotFound("asc check group", nil)
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("checks credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	if ctx.Req.PostFormValue("state") == "quarantined" && !asctrust.QuarantineAllowed(callCtx, conn, name) {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.checks.quarantine_not_allowed"))
		ctx.Redirect(back)
		return
	}
	g, err := asctrust.SetGroupState(callCtx, conn, name, uint64(id), ctx.Req.PostFormValue("state"), trustweb.Lines(ctx.Req.PostFormValue("note")), ctx.Req.PostFormValue("op"))
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("checks state", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_trust.checks.state_done." + g.StateKey))
	switch ctx.Req.PostFormValue("next") {
	case "pull":
		if index, err := strconv.ParseInt(ctx.Req.PostFormValue("index"), 10, 64); err == nil && index > 0 {
			ctx.Redirect(repo.Link() + "/pulls/" + strconv.FormatInt(index, 10) + "/asc/checks")
			return
		}
	case "checks":
		// Back to the list the form was on, with the group still open; only the keys of the list are kept.
		q := ascListBack(ctx.Req.PostFormValue("back"))
		q.Set("open", strconv.FormatInt(id, 10))
		ctx.Redirect(ascChecksURL(repo) + "?" + q.Encode() + "#asc-group-" + strconv.FormatInt(id, 10))
		return
	}
	ctx.Redirect(back)
}

// ascRerunMessage turns the error of a re-run into a sentence. known is false for an error the person cannot act on.
func ascRerunMessage(ctx *context.Context, err error) (msg string, known bool) {
	switch {
	case errors.Is(err, actions_service.ErrRerunWorkflowInvalid):
		return ctx.Locale.TrString("asc_trust.checks.rerun_invalid"), true
	case errors.Is(err, actions_service.ErrRerunWorkflowStillRunning), errors.Is(err, actions_service.ErrRerunJobStillRunning):
		return ctx.Locale.TrString("asc_trust.checks.rerun_running"), true
	case errors.Is(err, actions_service.ErrRerunWorkflowDisabled):
		return ctx.Locale.TrString("asc_trust.checks.rerun_disabled"), true
	}
	return "", false
}

// ascRerunRun re-runs one run: all of its jobs, or only the jobs that failed (and, as Forgejo does, the jobs that
// need them). It returns how many jobs were started.
func ascRerunRun(ctx *context.Context, run *actions_model.ActionRun, failedOnly bool) (int, error) {
	if !failedOnly {
		jobs, err := actions_service.RerunAllJobs(ctx, run)
		return len(jobs), err
	}
	all, err := actions_model.GetRunJobsByRunID(ctx, run.ID)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, job := range all {
		if !job.Status.IsFailure() {
			continue
		}
		jobs, err := actions_service.RerunJob(ctx, job)
		if errors.Is(err, actions_service.ErrRerunJobStillRunning) {
			continue // it was started again as a job that needs another one
		}
		if err != nil {
			return started, err
		}
		started += len(jobs)
	}
	return started, nil
}

// AscChecksRerun serves POST /{owner}/{repo}/asc/checks/rerun with the fields run (the id of an Actions run of this
// repository) and mode (all or failed).
func AscChecksRerun(ctx *context.Context) {
	repo, _, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	if !ctx.Repo.CanWrite(unit.TypeActions) {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	back := ascChecksURL(repo)
	runID, err := strconv.ParseInt(ctx.Req.PostFormValue("run"), 10, 64)
	if err != nil || runID <= 0 {
		ctx.NotFound("asc rerun", nil)
		return
	}
	run, err := actions_model.GetRunByID(ctx, runID)
	if err != nil || run.RepoID != repo.ID {
		ctx.NotFound("asc rerun", nil) // a run of another repository looks like a run that does not exist
		return
	}
	failedOnly := ctx.Req.PostFormValue("mode") == "failed"
	n, err := ascRerunRun(ctx, run, failedOnly)
	if msg, known := ascRerunMessage(ctx, err); known {
		ctx.Flash.Error(msg)
		ctx.Redirect(back)
		return
	} else if err != nil {
		log.Error("asc checks: re-run of run %d: %v", run.ID, err)
		ctx.ServerError("AscChecksRerun", err)
		return
	}
	if n == 0 {
		ctx.Flash.Info(ctx.Locale.TrString("asc_trust.checks.rerun_nothing"))
		ctx.Redirect(back)
		return
	}
	ctx.Redirect(repo.Link() + "/actions/runs/" + strconv.FormatInt(run.Index, 10))
}

// ascRerunFailedScan is how many of the newest runs of the repository the "Re-run failed" button looks at, and
// ascRerunFailedMax how many runs it starts again at most.
const (
	ascRerunFailedScan = 100
	ascRerunFailedMax  = 10
)

// ascLatestFailedRuns returns, newest first, the runs of runs that are the latest of their workflow on their ref and
// failed: a failure that a later run of the same workflow on the same ref has not already answered.
func ascLatestFailedRuns(runs []*actions_model.ActionRun, limit int) []*actions_model.ActionRun {
	seen := map[string]bool{}
	var out []*actions_model.ActionRun
	for _, run := range runs {
		key := run.WorkflowID + "|" + run.Ref
		if seen[key] {
			continue
		}
		seen[key] = true
		if run.Status.IsFailure() {
			out = append(out, run)
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// AscChecksRerunFailed serves POST /{owner}/{repo}/asc/checks/rerun_failed: it starts again the failed jobs of the
// latest failed run of each workflow and ref (at most ascRerunFailedMax runs). Whoever may write Actions may do it.
func AscChecksRerunFailed(ctx *context.Context) {
	repo, _, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	if !ctx.Repo.CanWrite(unit.TypeActions) {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	back := ascChecksURL(repo)
	runs, err := db.Find[actions_model.ActionRun](ctx, actions_model.FindRunOptions{RepoID: repo.ID, ListOptions: db.ListOptions{Page: 1, PageSize: ascRerunFailedScan}})
	if err != nil {
		ctx.ServerError("FindRuns", err)
		return
	}
	started := 0
	var firstErr error
	for _, run := range ascLatestFailedRuns(runs, ascRerunFailedMax) {
		n, err := ascRerunRun(ctx, run, true)
		started += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		if msg, known := ascRerunMessage(ctx, firstErr); known {
			ctx.Flash.Error(msg)
		} else {
			log.Error("asc checks: re-run of the failed runs of %s: %v", repo.FullName(), firstErr)
			ctx.Flash.Error(ctx.Locale.TrString("asc_trust.checks.rerun_some_failed"))
		}
	}
	if started > 0 {
		ctx.Flash.Success(ctx.Locale.TrString("asc_trust.checks.rerun_started", started))
	} else if firstErr == nil {
		ctx.Flash.Info(ctx.Locale.TrString("asc_trust.checks.rerun_nothing"))
	}
	ctx.Redirect(back)
}

// ------------------------------------------------------------------------------------------------ one version

// ascPullChecksPage is the template data under .AscPullChecks.
type ascPullChecksPage struct {
	Checks      *asctrust.PullChecks
	Problem     string
	CausesNote  string
	PullURL     string
	ChecksURL   string
	RerunURL    string
	NewIssueURL string
	CanRerun    bool
	CanWrite    bool
	// AllowQuarantine is true when the viewer may write and the policy allows quarantine ([ci] allow_quarantine).
	AllowQuarantine bool
	// Repro is the asc command that checks out this version, for "Copy a repro command"; "" when the head is unknown.
	Repro string
	// Version is the version the checks are of (0: not known), for "Run on v4".
	Version uint32
	Ops     map[uint64]string
	Index   int64
	// History is what each version of the change said, newest version first (nil: not known).
	History []AscVersionHistoryRow
	// Asks maps the group of a failing check to the address of the agent form filled in for it; empty when the viewer may
	// not start an agent run.
	Asks map[uint64]string
	// VsMain is how the newest finished run of the change compares with main (nil: not known); MainCommit is the commit
	// of the base branch the statuses of main are of ("" when not known).
	VsMain     *ascVsMain
	MainCommit string
}

// ascVsMain is the comparison with main as the card shows it.
type ascVsMain struct {
	*asctrust.MainCompare
	Took string
	// Was is the time of the slowest test on main ("" when main has no such test); Slower is true when it is slower now.
	Was    string
	Slower bool
}

func ascVsMainOf(c *asctrust.MainCompare) *ascVsMain {
	if c == nil {
		return nil
	}
	v := &ascVsMain{MainCompare: c}
	if c.SlowestTest != "" {
		v.Took = asctrust.MillisText(c.SlowestMs)
		if c.SlowestMainMs > 0 {
			v.Was, v.Slower = asctrust.MillisText(c.SlowestMainMs), c.SlowestMs > c.SlowestMainMs
		}
	}
	return v
}

// AscPullChecks serves GET /{owner}/{repo}/pulls/{index}/asc/checks.
func AscPullChecks(ctx *context.Context) {
	repo, name, ok := trustweb.ReaderRepo(ctx)
	if !ok {
		return
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	pull := issue.PullRequest
	ctx.Data["PageIsPullList"] = true
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.checks.title")
	p := &ascPullChecksPage{
		PullURL: issue.Link(), ChecksURL: ascChecksURL(repo), Ops: map[uint64]string{}, Index: issue.Index,
		CanRerun: ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeActions),
		CanWrite: ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode),
	}
	p.RerunURL = p.PullURL + "/asc/checks/rerun"
	p.NewIssueURL = repo.Link() + "/issues/new"
	ctx.Data["AscPullChecks"] = p

	sha, headOK := ascPullHead(ctx, pull)
	if !headOK {
		p.Problem = ctx.Locale.TrString("asc_trust.checks.err_head_page")
		ctx.HTML(http.StatusOK, tplAscPullChecks)
		return
	}
	in := asctrust.PullChecksIn{Head: sha, Now: time.Now()}
	p.Repro = ascReproCommand(repo.FullName(), sha)

	// The version of the change that the head is.
	if versions, err := pull_service.AscVersions(ctx, pull); err == nil {
		in.Version = versions.ByGitCommit(sha)
		p.Version = in.Version
	}

	statuses, _, err := git_model.GetLatestCommitStatus(ctx, repo.ID, sha, db.ListOptionsAll)
	if err != nil {
		ctx.ServerError("GetLatestCommitStatus", err)
		return
	}
	runs, err := db.Find[actions_model.ActionRun](ctx, actions_model.FindRunOptions{RepoID: repo.ID, CommitSHA: sha})
	if err != nil {
		ctx.ServerError("FindRuns", err)
		return
	}
	for _, s := range statuses {
		in.Statuses = append(in.Statuses, asctrust.StatusIn{
			Context: s.Context, State: string(s.State), Description: s.Description, URL: s.TargetURL,
			RunID: ascStatusRunID(s.TargetURL, runs),
		})
	}
	runIDs := map[uint64]bool{}
	var earliest time.Time
	for _, r := range runs {
		in.Runs = append(in.Runs, ascRunIn(repo, r))
		runIDs[uint64(r.ID)] = true
		if c := r.Created.AsTime(); earliest.IsZero() || c.Before(earliest) {
			earliest = c
		}
	}

	// The base branch, for "compared with main".
	if ctx.Repo.GitRepo != nil {
		if baseSHA, err := ctx.Repo.GitRepo.GetBranchCommitID(pull.BaseBranch); err == nil {
			p.MainCommit = baseSHA
			if base, _, err := git_model.GetLatestCommitStatus(ctx, repo.ID, baseSHA, db.ListOptionsAll); err == nil {
				in.MainStatuses = []asctrust.StatusIn{}
				for _, s := range base {
					in.MainStatuses = append(in.MainStatuses, asctrust.StatusIn{Context: s.Context, State: string(s.State)})
				}
			}
		}
	}

	// What the policy requires, and the failure groups seen in these runs: advice, so a failure of either leaves
	// the page without it and says so.
	if asctrust.HasFeature(ctx, asctrust.FeaturePolicy) || asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		ascPullAdvice(ctx, repo, name, &in, p, runIDs, earliest)
	}
	if ctx.IsSigned && ctx.Repo.CanRead(unit.TypeActions) {
		if runs := ascChangeRuns(ctx, pull); runs != nil {
			p.History = ascVersionHistoryOf(runs)
		}
		p.VsMain = ascVsMainOf(ascPullVsMain(ctx, pull))
	}
	p.Checks = asctrust.BuildPullChecks(in)
	p.Checks.Localize(ctx.Locale.TrString)
	p.Asks = ascAskLinks(ctx, repo, pull, p.Checks)
	if p.CanWrite && len(p.Checks.Flaky) > 0 {
		// Quarantine is offered only where the policy allows it; the state handler asks the policy again.
		if conn, err := asctrust.Conn(); err == nil {
			if callCtx, release, err := trustweb.ReadCall(ctx, repo); err == nil {
				p.AllowQuarantine = asctrust.QuarantineAllowed(callCtx, conn, name)
				release()
			}
		}
	}
	for i, card := range p.Checks.Flaky {
		p.Ops[card.GroupID] = asctrust.NewOpID()
		p.Checks.Flaky[i].IssueQuery = asctrust.GroupIssueQuery(ctx.Locale.TrString("asc_trust.pull.flaky_issue_title", card.Test), ascGroupIssueBody(ctx, repo.OwnerName, repo.Name, card.GroupID))
	}
	ctx.HTML(http.StatusOK, tplAscPullChecks)
}

// ascAskLinks builds, for each failing cause of the checks, the link to the agent form with the failure as the task.
// The form checks everything again when it is sent; the link starts nothing.
func ascAskLinks(ctx *context.Context, repo *repo_model.Repository, pull *issues_model.PullRequest, checks *asctrust.PullChecks) map[uint64]string {
	if checks == nil || len(checks.Causes) == 0 || !setting.Asc.Enabled || ctx.Doer == nil || repo.IsArchived ||
		repo.GetStorageBackend() != "asc" || !ctx.Repo.CanWrite(unit.TypeCode) || !ascagentui.FeaturesEnabled(ctx, ctx.Doer) {
		return nil
	}
	if on, err := repo_model.AscFeatureEnabled(ctx, repo.ID, repo_model.AscFeatureAgents); err != nil || !on {
		return nil
	}
	out := map[uint64]string{}
	for _, c := range checks.Causes {
		if c.GroupID == 0 {
			continue
		}
		task := ascAskTask(pull.Index, checks.Version, c.Title, c.Excerpt)
		q := url.Values{"title": {ascAskTitle(c.Title)}, "task": {task}, "target": {pull.HeadBranch}}
		out[c.GroupID] = repo.Link() + "/asc/agents/new?" + q.Encode()
	}
	return out
}

// ascAskTitle is the title of the run asked for: "Fix: " and the cause, cut to fit.
func ascAskTitle(cause string) string {
	t := "Fix: " + strings.Join(strings.Fields(cause), " ")
	if len(t) > ascagentui.MaxTitleBytes {
		t = strings.ToValidUTF8(t[:ascagentui.MaxTitleBytes], "")
	}
	return t
}

// ascAskTask is the text the agent starts from: which change, which version, what failed and the log excerpt.
func ascAskTask(index int64, version uint32, cause, excerpt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A check fails on change #%d", index)
	if version > 0 {
		fmt.Fprintf(&b, " (version %d)", version)
	}
	b.WriteString(". Find the cause and fix it.\n\nFailure: " + cause)
	if excerpt != "" {
		b.WriteString("\n\nLog excerpt:\n" + excerpt)
	}
	return b.String()
}

var ascStatusRunRe = regexp.MustCompile(`/actions/runs/(\d+)(?:/|$)`)

// ascStatusRunID returns the id of the run in runs that a commit status points at (its target URL is
// .../actions/runs/{index}/jobs/{n}), 0 when the status belongs to none.
func ascStatusRunID(targetURL string, runs []*actions_model.ActionRun) int64 {
	m := ascStatusRunRe.FindStringSubmatch(targetURL)
	if m == nil {
		return 0
	}
	index, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	for _, r := range runs {
		if r.Index == index {
			return r.ID
		}
	}
	return 0
}

// ascReproCommand is the asc command that checks out the exact version a check ran on, in the directory repro. It names
// no git command: a person without the repository runs it, then the failing test.
func ascReproCommand(repoFullName, sha string) string {
	if repoFullName == "" || sha == "" {
		return ""
	}
	return "asc ci checkout --repo " + repoFullName + " --rev " + sha + " --dir repro && cd repro"
}

// ascPullAdvice adds what the policy requires and the failure groups seen in the runs to in. The reads are made as
// the signed-in person. A failure of either leaves the page without it and says so.
func ascPullAdvice(ctx *context.Context, repo *repo_model.Repository, name string, in *asctrust.PullChecksIn, p *ascPullChecksPage, runIDs map[uint64]bool, earliest time.Time) {
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		log.Warn("asc checks: credential for %s: %v", repo.FullName(), err)
		p.CausesNote = ctx.Locale.TrString("asc_trust.checks.err_causes")
		return
	}
	defer release()
	if required, err := asctrust.RequiredChecks(callCtx, conn, name); err == nil {
		in.Required = required
	} else {
		log.Warn("asc checks: required checks of %s: %v", repo.FullName(), err)
	}
	if asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		causes, err := asctrust.PullCauses(callCtx, conn, name, runIDs, earliest)
		if err != nil {
			log.Warn("asc checks: failure groups of %s: %v", repo.FullName(), err)
			p.CausesNote = ctx.Locale.TrString("asc_trust.checks.err_causes")
			return
		}
		in.Causes, in.CausesKnown = causes, true
	}
}

func ascRunIn(repo *repo_model.Repository, r *actions_model.ActionRun) asctrust.RunIn {
	in := asctrust.RunIn{
		ID: r.ID, Title: r.Title, URL: repo.Link() + "/actions/runs/" + strconv.FormatInt(r.Index, 10),
		Created: r.Created.AsTime(),
	}
	if r.Started > 0 {
		in.Started = r.Started.AsTime()
	}
	if r.Stopped > 0 {
		in.Stopped = r.Stopped.AsTime()
	}
	switch {
	case r.Status.IsSuccess():
		in.State = "success"
	case r.Status.IsFailure():
		in.State = "failure"
	case r.Status.IsRunning():
		in.State = "running"
	case r.Status.IsWaiting():
		in.State = "waiting"
	case r.Status.IsBlocked():
		in.State = "blocked"
	case r.Status.IsCancelled():
		in.State = "cancelled"
	case r.Status.IsSkipped():
		in.State = "skipped"
	}
	return in
}

// ascPullHead returns the commit the head of a pull request is at.
func ascPullHead(ctx *context.Context, pull *issues_model.PullRequest) (string, bool) {
	if ctx.Repo.GitRepo != nil {
		if sha, err := ctx.Repo.GitRepo.GetRefCommitID(pull.GetGitRefName()); err == nil && sha != "" {
			return sha, true
		}
	}
	if pull.HeadCommitID != "" {
		return pull.HeadCommitID, true
	}
	return "", false
}

// AscPullChecksRerun serves POST /{owner}/{repo}/pulls/{index}/asc/checks/rerun with the field mode (all or failed).
// The runs are the ones of the head commit of the pull request, found here: the request names no run.
func AscPullChecksRerun(ctx *context.Context) {
	repo, _, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	if !ctx.Repo.CanWrite(unit.TypeActions) {
		ctx.Error(http.StatusForbidden)
		return
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	back := issue.Link() + "/asc/checks"
	sha, headOK := ascPullHead(ctx, issue.PullRequest)
	if !headOK {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.checks.err_head"))
		ctx.Redirect(back)
		return
	}
	runs, err := db.Find[actions_model.ActionRun](ctx, actions_model.FindRunOptions{RepoID: repo.ID, CommitSHA: sha})
	if err != nil {
		ctx.ServerError("FindRuns", err)
		return
	}
	failedOnly := ctx.Req.PostFormValue("mode") == "failed"
	// "Run on v4" on one row names the run; it must be one of the runs of this head commit.
	if raw := ctx.Req.PostFormValue("run"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		var only []*actions_model.ActionRun
		for _, run := range runs {
			if err == nil && run.ID == id {
				only = append(only, run)
			}
		}
		if len(only) == 0 {
			ctx.NotFound("asc pull rerun", nil)
			return
		}
		runs = only
	}
	started := 0
	var firstErr error
	for _, run := range runs {
		if failedOnly && !run.Status.IsFailure() {
			continue
		}
		n, err := ascRerunRun(ctx, run, failedOnly)
		started += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		if msg, known := ascRerunMessage(ctx, firstErr); known {
			ctx.Flash.Error(msg)
		} else {
			log.Error("asc checks: re-run for pull request %d: %v", issue.PullRequest.ID, firstErr)
			ctx.Flash.Error(ctx.Locale.TrString("asc_trust.checks.rerun_some_failed"))
		}
	}
	if started > 0 {
		ctx.Flash.Success(ctx.Locale.TrString("asc_trust.checks.rerun_started", started))
	} else if firstErr == nil {
		ctx.Flash.Info(ctx.Locale.TrString("asc_trust.checks.rerun_nothing"))
	}
	ctx.Redirect(back)
}
