// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// The checks of one version of a change (the Checks board): what failed, for how many reasons, what is flaky, and
// which results belong to another version. The router reads the statuses and the Actions runs from Forgejo and the
// failure groups from asc; this file turns them into the page. It is pure, so its rules have tests.
//
// Results belong to the exact version being read. A required check with no result on this version is shown as
// "no result for this version", never as passing: when a new version is pushed, the results of the old one turn
// stale instead of passing silently.

const (
	maxCauses       = 10
	maxFlakyCards   = 3
	maxExcerptBytes = 1200
	causeSinceSlack = 60
)

// StatusIn is a commit status as the router read it from Forgejo.
type StatusIn struct {
	Context     string
	State       string // success, failure, error, pending, warning or skipped
	Description string
	URL         string
}

// RunIn is an Actions run on the head commit.
type RunIn struct {
	ID      int64
	Title   string
	URL     string
	State   string // success, failure, running, waiting, cancelled, skipped or blocked
	Created time.Time
	Started time.Time
	Stopped time.Time
}

// CauseIn is a failure group with the occurrences that belong to this version (the runs of its head commit).
type CauseIn struct {
	Group       GroupRow
	Occurrences []OccurrenceRow
	History     *History
}

// PullChecksIn is everything the page is built from.
type PullChecksIn struct {
	Version  uint32
	Head     string
	Statuses []StatusIn
	Required []string
	Runs     []RunIn
	// MainStatuses are the statuses of the base branch tip; nil when unknown.
	MainStatuses []StatusIn
	// Causes are the open failure groups seen on this version; nil when asc could not be asked.
	Causes      []CauseIn
	CausesKnown bool
	Now         time.Time
}

// StatusRow is one line of "All checks".
type StatusRow struct {
	Context     string
	Label       string
	Tone        string
	Description string
	URL         string
	Required    bool
	Stale       bool
}

// Cause is one reason for failing, from one failure group.
type Cause struct {
	Index    int
	GroupID  uint64
	Title    string
	Tests    int
	IsNew    bool
	Excerpt  string
	RunURL   string
	StateKey string
	State    string
}

// FlakyCard is a failing test that is probably not the fault of this change.
type FlakyCard struct {
	GroupID  uint64
	Test     string
	Flips    uint32
	Runs     uint32
	Strip    []StripCell
	StateKey string
	State    string
	// IssueQuery is the query string of the new-issue link: the title is filled in.
	IssueQuery string
}

// PullChecks is the page.
type PullChecks struct {
	Headline string
	Tone     string
	Pill     string
	Version  uint32
	Head     string
	Started  time.Time
	Took     string
	Stale    []string
	Statuses []StatusRow
	Causes   []Cause
	Flaky    []FlakyCard
	Main     string
	MainTone string
	// FailedRuns is the number of finished runs that failed; the "re-run failed only" button needs one.
	FailedRuns int
	Runs       int
}

func statusLabel(state string) (label, tone string) {
	switch state {
	case "success":
		return "Pass", "ok"
	case "failure":
		return "Failed", "no"
	case "error":
		return "Error", "no"
	case "pending":
		return "Running", "wn"
	case "warning":
		return "Warning", "wn"
	case "skipped":
		return "Skipped", "ro"
	}
	return "Unknown", "ro"
}

func isFailed(state string) bool { return state == "failure" || state == "error" }

// contextMatches reports whether the status context ctxName satisfies the required check named by want. A required
// name is a glob with `*` for any run of characters, as in the branch protection of Forgejo.
func contextMatches(want, ctxName string) bool { return nameGlob(want, ctxName) }

