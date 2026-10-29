// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/organization"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// The user dashboard of the design board "Dashboard": a greeting, what needs the person (the actionable part
// of the inbox plus the agent runs that wait for an answer), the agents working now, the person's stacks,
// today's activity and the sidebar of repositories and teams. Everything is read for the signed-in person:
// the inbox loader re-checks read access to every pull request, the stacks are the person's own pull requests
// (again read-checked), and the agent runs are asked for with `created_by` set to the signed-in name.

const (
	// ascDashMaxRows bounds the rows of "Needs you".
	ascDashMaxRows = 6
	// ascDashMaxStacks bounds the stacks of the side card.
	ascDashMaxStacks = 5
	// ascDashMaxRepos and ascDashMaxTeams bound the sidebar lists.
	ascDashMaxRepos = 5
	ascDashMaxTeams = 6
	// ascDashAgentRepos is how many repositories are asked for running agents, ascDashAgentRuns how many runs are shown.
	ascDashAgentRepos   = 8
	ascDashAgentRuns    = 3
	ascDashAgentTimeout = 4 * time.Second
	// ascDashTodayMax is how many activities "Today" shows.
	ascDashTodayMax = 6
)

// AscDashChip is a small coloured fact on a row. The text is already translated.
type AscDashChip struct {
	Tone string // amber, violet, green, red, grey or info
	Text string
}

// AscDashRow is one row of "Needs you".
type AscDashRow struct {
	Tone    string // the colour of the dot: amber, red or green
	Title   string
	Where   string // "owner/repo - stack 3 of 4"
	Link    string
	Chips   []AscDashChip
	Meta    string // "+84 -12 in 3 files"
	Updated time.Time
	Action  string // "Merge" on a row that is ready to land, else empty
}

// AscDashSeg is one change of a stack.
type AscDashSeg struct {
	Tone  string // green, amber, red or grey
	Label string
	Link  string // the review of this change, so a click on the bar opens it
}

// AscDashStack is one of the person's stacks.
type AscDashStack struct {
	Name string
	Link string
	Segs []AscDashSeg
}

// AscDashAgent is one running agent.
type AscDashAgent struct {
	Title   string
	Agent   string
	Repo    string
	Link    string
	State   ascagentui.State
	Percent int    // the fullest limited meter, 0 when nothing is limited
	Meter   string // "58% budget - 6 of 20 min", empty when nothing is limited
}

// AscDashRepo is a repository of the sidebar.
type AscDashRepo struct {
	FullName string
	Link     string
}

// AscDashTeam is a team of the sidebar.
type AscDashTeam struct {
	Name string
	Org  string
	Link string
}

// AscDash is everything the dashboard template shows beyond the stock data.
type AscDash struct {
	Greeting     string // "Good afternoon, Jordan"
	Summary      string // "7 things need you. Nothing is blocked."
	NeedsCount   int
	BlockedCount int
	ReviewCount  int
	InboxLink    string // empty when the inbox is off
	Rows         []AscDashRow
	Agents       []AscDashAgent
	AgentsOn     bool
	AgentsActive int
	Stacks       []AscDashStack
	StackCount   int
	Repos        []AscDashRepo
	Teams        []AscDashTeam
	Scopes       []AscDashScope
	Scope        string
}

// ascDashAgentsLoad returns the agents working now. Tests replace it.
var ascDashAgentsLoad = ascDashAgentsFromServe

// ascDashActionable reports whether a section is one the person has to act on.
func ascDashActionable(s issues_model.AscInboxSection) bool {
	switch s {
	case issues_model.AscInboxNeedsReview, issues_model.AscInboxReturned, issues_model.AscInboxReady, issues_model.AscInboxBlocked:
		return true
	}
	return false
}

// ascDashTone is the colour of the dot of a section.
func ascDashTone(s issues_model.AscInboxSection) string {
	switch s {
	case issues_model.AscInboxBlocked:
		return "red"
	case issues_model.AscInboxReady:
		return "green"
	case issues_model.AscInboxNeedsReview, issues_model.AscInboxReturned:
		return "amber"
	}
	return "grey"
}

