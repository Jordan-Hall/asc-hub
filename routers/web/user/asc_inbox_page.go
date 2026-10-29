// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// The inbox page after the design board "Inbox: whose turn": tabs (needs you, waiting on others, drafts, agents,
// done), a list of what is in the tab and, beside it, the selected item with why it waits for the person, what
// was said and the ways to answer. The person is always the signed-in doer: no value of the request names
// another user, a query value only narrows what is shown, and the marks (pin, snooze) are written for the
// doer after the pull request was found in the doer's own inbox.

const (
	ascInboxTabNeeds   = "needs"
	ascInboxTabWaiting = "waiting"
	ascInboxTabDrafts  = "drafts"
	ascInboxTabAgents  = "agents"
	ascInboxTabDone    = "done"

	// ascInboxInfoRows is how many rows of the list are enriched (stack place, checks, size): each costs git work.
	ascInboxInfoRows = 20
	// ascInboxThreadMax is how many comments the detail pane shows, ascInboxCommentMax how long one may be.
	ascInboxThreadMax  = 4
	ascInboxCommentMax = 800
	// ascInboxAgentRepos, ascInboxAgentPerRepo and ascInboxAgentMax bound the agent runs asked for.
	ascInboxAgentRepos   = 30
	ascInboxAgentPerRepo = 5
	ascInboxAgentMax     = 20
)

var ascInboxTabOrder = []string{ascInboxTabNeeds, ascInboxTabWaiting, ascInboxTabDrafts, ascInboxTabAgents, ascInboxTabDone}

// ascInboxTabSections says which sections a tab lists.
var ascInboxTabSections = map[string][]issues_model.AscInboxSection{
	ascInboxTabNeeds:   {issues_model.AscInboxNeedsReview, issues_model.AscInboxReturned, issues_model.AscInboxReady, issues_model.AscInboxBlocked},
	ascInboxTabWaiting: {issues_model.AscInboxWaiting, issues_model.AscInboxQueued},
	ascInboxTabDrafts:  {issues_model.AscInboxDrafts},
	ascInboxTabDone:    {issues_model.AscInboxMerged},
}

// ascInboxAgentsLoad returns the person's active agent runs. Tests replace it.
var ascInboxAgentsLoad = func(ctx *context.Context, doer *user_model.User) ([]AscDashAgent, int, error) {
	return ascAgentsActive(ctx, doer, ascInboxAgentRepos, ascInboxAgentPerRepo, ascInboxAgentMax)
}

// ascInboxCommentsLoad returns the latest comments of a pull request, oldest first. Tests replace it.
var ascInboxCommentsLoad = ascInboxCommentsFromModels

// AscInboxTab is one tab of the page.
type AscInboxTab struct {
	Key    string
	Label  string
	Count  int
	Active bool
	URL    string
}

// AscInboxRow is one item of the list: a pull request or an agent run waiting for an answer.
type AscInboxRow struct {
	Entry    *AscInboxEntry // nil for an agent run
	Agent    *AscDashAgent  // nil for a pull request
	Dash     AscDashRow
	Selected bool
	URL      string // the page with this row selected
	Day      string // "Today", "Yesterday" or the date: the heading the row sits under
}

// AscInboxComment is one comment of the thread.
type AscInboxComment struct {
	Poster  *user_model.User
	Body    string
	Created time.Time
}

// AscInboxDetail is the pane of the selected pull request.
type AscInboxDetail struct {
	Row       AscInboxRow
	Why       string
	Comments  []AscInboxComment
	CanReview bool   // approve and request changes: a person other than the author on a pull request open to review
	ReviewURL string // where the answer form posts
	CommitID  string
	FilesURL  string
	Pinned    bool
	Snoozed   bool
	Info      *AscInboxInfo
	Return    string // the tab, kept after a mark
}

// AscInboxPage is everything the template shows.
type AscInboxPage struct {
	Tab      string
	Tabs     []AscInboxTab
	Repo     string
	Repos    []string
	Kind     string // "", "humans" or "agents"
	Sort     string // "", "oldest" or "repo"
	Rows     []AscInboxRow
	Detail   *AscInboxDetail
	AgentRow *AscDashAgent // the selected agent run on the agents tab
	Snoozed  int
	Total    int
	NextURL  string // the first row that is not selected, for the phone's "open next" button
	Return   string
	Explicit bool // the person picked a row (the address names a listed pull request): a phone then shows its pane instead of the list
}

