// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// The checks surface (docs/BUILDOUT.md section 8): failures grouped by cause, the flaky tests and the history strip
// of one test, and the states a maintainer sets (acknowledge, quarantine, resolve).

const (
	maxGroupsPage      = 100
	maxOccurrences     = 100
	maxFlakyPage       = 100
	maxNameBytes       = 512
	maxBuckets         = 400
	maxStripCells      = 40
	flakyDefaultWindow = 30
)

// GroupStates lists the states of a failure group in the order the page shows them.
var GroupStates = []struct {
	Key   string
	Label string
	State ascv1.FailureGroupState
}{
	{"open", "Open", ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN},
	{"acknowledged", "Acknowledged", ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED},
	{"quarantined", "Quarantined", ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED},
	{"resolved", "Resolved", ascv1.FailureGroupState_FAILURE_GROUP_STATE_RESOLVED},
}

// GroupStateOf returns the state of a form value.
func GroupStateOf(key string) (ascv1.FailureGroupState, bool) {
	for _, s := range GroupStates {
		if s.Key == key {
			return s.State, true
		}
	}
	return ascv1.FailureGroupState_FAILURE_GROUP_STATE_UNSPECIFIED, false
}

func groupStateLabel(st ascv1.FailureGroupState) (key, label, tone string) {
	switch st {
	case ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN:
		return "open", "Open", "no"
	case ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED:
		return "acknowledged", "Acknowledged", "wn"
	case ascv1.FailureGroupState_FAILURE_GROUP_STATE_QUARANTINED:
		return "quarantined", "Quarantined", "wn"
	case ascv1.FailureGroupState_FAILURE_GROUP_STATE_RESOLVED:
		return "resolved", "Resolved", "ok"
	}
	return "unknown", "Unknown", "ro"
}

// GroupRow is one failure group.
type GroupRow struct {
	ID              uint64
	Signature       string
	Title           string
	TestName        string
	JobName         string
	StateKey        string
	State           string
	Tone            string
	Occurrences     uint32
	ChangesAffected uint32
	FirstSeen       time.Time
	LastSeen        time.Time
	Flaky           bool
	Note            string
}

// GroupsPage is one page of failure groups.
type GroupsPage struct {
	Rows []GroupRow
	Next string
}