// ascTrN is TrN as a plain string.
func ascTrN(l translation.Locale, n int, one, many string, args ...any) string {
	return string(l.TrN(n, one, many, args...))
}

// ascGreetingKey returns "Good morning", "Good afternoon" or "Good evening" for the local hour.
func ascGreetingKey(hour int) string {
	switch {
	case hour < 12:
		return "user.asc_dash.good_morning"
	case hour < 18:
		return "user.asc_dash.good_afternoon"
	default:
		return "user.asc_dash.good_evening"
	}
}

// ascGreetName is the first word of the display name, or the login when the name is empty.
func ascGreetName(u *user_model.User) string {
	name := strings.TrimSpace(u.FullName)
	if name == "" {
		return u.Name
	}
	if i := strings.IndexAny(name, " \t"); i > 0 {
		name = name[:i]
	}
	return name
}

// ascDashSummary writes "7 things need you. Nothing is blocked."
func ascDashSummary(l translation.Locale, needs, blocked int) string {
	var s string
	if needs == 0 {
		s = l.TrString("user.asc_dash.needs_none")
	} else {
		s = ascTrN(l, needs, "user.asc_dash.needs_one", "user.asc_dash.needs_n", needs)
	}
	if blocked == 0 {
		return s + " " + l.TrString("user.asc_dash.blocked_none")
	}
	return s + " " + l.TrString("user.asc_dash.blocked_n", blocked)
}

// ascDashRowFor draws one inbox entry as a row of "Needs you".
func ascDashRowFor(l translation.Locale, e *AscInboxEntry, info *AscInboxInfo) AscDashRow {
	row := AscDashRow{
		Tone:    ascDashTone(e.Section),
		Title:   e.Title,
		Link:    e.Link,
		Updated: e.Updated,
	}
	where := e.RepoFullName + " #" + strconv.FormatInt(e.Index, 10)
	if info != nil && info.StackTotal > 1 {
		where = e.RepoFullName + " · " + l.TrString("user.asc_dash.stack_pos", info.StackIndex, info.StackTotal)
	}
	row.Where = where
	switch {
	case e.Conflicted:
		row.Chips = append(row.Chips, AscDashChip{"red", l.TrString("user.asc_dash.conflicts")})
	case e.Draft:
		row.Chips = append(row.Chips, AscDashChip{"grey", l.TrString("user.asc_dash.draft")})
	}
	if info != nil {
		switch {
		case info.ChecksFail > 0:
			row.Chips = append(row.Chips, AscDashChip{"red", ascTrN(l, info.ChecksFail, "user.asc_dash.check_failing_one", "user.asc_dash.check_failing_n", info.ChecksFail)})
		case info.ChecksPending > 0:
			row.Chips = append(row.Chips, AscDashChip{"grey", l.TrString("user.asc_dash.checks_running")})
		case info.ChecksPass > 0:
			row.Chips = append(row.Chips, AscDashChip{"green", ascTrN(l, info.ChecksPass, "user.asc_dash.check_pass_one", "user.asc_dash.check_pass_n", info.ChecksPass)})
		}
		if info.Approvals > 0 {
			row.Chips = append(row.Chips, AscDashChip{"green", ascTrN(l, info.Approvals, "user.asc_dash.approval_one", "user.asc_dash.approval_n", info.Approvals)})
		}
		if info.HasDiff {
			row.Meta = ascDiffMeta(l, info)
		}
	}
	switch e.Section {
	case issues_model.AscInboxNeedsReview:
		row.Chips = append([]AscDashChip{{"amber", l.TrString("user.asc_dash.review_asked")}}, row.Chips...)
	case issues_model.AscInboxReturned:
		row.Chips = append([]AscDashChip{{"amber", l.TrString("user.asc_dash.changes_asked")}}, row.Chips...)
	case issues_model.AscInboxReady:
		row.Action = l.TrString("user.asc_dash.merge")
	}
	return row
}

