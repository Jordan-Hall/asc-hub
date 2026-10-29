// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"

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
	labelKey    string
}

// Cause is one reason for failing, from one failure group.
type Cause struct {
	Index   int
	GroupID uint64
	Title   string
	Tests   int
	IsNew   bool
	Excerpt string
	// Where is the first "path:line" the excerpt names, with the anchor of that line in the Files page of the pull
	// request ("" when there is none): "Open the line" jumps to it.
	Where    string
	Anchor   string
	RunURL   string
	StateKey string
	State    string
	titleKey string
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

	// The text above is English, the fallback. These say which locale key and arguments make the same sentence;
	// Localize fills the text in the language of the request.
	headline pullMsg
	pillKey  string
	main     pullMsg
}

func statusLabel(state string) (label, key, tone string) {
	switch state {
	case "success":
		return "Pass", "pass", "ok"
	case "failure":
		return "Failed", "failed", "no"
	case "error":
		return "Error", "error", "no"
	case "pending":
		return "Running", "running", "wn"
	case "warning":
		return "Warning", "warning", "wn"
	case "skipped":
		return "Skipped", "skipped", "ro"
	}
	return "Unknown", "unknown", "ro"
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
		label, labelKey, tone := statusLabel(s.State)
		req := false
		for _, want := range in.Required {
			if contextMatches(want, s.Context) {
				req = true
			}
		}
		pc.Statuses = append(pc.Statuses, StatusRow{
			Context: CleanText(s.Context, MaxTextBytes), Label: label, labelKey: labelKey, Tone: tone,
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
			pc.Statuses = append(pc.Statuses, StatusRow{Context: name, Label: "No result for this version", labelKey: "no_result", Tone: "wn", Required: true, Stale: true})
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
		cause.Where, cause.Anchor = ExcerptLocation(cause.Excerpt)
		if cause.Title == "" {
			if cause.Title = firstNonEmpty(c.Group.TestName, c.Group.JobName); cause.Title == "" {
				cause.Title, cause.titleKey = "a failing check", "asc_trust.pull.cause_fallback_title"
			}
		}
		pc.Causes = append(pc.Causes, cause)
	}

	// The headline.
	reasons := len(pc.Causes)
	switch {
	case failed > 0:
		pc.Tone, pc.Pill, pc.pillKey = "no", "Failing", "failing"
		pc.Headline = plural(failed, "check", "checks") + " failed"
		pc.headline = pullMsg{Key: "headline_failed_" + pluralKey(failed) + "_none", Args: []any{failed}}
		if in.CausesKnown && reasons > 0 {
			pc.Headline += ", for " + plural(reasons, "reason", "reasons")
			pc.headline = pullMsg{Key: "headline_failed_" + pluralKey(failed) + "_" + pluralKey(reasons), Args: []any{failed, reasons}}
		}
	case pending > 0 || running:
		pc.Tone, pc.Pill, pc.pillKey = "wn", "Running", "running"
		pc.Headline = "Checks are running"
		pc.headline = pullMsg{Key: "headline_running"}
	case len(pc.Stale) > 0:
		pc.Tone, pc.Pill, pc.pillKey = "wn", "Waiting", "waiting"
		pc.Headline = "Waiting for " + plural(len(pc.Stale), "required check", "required checks") + " on this version"
		pc.headline = pullMsg{Key: "headline_waiting_" + pluralKey(len(pc.Stale)), Args: []any{len(pc.Stale)}}
	case len(in.Statuses) == 0:
		pc.Tone, pc.Pill, pc.pillKey = "ro", "No checks", "none"
		pc.Headline = "No checks have reported for this version"
		pc.headline = pullMsg{Key: "headline_none"}
	default:
		pc.Tone, pc.Pill, pc.pillKey = "ok", "Passing", "passing"
		pc.Headline = "All checks passed"
		pc.headline = pullMsg{Key: "headline_passed"}
	}

	// Compared with the base branch.
	switch {
	case in.MainStatuses == nil:
		pc.Main, pc.main = "Unknown", pullMsg{Key: "main_unknown"}
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
			pc.Main, pc.MainTone, pc.main = "Main has no results.", "ro", pullMsg{Key: "main_none"}
		case mainFailed > 0:
			pc.Main, pc.MainTone, pc.main = "Main is failing: "+plural(mainFailed, "check", "checks")+" failed there too.", "no", pullMsg{Key: "main_failing_" + pluralKey(mainFailed), Args: []any{mainFailed}}
		case mainPending > 0:
			pc.Main, pc.MainTone, pc.main = "Main is still running its checks.", "wn", pullMsg{Key: "main_pending"}
		default:
			pc.Main, pc.MainTone, pc.main = "Main is green.", "ok", pullMsg{Key: "main_green"}
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

// excerptLocation finds a source location in a log: a path with an extension, a colon and a line number.
var excerptLocation = regexp.MustCompile(`(?:^|[\s(\[\x60'"])((?:[A-Za-z0-9_.@-]+/)*[A-Za-z0-9_.@-]+\.[A-Za-z][A-Za-z0-9]{0,7}):(\d{1,6})(?::\d+)?`)

// ExcerptLocation returns the first "path:line" of a log excerpt as shown ("src/client.rs:41") and the anchor of that line
// in the diff of the pull request ("diff-<hash of the path>R<line>"), or two empty strings. A path that climbs out of the
// repository ("..") or starts at the root of the machine is not a location of the change.
func ExcerptLocation(excerpt string) (where, anchor string) {
	for _, m := range excerptLocation.FindAllStringSubmatch(excerpt, 20) {
		path, line := m[1], m[2]
		if strings.Contains(path, "..") || strings.HasPrefix(path, "/") || strings.HasPrefix(path, ".git/") {
			continue
		}
		if n, err := strconv.Atoi(line); err != nil || n < 1 {
			continue
		}
		return path + ":" + line, "diff-" + git.HashFilePathForWebUI(path) + "R" + line
	}
	return "", ""
}