// BuildPullChecks builds the page.
func BuildPullChecks(in PullChecksIn) *PullChecks {
	pc := &PullChecks{Version: in.Version, Head: shortSHA(in.Head)}

	var failed, pending int
	for _, s := range in.Statuses {
		label, tone := statusLabel(s.State)
		req := false
		for _, want := range in.Required {
			if contextMatches(want, s.Context) {
				req = true
			}
		}
		pc.Statuses = append(pc.Statuses, StatusRow{
			Context: CleanText(s.Context, MaxTextBytes), Label: label, Tone: tone,
			Description: CleanText(s.Description, MaxTextBytes), URL: safeURL(s.URL), Required: req,
		})
		switch {
		case isFailed(s.State):
			failed++
		case s.State == "pending":
			pending++
		}
	}
	for _, want := range in.Required {
		found := false
		for _, s := range in.Statuses {
			if contextMatches(want, s.Context) {
				found = true
				break
			}
		}
		if !found {
			name := CleanText(want, MaxTextBytes)
			pc.Stale = append(pc.Stale, name)
			pc.Statuses = append(pc.Statuses, StatusRow{Context: name, Label: "No result for this version", Tone: "wn", Required: true, Stale: true})
		}
	}
	sort.SliceStable(pc.Statuses, func(i, j int) bool { return rank(pc.Statuses[i]) < rank(pc.Statuses[j]) })

	// Runs: when they started, how long they took, which can be re-run.
	var first, last time.Time
	running := false
	for _, r := range in.Runs {
		pc.Runs++
		if !r.Started.IsZero() && (first.IsZero() || r.Started.Before(first)) {
			first = r.Started
		}
		if r.Stopped.After(last) {
			last = r.Stopped
		}
		switch r.State {
		case "failure":
			pc.FailedRuns++
		case "running", "waiting", "blocked":
			running = true
		}
	}
	pc.Started = first
	if !first.IsZero() && !last.IsZero() && last.After(first) && !running {
		pc.Took = duration(last.Sub(first))
	}

	// Causes and flaky cards.
	earliest := first
	for _, r := range in.Runs {
		if !r.Created.IsZero() && (earliest.IsZero() || r.Created.Before(earliest)) {
			earliest = r.Created
		}
	}
	runURL := map[uint64]string{}
	for _, r := range in.Runs {
		if r.ID > 0 {
			runURL[uint64(r.ID)] = safeURL(r.URL)
		}
	}
	for _, c := range in.Causes {
		if len(c.Occurrences) == 0 {
			continue
		}
		if c.Group.Flaky && c.Group.StateKey != "quarantined" && len(pc.Flaky) < maxFlakyCards {
			card := FlakyCard{GroupID: c.Group.ID, Test: firstNonEmpty(c.Group.TestName, c.Group.Title), StateKey: c.Group.StateKey, State: c.Group.State}
			if c.History != nil {
				card.Flips, card.Runs, card.Strip = c.History.Flips, c.History.Runs, c.History.Cells
			}
			card.IssueQuery = "title=" + queryEscape("Flaky test: "+card.Test)
			pc.Flaky = append(pc.Flaky, card)
			continue
		}
		if c.Group.StateKey == "quarantined" {
			continue // a quarantined failure does not block landing and is not a reason to fail
		}
		cause := Cause{
			Index: len(pc.Causes) + 1, GroupID: c.Group.ID, Title: c.Group.Title, Tests: distinctTests(c),
			IsNew:   !earliest.IsZero() && !c.Group.FirstSeen.IsZero() && !c.Group.FirstSeen.Before(earliest.Add(-causeSinceSlack*time.Second)),
			Excerpt: CleanBlock(c.Occurrences[0].Message, maxExcerptBytes), RunURL: runURL[c.Occurrences[0].RunID],
			StateKey: c.Group.StateKey, State: c.Group.State,
		}
		if cause.Title == "" {
			cause.Title = firstNonEmpty(c.Group.TestName, c.Group.JobName, "a failing check")
		}
		pc.Causes = append(pc.Causes, cause)
	}

	// The headline.
	reasons := len(pc.Causes)
	switch {
	case failed > 0:
		pc.Tone, pc.Pill = "no", "Failing"
		pc.Headline = plural(failed, "check", "checks") + " failed"
		if in.CausesKnown && reasons > 0 {
			pc.Headline += ", for " + plural(reasons, "reason", "reasons")
		}
	case pending > 0 || running:
		pc.Tone, pc.Pill = "wn", "Running"
		pc.Headline = "Checks are running"
	case len(pc.Stale) > 0:
		pc.Tone, pc.Pill = "wn", "Waiting"
		pc.Headline = "Waiting for " + plural(len(pc.Stale), "required check", "required checks") + " on this version"
	case len(in.Statuses) == 0:
		pc.Tone, pc.Pill = "ro", "No checks"
		pc.Headline = "No checks have reported for this version"
	default:
		pc.Tone, pc.Pill = "ok", "Passing"
		pc.Headline = "All checks passed"
	}

	// Compared with the base branch.
	switch {
	case in.MainStatuses == nil:
		pc.Main = "Unknown"
	default:
		mainFailed, mainPending := 0, 0
		for _, s := range in.MainStatuses {
			if isFailed(s.State) {
				mainFailed++
			} else if s.State == "pending" {
				mainPending++
			}
		}
		switch {
		case len(in.MainStatuses) == 0:
			pc.Main, pc.MainTone = "Main has no results.", "ro"
		case mainFailed > 0:
			pc.Main, pc.MainTone = "Main is failing: "+plural(mainFailed, "check", "checks")+" failed there too.", "no"
		case mainPending > 0:
			pc.Main, pc.MainTone = "Main is still running its checks.", "wn"
		default:
			pc.Main, pc.MainTone = "Main is green.", "ok"
		}
	}
	return pc
}