// ascDiffMeta writes "+84 -12 in 3 files" with the real minus sign.
func ascDiffMeta(l translation.Locale, info *AscInboxInfo) string {
	return fmt.Sprintf("+%d −%d %s", info.Additions, info.Deletions, ascTrN(l, info.Files, "user.asc_dash.in_file_one", "user.asc_dash.in_file_n", info.Files))
}

// ascDashBuild assembles the dashboard of the signed-in person. Failures of one part leave that part out.
func ascDashBuild(ctx *context.Context, now time.Time) *AscDash {
	doer := ctx.Doer
	d := &AscDash{AgentsOn: setting.Asc.Enabled && ascagentui.FeaturesEnabled(ctx, doer)}
	if setting.Asc.Enabled {
		d.InboxLink = setting.AppSubURL + "/-/asc/inbox"
	}
	d.Greeting = ctx.Locale.TrString(ascGreetingKey(now.In(setting.DefaultUILocation).Hour()), ascGreetName(doer))

	entries, err := ascInboxLoad(ctx, doer, now)
	if err != nil {
		log.Error("asc dashboard: inbox of user %d: %v", doer.ID, err)
	}
	// the scope chips: the owners the person works with, from the pull requests, the repositories and the teams
	repos := ascDashRepos(ctx, doer)
	teams := ascDashTeams(ctx, doer)
	owners := make([]string, 0, len(entries)+len(repos)+len(teams))
	for _, e := range entries {
		owners = append(owners, ascOwnerOf(e.RepoFullName))
	}
	for _, r := range repos {
		owners = append(owners, ascOwnerOf(r.FullName))
	}
	for _, tm := range teams {
		owners = append(owners, tm.Org)
	}
	var scope string
	d.Scopes, scope = ascDashScopes(ctx, doer, owners, ctx.Locale)
	d.Scope = scope
	entries = slices.DeleteFunc(slices.Clone(entries), func(e *AscInboxEntry) bool { return !ascScopeKeep(scope, doer.Name, ascOwnerOf(e.RepoFullName)) })

	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.PullID)
	}
	states, err := issues_model.ListAscInboxStates(ctx, doer.ID, ids)
	if err != nil {
		log.Error("asc dashboard: inbox marks of user %d: %v", doer.ID, err)
		states = nil
	}
	groups, _ := ascInboxGroup(entries, states, now)
	var actionable []*AscInboxEntry
	for _, g := range groups {
		if !ascDashActionable(g.Section) {
			continue
		}
		switch g.Section {
		case issues_model.AscInboxBlocked:
			d.BlockedCount += len(g.Entries)
		case issues_model.AscInboxNeedsReview:
			d.ReviewCount += len(g.Entries)
		}
		actionable = append(actionable, g.Entries...)
	}
	d.NeedsCount = len(actionable)

	if d.AgentsOn {
		agents, waiting, err := ascDashAgentsLoad(ctx, doer)
		agents = slices.DeleteFunc(slices.Clone(agents), func(a AscDashAgent) bool { return !ascScopeKeep(scope, doer.Name, ascOwnerOf(a.Repo)) })
		if err != nil {
			log.Warn("asc dashboard: agents of user %d: %v", doer.ID, err)
		}
		d.Agents = agents
		d.AgentsActive = len(agents)
		d.NeedsCount += waiting
		d.BlockedCount += waiting
	}
	d.Summary = ascDashSummary(ctx.Locale, d.NeedsCount, d.BlockedCount)

	for i, e := range actionable {
		if i == ascDashMaxRows {
			break
		}
		d.Rows = append(d.Rows, ascDashRowFor(ctx.Locale, e, ascInboxInfoLoad(ctx, e)))
	}
	d.Stacks, d.StackCount = ascDashStacks(ctx, doer, ctx.Locale)
	d.Stacks = slices.DeleteFunc(d.Stacks, func(s AscDashStack) bool { return !ascScopeKeep(scope, doer.Name, ascOwnerOf(s.Link)) })
	d.StackCount = len(d.Stacks)
	d.Repos = slices.DeleteFunc(repos, func(r AscDashRepo) bool { return !ascScopeKeep(scope, doer.Name, ascOwnerOf(r.FullName)) })
	d.Teams = teams
	return d
}