func groupRow(g *ascv1.FailureGroup) (GroupRow, error) {
	if g == nil || g.GetId() == 0 {
		return GroupRow{}, fmt.Errorf("%w: failure group", ErrBadAnswer)
	}
	key, label, tone := groupStateLabel(g.GetState())
	sig := g.GetSignature()
	if len(sig) > 128 || !isHex(sig) {
		sig = ""
	}
	return GroupRow{
		ID: g.GetId(), Signature: sig, Title: CleanText(g.GetTitle(), MaxTextBytes), TestName: CleanText(g.GetTestName(), maxNameBytes),
		JobName: CleanText(g.GetJobName(), maxNameBytes), StateKey: key, State: label, Tone: tone,
		Occurrences: g.GetOccurrences(), ChangesAffected: g.GetChangesAffected(), FirstSeen: ts(g.GetFirstSeen()), LastSeen: ts(g.GetLastSeen()),
		Flaky: g.GetFlaky(), Note: CleanText(g.GetNote(), maxNoteBytes),
	}, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// GroupsPageOf checks and cleans an answer of CiInsights.ListFailureGroups.
func GroupsPageOf(resp *ascv1.ListFailureGroupsResponse) (*GroupsPage, error) {
	if len(resp.GetGroups()) > maxGroupsPage {
		return nil, fmt.Errorf("%w: too many groups", ErrBadAnswer)
	}
	if tok := resp.GetNextPageToken(); tok != "" && !tokenPattern.MatchString(tok) {
		return nil, fmt.Errorf("%w: page token", ErrBadAnswer)
	}
	page := &GroupsPage{Next: resp.GetNextPageToken()}
	for _, g := range resp.GetGroups() {
		row, err := groupRow(g)
		if err != nil {
			return nil, err
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

// GroupOf checks and cleans one group (the answer of SetFailureGroupState).
func GroupOf(g *ascv1.FailureGroup) (GroupRow, error) { return groupRow(g) }

// OccurrenceRow is one place a failure happened.
type OccurrenceRow struct {
	RunID   uint64
	JobID   uint64
	Change  string
	Version uint32
	Head    string
	Attempt uint32
	At      time.Time
	Message string
}

// GroupDetail is a group with its occurrences.
type GroupDetail struct {
	Group       GroupRow
	Occurrences []OccurrenceRow
	Next        string
}

// GroupDetailOf checks and cleans an answer of CiInsights.GetFailureGroup. The messages are runner output: they are
// cut and stripped of control characters, and the page escapes them.
func GroupDetailOf(resp *ascv1.GetFailureGroupResponse) (*GroupDetail, error) {
	g, err := groupRow(resp.GetGroup())
	if err != nil {
		return nil, err
	}
	if len(resp.GetOccurrences()) > maxOccurrences {
		return nil, fmt.Errorf("%w: too many occurrences", ErrBadAnswer)
	}
	if tok := resp.GetNextPageToken(); tok != "" && !tokenPattern.MatchString(tok) {
		return nil, fmt.Errorf("%w: page token", ErrBadAnswer)
	}
	d := &GroupDetail{Group: g, Next: resp.GetNextPageToken()}
	for _, o := range resp.GetOccurrences() {
		head := ""
		if len(o.GetHead()) > 0 {
			head = hexPrefix(o.GetHead(), 12)
		}
		d.Occurrences = append(d.Occurrences, OccurrenceRow{
			RunID: o.GetRunId(), JobID: o.GetJobId(), Change: changeText(o.GetChange()), Version: o.GetVersion(),
			Head: head, Attempt: o.GetAttempt(), At: ts(o.GetAt()), Message: CleanBlock(o.GetMessage(), MaxMessageBytes),
		})
	}
	return d, nil
}

// Cell of the history strip.
type StripCell struct {
	Day     time.Time
	Runs    uint32
	Fails   uint32
	Flips   uint32
	Tone    string // ok, no or wn (flipped)
	Label   string
	Percent int // the share of failed runs, 0 to 100
}

// History is the flaky history of one test or job.
type History struct {
	Cells     []StripCell
	Runs      uint32
	Failures  uint32
	Flips     uint32
	FlakeRate float64
	// RatePercent is the flake rate with one decimal, as text.
	RatePercent string
	Flaky       bool
	FirstFlaky  time.Time
}

// HistoryOf checks and cleans an answer of CiInsights.FlakyHistory. The strip has one cell per day, oldest first,
// at most maxStripCells; older days fold into the totals only.
func HistoryOf(resp *ascv1.FlakyHistoryResponse) (*History, error) {
	if len(resp.GetBuckets()) > maxBuckets {
		return nil, fmt.Errorf("%w: too many buckets", ErrBadAnswer)
	}
	rate := resp.GetFlakeRate()
	if rate < 0 || rate > 1 || rate != rate {
		return nil, fmt.Errorf("%w: flake rate", ErrBadAnswer)
	}
	h := &History{FlakeRate: rate, Flaky: resp.GetFlaky(), FirstFlaky: ts(resp.GetFirstFlaky()), RatePercent: fmt.Sprintf("%.1f", rate*100)}
	buckets := resp.GetBuckets()
	for _, b := range buckets {
		if b.GetFailures() > b.GetRuns() {
			return nil, fmt.Errorf("%w: bucket", ErrBadAnswer)
		}
		h.Runs += b.GetRuns()
		h.Failures += b.GetFailures()
		h.Flips += b.GetFlakyFlips()
	}
	if len(buckets) > maxStripCells {
		buckets = buckets[len(buckets)-maxStripCells:]
	}
	for _, b := range buckets {
		c := StripCell{Day: ts(b.GetStart()), Runs: b.GetRuns(), Fails: b.GetFailures(), Flips: b.GetFlakyFlips()}
		switch {
		case b.GetRuns() == 0:
			c.Tone, c.Label = "ro", "no runs"
		case b.GetFlakyFlips() > 0:
			c.Tone, c.Label = "wn", fmt.Sprintf("%d of %d failed, flipped %d times", b.GetFailures(), b.GetRuns(), b.GetFlakyFlips())
		case b.GetFailures() > 0:
			c.Tone, c.Label = "no", fmt.Sprintf("%d of %d failed", b.GetFailures(), b.GetRuns())
		default:
			c.Tone, c.Label = "ok", fmt.Sprintf("%d runs passed", b.GetRuns())
		}
		if b.GetRuns() > 0 {
			c.Percent = int(uint64(b.GetFailures()) * 100 / uint64(b.GetRuns()))
		}
		h.Cells = append(h.Cells, c)
	}
	return h, nil
}

// FlakyRow is one test in the flaky list.
type FlakyRow struct {
	TestName    string
	JobName     string
	RatePercent string
	Runs        uint32
	LastFlip    time.Time
	GroupID     uint64
}

// FlakyOf checks and cleans an answer of CiInsights.ListFlakyTests.
func FlakyOf(resp *ascv1.ListFlakyTestsResponse) ([]FlakyRow, string, error) {
	if len(resp.GetTests()) > maxFlakyPage {
		return nil, "", fmt.Errorf("%w: too many tests", ErrBadAnswer)
	}
	if tok := resp.GetNextPageToken(); tok != "" && !tokenPattern.MatchString(tok) {
		return nil, "", fmt.Errorf("%w: page token", ErrBadAnswer)
	}
	var out []FlakyRow
	for _, t := range resp.GetTests() {
		if t.GetFlakeRate() < 0 || t.GetFlakeRate() > 1 || t.GetFlakeRate() != t.GetFlakeRate() {
			return nil, "", fmt.Errorf("%w: flake rate", ErrBadAnswer)
		}
		out = append(out, FlakyRow{
			TestName: CleanText(t.GetTestName(), maxNameBytes), JobName: CleanText(t.GetJobName(), maxNameBytes),
			RatePercent: fmt.Sprintf("%.1f", t.GetFlakeRate()*100), Runs: t.GetRuns(), LastFlip: ts(t.GetLastFlip()), GroupID: t.GetGroupId(),
		})
	}
	return out, resp.GetNextPageToken(), nil
}

// CheckGroupNote checks the note of a state change: plain text of at most 1,024 bytes.
func CheckGroupNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if len(note) > maxNoteBytes || CleanBlock(note, maxNoteBytes) != strings.ReplaceAll(note, "\r\n", "\n") {
		return "", fmt.Errorf("%w: the note is at most %d bytes of plain text", ErrBadInput, maxNoteBytes)
	}
	return note, nil
}

// CheckName checks a test or job name taken from a form: 1 to 512 bytes of plain text.
func CheckName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameBytes || CleanText(name, maxNameBytes) != name {
		return "", fmt.Errorf("%w: a name is 1 to %d bytes of plain text", ErrBadInput, maxNameBytes)
	}
	return name, nil
}

// WindowDays reads the window of a history from a form value: 7, 14, 30, 90 or 180 days. The default is 30.
func WindowDays(s string) (uint32, bool) {
	switch s {
	case "":
		return flakyDefaultWindow, true
	case "7":
		return 7, true
	case "14":
		return 14, true
	case "30":
		return 30, true
	case "90":
		return 90, true
	case "180":
		return 180, true
	}
	return 0, false
}