// ascInboxDay names the day of t for the headings of the list: today, yesterday or the date.
func ascInboxDay(l translation.Locale, t, now time.Time) string {
	loc := setting.DefaultUILocation
	t, now = t.In(loc), now.In(loc)
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, loc)
	switch {
	case !t.Before(today):
		return l.TrString("asc_work.inbox_today")
	case !t.Before(today.AddDate(0, 0, -1)):
		return l.TrString("asc_work.inbox_yesterday")
	}
	return t.Format("2 Jan 2006")
}

// ascInboxURL builds the address of the page with the given parts.
func ascInboxURL(tab, repo string, pull int64) string {
	return ascInboxURLF(tab, repo, pull, "", "")
}

// ascInboxURLF is ascInboxURL that keeps the filters kind and sort.
func ascInboxURLF(tab, repo string, pull int64, kind, sort string) string {
	q := url.Values{}
	if tab != "" && tab != ascInboxTabNeeds {
		q.Set("tab", tab)
	}
	if repo != "" {
		q.Set("repo", repo)
	}
	if pull > 0 {
		q.Set("pull", strconv.FormatInt(pull, 10))
	}
	if kind != "" {
		q.Set("kind", kind)
	}
	if sort != "" {
		q.Set("sort", sort)
	}
	u := setting.AppSubURL + "/-/asc/inbox"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// ascInboxTabOf returns the tab named by a query value; an unknown name is the first tab.
func ascInboxTabOf(s string) string {
	if slices.Contains(ascInboxTabOrder, s) {
		return s
	}
	return ascInboxTabNeeds
}

func ascInboxTabLabel(l translation.Locale, tab string) string {
	return l.TrString("user.asc_inbox.tab." + tab)
}

// ascInboxBuild assembles the page of doer for the query values tab, repo and pull.
func ascInboxBuild(ctx *context.Context, now time.Time, tabName, repoFilter string, pullID int64) (*AscInboxPage, error) {
	doer := ctx.Doer
	l := ctx.Locale
	tab := ascInboxTabOf(tabName)
	entries, err := ascInboxLoad(ctx, doer, now)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.PullID)
	}
	states, err := issues_model.ListAscInboxStates(ctx, doer.ID, ids)
	if err != nil {
		return nil, err
	}
	groups, snoozed := ascInboxGroup(entries, states, now)

	p := &AscInboxPage{Tab: tab, Snoozed: snoozed, Return: tab}
	p.Kind, p.Sort = ascInboxFilterOf(ctx.FormString("kind"), []string{"humans", "agents"}), ascInboxFilterOf(ctx.FormString("sort"), []string{"oldest", "repo"})
	repoSet := map[string]bool{}
	counts := map[string]int{}
	var inTab []*AscInboxEntry
	for _, g := range groups {
		for tname, secs := range ascInboxTabSections {
			if !slices.Contains(secs, g.Section) {
				continue
			}
			counts[tname] += len(g.Entries)
			if tname == tab {
				for _, e := range g.Entries {
					repoSet[e.RepoFullName] = true
				}
				inTab = append(inTab, g.Entries...)
			}
		}
	}
	// a repository filter only narrows: it must name a repository that has an item in the tab
	if repoFilter != "" && repoSet[repoFilter] {
		p.Repo = repoFilter
	}
	for r := range repoSet {
		p.Repos = append(p.Repos, r)
	}
	slices.Sort(p.Repos)

	var agents []AscDashAgent
	waitingAgents := 0
	agentsOn := setting.Asc.Enabled && ascagentui.FeaturesEnabled(ctx, doer)
	if agentsOn {
		agents, waitingAgents, err = ascInboxAgentsLoad(ctx, doer)
		if err != nil {
			log.Warn("asc inbox: agents of user %d: %v", doer.ID, err)
		}
		counts[ascInboxTabAgents] = len(agents)
		counts[ascInboxTabNeeds] += waitingAgents
	}

	for _, key := range ascInboxTabOrder {
		if key == ascInboxTabAgents && !agentsOn {
			continue
		}
		if key == ascInboxTabDrafts && counts[key] == 0 && tab != key {
			continue
		}
		p.Tabs = append(p.Tabs, AscInboxTab{Key: key, Label: ascInboxTabLabel(l, key), Count: counts[key], Active: key == tab, URL: ascInboxURLF(key, p.Repo, 0, p.Kind, p.Sort)})
	}
	if tab == ascInboxTabAgents && !agentsOn {
		tab, p.Tab = ascInboxTabNeeds, ascInboxTabNeeds
	}

	if tab == ascInboxTabAgents {
		for i := range agents {
			a := agents[i]
			p.Rows = append(p.Rows, AscInboxRow{Agent: &a, Dash: ascAgentDashRow(l, &a), URL: a.Link})
		}
		p.Total = len(p.Rows)
		return p, nil
	}

	for _, e := range inTab {
		if p.Repo != "" && e.RepoFullName != p.Repo {
			continue
		}
		var info *AscInboxInfo
		if len(p.Rows) < ascInboxInfoRows {
			info = ascInboxInfoLoad(ctx, e)
		}
		p.Rows = append(p.Rows, AscInboxRow{Entry: e, Dash: ascDashRowFor(l, e, info), URL: ascInboxURLF(tab, p.Repo, e.PullID, p.Kind, p.Sort), Day: ascInboxDay(l, e.Updated, now)})
	}
	if tab == ascInboxTabNeeds && agentsOn {
		for i := range agents {
			if agents[i].State.Slug != "waiting" || p.Repo != "" {
				continue
			}
			a := agents[i]
			p.Rows = append(p.Rows, AscInboxRow{Agent: &a, Dash: ascAgentDashRow(l, &a), URL: a.Link})
		}
	}
	p.Rows = ascInboxFilterRows(p.Rows, p.Kind, p.Sort)
	p.Total = len(p.Rows)

	// the selected row: the one asked for when it is listed, else the first pull request
	sel := -1
	for i, r := range p.Rows {
		if r.Entry != nil && r.Entry.PullID == pullID && pullID > 0 {
			sel = i
			p.Explicit = true
			break
		}
	}
	if sel < 0 {
		for i, r := range p.Rows {
			if r.Entry != nil {
				sel = i
				break
			}
		}
	}
	if sel >= 0 {
		p.Rows[sel].Selected = true
		p.Detail = ascInboxDetailFor(ctx, p.Rows[sel], tab)
		for i, r := range p.Rows {
			if i != sel && r.Entry != nil {
				p.NextURL = r.URL
				break
			}
		}
	}
	return p, nil
}