func rank(s StatusRow) int {
	switch {
	case s.Tone == "no":
		return 0
	case s.Stale:
		return 1
	case s.Tone == "wn":
		return 2
	}
	return 3
}

func distinctTests(c CauseIn) int {
	if c.Group.TestName != "" {
		return 1
	}
	return len(c.Occurrences)
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// safeURL keeps a link that is a path of this site or an http(s) URL, and drops anything else (javascript:, data:).
func safeURL(u string) string {
	u = strings.TrimSpace(u)
	switch {
	case u == "", len(u) > 2048:
		return ""
	case strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//"):
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
	default:
		return ""
	}
	for _, r := range u {
		if r < 0x21 || r == 0x7f {
			return ""
		}
	}
	return u
}

func queryEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// duration writes a duration the way the board does: "3 min 12 s", "41 s", "1 h 5 min".
func duration(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s < 60:
		return itoa(s) + " s"
	case s < 3600:
		return itoa(s/60) + " min " + itoa(s%60) + " s"
	}
	return itoa(s/3600) + " h " + itoa((s%3600)/60) + " min"
}

// PullCauses asks asc for the failure groups that were seen in the given runs: the open and acknowledged groups
// seen since the first of the runs, each with its occurrences, kept when one of them is in a run of this version.
// At most maxCauses groups are read. The history strip is read for the flaky ones only.
func PullCauses(ctx context.Context, conn grpc.ClientConnInterface, repoName string, runIDs map[uint64]bool, since time.Time) ([]CauseIn, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}
	client := ascv1.NewCiInsightsClient(conn)
	cctx, cancel := context.WithTimeout(ctx, 2*CallTimeout)
	defer cancel()
	req := &ascv1.ListFailureGroupsRequest{
		Repo: RepoRef(repoName), Limit: maxCauses,
		States: []ascv1.FailureGroupState{
			ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN,
			ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED,
			ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED,
		},
	}
	if !since.IsZero() {
		req.Since = since.Add(-causeSinceSlack * time.Second).Unix()
	}
	resp, err := client.ListFailureGroups(cctx, req)
	if err != nil {
		return nil, err
	}
	page, err := GroupsPageOf(resp)
	if err != nil {
		return nil, err
	}
	var out []CauseIn
	flaky := 0
	for _, g := range page.Rows {
		detail, err := client.GetFailureGroup(cctx, &ascv1.GetFailureGroupRequest{Repo: RepoRef(repoName), Id: g.ID, Limit: maxOccurrences})
		if err != nil {
			return nil, err
		}
		d, err := GroupDetailOf(detail)
		if err != nil {
			return nil, err
		}
		var mine []OccurrenceRow
		for _, o := range d.Occurrences {
			if runIDs[o.RunID] {
				mine = append(mine, o)
			}
		}
		if len(mine) == 0 {
			continue
		}
		c := CauseIn{Group: g, Occurrences: mine}
		if g.Flaky && flaky < maxFlakyCards && (g.TestName != "" || g.JobName != "") {
			flaky++
			if h, err := client.FlakyHistory(cctx, &ascv1.FlakyHistoryRequest{Repo: RepoRef(repoName), TestName: g.TestName, JobName: g.JobName, WindowDays: 14}); err == nil {
				if hist, err := HistoryOf(h); err == nil {
					c.History = hist
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}