// ascDashToday keeps the activities of the last day, newest first, at most ascDashTodayMax.
func ascDashToday(feeds activities_model.ActionList, now time.Time) activities_model.ActionList {
	var out activities_model.ActionList
	for _, a := range feeds {
		if len(out) == ascDashTodayMax {
			break
		}
		if a.GetCreate().Before(now.Add(-24 * time.Hour)) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// ascDashRepos lists the repositories the person works in, most recently updated first.
func ascDashRepos(ctx gocontext.Context, doer *user_model.User) []AscDashRepo {
	repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: ascDashMaxRepos},
		Actor:       doer,
		OwnerID:     doer.ID,
		Private:     true,
		Collaborate: optional.None[bool](),
		Archived:    optional.Some(false),
		OrderBy:     "updated_unix DESC",
	})
	if err != nil {
		log.Error("asc dashboard: repositories of user %d: %v", doer.ID, err)
		return nil
	}
	out := make([]AscDashRepo, 0, len(repos))
	for _, r := range repos {
		out = append(out, AscDashRepo{FullName: r.FullName(), Link: r.Link()})
	}
	return out
}

// ascDashTeams lists the teams the person is in, with their organisation.
func ascDashTeams(ctx gocontext.Context, doer *user_model.User) []AscDashTeam {
	teams, err := organization.GetUserTeams(ctx, doer.ID)
	if err != nil {
		log.Error("asc dashboard: teams of user %d: %v", doer.ID, err)
		return nil
	}
	var out []AscDashTeam
	for _, t := range teams {
		if len(out) == ascDashMaxTeams {
			break
		}
		if t.IsOwnerTeam() {
			continue // the owners team is not a place to work in
		}
		org, err := organization.GetOrgByID(ctx, t.OrgID)
		if err != nil {
			continue
		}
		out = append(out, AscDashTeam{Name: t.Name, Org: org.Name, Link: t.Link(ctx)})
	}
	return out
}

// ascDashStacks lists the stacks of the person's open pull requests, each as one segment per change: merged and
// approved changes are green, a change in review is amber, one with a conflict or a rejection is red and a
// draft is grey. It returns at most ascDashMaxStacks stacks and the number of stacks found.
func ascDashStacks(ctx gocontext.Context, doer *user_model.User, l translation.Locale) ([]AscDashStack, int) {
	var rows []*issues_model.PullRequestAscStack
	err := db.GetEngine(ctx).Table("pull_request_asc_stack").
		Join("INNER", "pull_request", "pull_request.id = pull_request_asc_stack.pull_id").
		Join("INNER", "issue", "issue.id = pull_request.issue_id").
		Where("issue.poster_id = ? AND issue.is_closed = ? AND pull_request.has_merged = ?", doer.ID, false, false).
		OrderBy("issue.updated_unix DESC").Limit(200).Find(&rows)
	if err != nil {
		log.Error("asc dashboard: stacks of user %d: %v", doer.ID, err)
		return nil, 0
	}
	// the bottom of each stack, in the order of the most recently changed pull request
	var roots []int64
	seen := map[int64]bool{}
	for _, r := range rows {
		root := r
		for i := 0; i < ascStackWalkLimit && root.StackParentID != 0; i++ {
			p, err := issues_model.GetPullRequestAscStack(ctx, root.StackParentID)
			if err != nil || p == nil {
				break
			}
			root = p
		}
		if !seen[root.PullID] {
			seen[root.PullID] = true
			roots = append(roots, root.PullID)
		}
	}
	var out []AscDashStack
	count := 0
	for _, rootID := range roots {
		st, ok := ascDashStackFrom(ctx, doer, rootID, l)
		if !ok {
			continue // a pull request of the stack is not readable any more: the stack is left out
		}
		count++
		if len(out) < ascDashMaxStacks {
			out = append(out, st)
		}
	}
	return out, count
}