// ascAgentDashRow draws an agent run as a row.
func ascAgentDashRow(l translation.Locale, a *AscDashAgent) AscDashRow {
	row := AscDashRow{Title: a.Agent + " · " + a.Title, Where: a.Repo, Link: a.Link, Tone: "violet"}
	if a.State.Slug == "waiting" {
		row.Tone = "red"
		row.Chips = []AscDashChip{{"red", l.TrString("user.asc_dash.paused")}}
	} else {
		row.Chips = []AscDashChip{{"violet", a.State.Label}}
	}
	if a.Meter != "" {
		row.Meta = a.Meter
	}
	return row
}

// ascInboxDetailFor builds the pane of a selected pull request row.
func ascInboxDetailFor(ctx *context.Context, row AscInboxRow, tab string) *AscInboxDetail {
	e := row.Entry
	d := &AscInboxDetail{Row: row, Return: tab, Pinned: e.Pinned}
	d.Info = ascInboxInfoLoad(ctx, e)
	d.Row.Dash = ascDashRowFor(ctx.Locale, e, d.Info)
	if e.Section == issues_model.AscInboxNeedsReview {
		who := e.Author
		if who == "" {
			who = ctx.Locale.TrString("user.asc_inbox.someone")
		}
		d.Why = ctx.Locale.TrString("user.asc_inbox.why.needs_review", who)
	} else {
		d.Why = ctx.Locale.TrString("user.asc_inbox.why." + string(e.Section))
	}
	d.Comments = ascInboxCommentsLoad(ctx, e.PullID)
	link := strings.TrimSuffix(e.Link, "/")
	d.FilesURL = link + "/files"
	d.ReviewURL = link + "/files/reviews/submit"
	d.CanReview = e.Section == issues_model.AscInboxNeedsReview && e.Author != ctx.Doer.Name
	if d.CanReview {
		d.CommitID = ascInboxHeadOf(ctx, e.PullID)
	}
	return d
}

// ascInboxHeadOf returns the head commit of the pull request, or "".
func ascInboxHeadOf(ctx gocontext.Context, pullID int64) string {
	pr, err := issues_model.GetPullRequestByID(ctx, pullID)
	if err != nil || pr.LoadBaseRepo(ctx) != nil {
		return ""
	}
	return ascInboxHead(ctx, pr)
}

func ascInboxCommentsFromModels(ctx gocontext.Context, pullID int64) []AscInboxComment {
	pr, err := issues_model.GetPullRequestByID(ctx, pullID)
	if err != nil {
		return nil
	}
	list, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{
		IssueID:     pr.IssueID,
		Type:        issues_model.CommentTypeComment,
		ListOptions: db.ListOptions{Page: 1, PageSize: 100},
	})
	if err != nil {
		return nil
	}
	if len(list) > ascInboxThreadMax {
		list = list[len(list)-ascInboxThreadMax:]
	}
	var out []AscInboxComment
	for _, c := range list {
		if err := c.LoadPoster(ctx); err != nil || c.Poster == nil {
			continue
		}
		out = append(out, AscInboxComment{Poster: c.Poster, Body: ascInboxClip(c.Content, ascInboxCommentMax), Created: c.CreatedUnix.AsTime()})
	}
	return out
}

// ascInboxClip cuts s to at most max bytes at a character border and marks the cut.
func ascInboxClip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// AscInbox serves `GET /-/asc/inbox`.
func AscInbox(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscInbox", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("user.asc_inbox")
	ctx.Data["PageIsAscInbox"] = true

	pull, _ := strconv.ParseInt(ctx.FormString("pull"), 10, 64)
	page, err := ascInboxBuild(ctx, time.Now(), ctx.FormString("tab"), ctx.FormString("repo"), pull)
	if err != nil {
		ctx.ServerError("ascInboxBuild", err)
		return
	}
	ctx.Data["AscInbox"] = page
	ctx.Data["AscInboxSnoozed"] = page.Snoozed
	ctx.HTML(http.StatusOK, tplAscInbox)
}

// ascInboxSnoozeUntil turns the choice of the snooze menu into a time.
func ascInboxSnoozeUntil(choice string, now time.Time) (time.Time, bool) {
	switch choice {
	case "1h":
		return now.Add(time.Hour), true
	case "4h":
		return now.Add(4 * time.Hour), true
	case "tomorrow":
		t := now.In(setting.DefaultUILocation).AddDate(0, 0, 1)
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, t.Location()), true
	case "week":
		return now.AddDate(0, 0, 7), true
	}
	return time.Time{}, false
}