// ascDashStackFrom draws the stack that starts at the pull request rootID.
func ascDashStackFrom(ctx gocontext.Context, doer *user_model.User, rootID int64, l translation.Locale) (AscDashStack, bool) {
	var st AscDashStack
	id := rootID
	for i := 0; i < ascStackWalkLimit; i++ {
		pr, err := issues_model.GetPullRequestByID(ctx, id)
		if err != nil || pr.LoadIssue(ctx) != nil || pr.LoadBaseRepo(ctx) != nil {
			return st, false
		}
		if !ascInboxCanSee(ctx, doer, pr.BaseRepo) {
			return st, false
		}
		if i == 0 {
			st.Name = pr.Issue.Title
		}
		seg := AscDashSeg{Tone: "amber", Label: l.TrString("user.asc_dash.seg_review", pr.Issue.Title)}
		switch {
		case pr.HasMerged:
			seg = AscDashSeg{Tone: "green", Label: l.TrString("user.asc_dash.seg_merged", pr.Issue.Title)}
		case pr.IsWorkInProgress(ctx):
			seg = AscDashSeg{Tone: "grey", Label: l.TrString("user.asc_dash.seg_draft", pr.Issue.Title)}
		case pr.Status == issues_model.PullRequestStatusConflict:
			seg = AscDashSeg{Tone: "red", Label: l.TrString("user.asc_dash.seg_conflict", pr.Issue.Title)}
		default:
			if rv, err := issues_model.GetAscInboxReviewers(ctx, pr.IssueID); err == nil {
				for _, r := range rv {
					if r.Type == issues_model.ReviewTypeReject && r.Official {
						seg = AscDashSeg{Tone: "red", Label: l.TrString("user.asc_dash.seg_rejected", pr.Issue.Title)}
					}
				}
				if seg.Tone == "amber" && ascInboxApproved(ctx, pr) {
					seg = AscDashSeg{Tone: "green", Label: l.TrString("user.asc_dash.seg_approved", pr.Issue.Title)}
				}
			}
		}
		seg.Link = ascPullLink(pr)
		st.Segs = append(st.Segs, seg)
		if !pr.HasMerged {
			st.Link = ascPullLink(pr)
		}
		kids, err := issues_model.GetAscStackChildren(ctx, id)
		if err != nil || len(kids) == 0 {
			break
		}
		id = kids[0].PullID
	}
	if st.Link == "" && len(st.Segs) > 0 {
		pr, err := issues_model.GetPullRequestByID(ctx, rootID)
		if err == nil && pr.LoadBaseRepo(ctx) == nil {
			st.Link = ascPullLink(pr)
		}
	}
	return st, len(st.Segs) > 0
}

// ascPullLink is the address of the pull request page; the base repository must be loaded.
func ascPullLink(pr *issues_model.PullRequest) string {
	return pr.BaseRepo.Link() + "/pulls/" + strconv.FormatInt(pr.Index, 10)
}

// ascDashAgentsFromServe asks asc serve for the person's runs that are queued, running or waiting for an
// answer. It returns the runs to show and how many of them wait for the person.
func ascDashAgentsFromServe(ctx *context.Context, doer *user_model.User) ([]AscDashAgent, int, error) {
	return ascAgentsActive(ctx, doer, ascDashAgentRepos, ascDashAgentRuns, ascDashAgentRuns)
}