// AscInboxStatePost serves `POST /-/asc/inbox/state`: pin, unpin, snooze or wake one pull request of the
// signed-in person's inbox. The marks are the doer's own; the pull request must be one the inbox loader lists
// for the doer, which re-checks read access, so a pull request id of someone else's repository changes nothing.
func AscInboxStatePost(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscInboxStatePost", nil)
		return
	}
	pullID, err := strconv.ParseInt(ctx.FormString("pull"), 10, 64)
	if err != nil || pullID <= 0 {
		ctx.Error(http.StatusBadRequest)
		return
	}
	now := time.Now()
	entries, err := ascInboxLoad(ctx, ctx.Doer, now)
	if err != nil {
		ctx.ServerError("ascInboxLoad", err)
		return
	}
	found := false
	for _, e := range entries {
		if e.PullID == pullID {
			found = true
			break
		}
	}
	if !found {
		ctx.NotFound("AscInboxStatePost", nil)
		return
	}
	var pinned *bool
	var until *time.Time
	switch ctx.FormString("do") {
	case "pin":
		v := true
		pinned = &v
	case "unpin":
		v := false
		pinned = &v
	case "snooze":
		t, ok := ascInboxSnoozeUntil(ctx.FormString("for"), now)
		if !ok {
			ctx.Error(http.StatusBadRequest)
			return
		}
		until = &t
	case "wake":
		t := time.Time{}
		until = &t
	default:
		ctx.Error(http.StatusBadRequest)
		return
	}
	if _, err := issues_model.SetAscInboxState(ctx, ctx.Doer.ID, pullID, pinned, until, now); err != nil {
		ctx.ServerError("SetAscInboxState", err)
		return
	}
	ctx.Redirect(ascInboxURL(ascInboxTabOf(ctx.FormString("tab")), "", 0))
}

// ascInboxCountTTL is how long the header badge reuses a count, in seconds. The count costs a walk over the
// person's pull requests, and every page of the app asks for it once.
const ascInboxCountTTL = 60

// AscInboxCount serves `GET /-/asc/inbox/count`: how many items need the signed-in person, for the badge in
// the header. Only the number is returned. It is cached for a short time per person.
func AscInboxCount(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscInboxCount", nil)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	key := "AscInboxCount_" + strconv.FormatInt(ctx.Doer.ID, 10)
	if ctx.Cache != nil {
		if v, ok := ctx.Cache.Get(key).(int); ok {
			ctx.JSON(http.StatusOK, map[string]int{"count": v})
			return
		}
	}
	now := time.Now()
	entries, err := ascInboxLoad(ctx, ctx.Doer, now)
	if err != nil {
		ctx.ServerError("ascInboxLoad", err)
		return
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.PullID)
	}
	states, err := issues_model.ListAscInboxStates(ctx, ctx.Doer.ID, ids)
	if err != nil {
		ctx.ServerError("ListAscInboxStates", err)
		return
	}
	groups, _ := ascInboxGroup(entries, states, now)
	count := 0
	for _, g := range groups {
		if ascDashActionable(g.Section) {
			count += len(g.Entries)
		}
	}
	if ctx.Cache != nil {
		if err := ctx.Cache.Put(key, count, ascInboxCountTTL); err != nil {
			log.Warn("asc inbox: caching the count of user %d: %v", ctx.Doer.ID, err)
		}
	}
	ctx.JSON(http.StatusOK, map[string]int{"count": count})
}

// ascInboxFilterOf returns v when it is one of the allowed values, else "" (the default).
func ascInboxFilterOf(v string, allowed []string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return ""
}

// ascInboxFilterRows keeps the rows of people ("humans": pull requests) or of agents, and sorts them: by default
// the newest first as loaded, "oldest" the oldest first, "repo" by repository. A sorted list has no day headings,
// they would be out of order.
func ascInboxFilterRows(rows []AscInboxRow, kind, sort string) []AscInboxRow {
	switch kind {
	case "humans":
		rows = slices.DeleteFunc(slices.Clone(rows), func(r AscInboxRow) bool { return r.Entry == nil })
	case "agents":
		rows = slices.DeleteFunc(slices.Clone(rows), func(r AscInboxRow) bool { return r.Entry != nil })
	}
	if sort == "" {
		return rows
	}
	rows = slices.Clone(rows)
	repoOf := func(r AscInboxRow) string {
		if r.Entry != nil {
			return strings.ToLower(r.Entry.RepoFullName)
		}
		return strings.ToLower(r.Dash.Where)
	}
	updated := func(r AscInboxRow) time.Time {
		if r.Entry != nil {
			return r.Entry.Updated
		}
		return time.Time{}
	}
	slices.SortStableFunc(rows, func(a, b AscInboxRow) int {
		if sort == "repo" {
			return strings.Compare(repoOf(a), repoOf(b))
		}
		return updated(a).Compare(updated(b))
	})
	for i := range rows {
		rows[i].Day = ""
	}
	return rows
}