// ascAgentsActive asks asc serve, for at most maxRepos repositories of the person, for at most perRepo active
// runs each, and returns at most maxTotal of them with how many wait for the person.
func ascAgentsActive(ctx *context.Context, doer *user_model.User, maxRepos, perRepo, maxTotal int) ([]AscDashAgent, int, error) {
	repos, err := ascAgentsHomeRepos(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(repos) > maxRepos {
		repos = repos[:maxRepos]
	}
	if len(repos) == 0 {
		return nil, 0, nil
	}
	conn, err := ascDraftsConn()
	if err != nil {
		return nil, 0, err
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascDashAgentTimeout)
	defer cancel()
	api := ascv1.NewAgentsClient(conn)
	states, _ := ascagentui.RunStateFilter("active")

	var out []AscDashAgent
	waiting := 0
	for _, repo := range repos {
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
		if err != nil {
			continue
		}
		resp, err := api.ListRuns(callCtx, &ascv1.ListAgentRunsRequest{
			Repo:      &ascv1.RepoRef{Name: name},
			States:    states,
			CreatedBy: doer.Name, // always the signed-in person
			Limit:     uint32(perRepo),
		})
		if err != nil {
			if f := ascagentui.Classify(err); f != nil && !f.Disabled {
				log.Warn("asc dashboard: ListRuns failed for repository %d: %s", repo.ID, f.Message)
			}
			continue
		}
		for _, run := range resp.GetRuns() {
			if run == nil || run.GetId() == 0 || run.GetRepo() != name {
				continue
			}
			a := ascDashAgentFrom(repo, run)
			if a.State.Slug == "waiting" {
				waiting++
			}
			out = append(out, a)
		}
	}
	if len(out) > maxTotal {
		out = out[:maxTotal]
	}
	return out, waiting, nil
}

// ascDashAgentFrom draws one run.
func ascDashAgentFrom(repo *repo_model.Repository, run *ascv1.AgentRun) AscDashAgent {
	title := ascagentui.CleanLine(run.GetTitle(), ascagentui.MaxTitleBytes)
	if title == "" {
		title = "Run " + strconv.FormatUint(run.GetId(), 10)
	}
	a := AscDashAgent{
		Title: title,
		Agent: ascagentui.SafeName(run.GetAgent()),
		Repo:  repo.FullName(),
		Link:  repo.Link() + "/asc/agents/runs/" + strconv.FormatUint(run.GetId(), 10),
		State: ascagentui.RunState(run.GetState()),
	}
	var fullest *ascagentui.Meter
	meters := ascagentui.Meters(run.GetLimits(), run.GetUsage())
	for i := range meters {
		if meters[i].Unlimited {
			continue
		}
		if fullest == nil || meters[i].Percent > fullest.Percent {
			fullest = &meters[i]
		}
	}
	if fullest != nil {
		a.Percent = fullest.Percent
		a.Meter = fmt.Sprintf("%d%% of %s: %s of %s", fullest.Percent, strings.ToLower(fullest.Label), fullest.Used, fullest.Limit)
	}
	return a
}

// ascFeedKind groups an activity for the filter of the Today feed: a push, a change (a pull request or an issue
// opened, merged or closed), a comment or review, else "other".
func ascFeedKind(a *activities_model.Action) string {
	switch a.OpType {
	case activities_model.ActionCommitRepo, activities_model.ActionPushTag, activities_model.ActionDeleteTag, activities_model.ActionDeleteBranch, activities_model.ActionMirrorSyncPush, activities_model.ActionMirrorSyncCreate, activities_model.ActionMirrorSyncDelete:
		return "push"
	case activities_model.ActionCreatePullRequest, activities_model.ActionMergePullRequest, activities_model.ActionClosePullRequest, activities_model.ActionReopenPullRequest, activities_model.ActionAutoMergePullRequest,
		activities_model.ActionCreateIssue, activities_model.ActionCloseIssue, activities_model.ActionReopenIssue:
		return "change"
	case activities_model.ActionCommentIssue, activities_model.ActionCommentPull, activities_model.ActionApprovePullRequest, activities_model.ActionRejectPullRequest:
		return "comment"
	}
	return "other"
}

// ascFeedKinds returns the kind of each activity of the list, in the same order.
func ascFeedKinds(list activities_model.ActionList) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = ascFeedKind(a)
	}
	return out
}
