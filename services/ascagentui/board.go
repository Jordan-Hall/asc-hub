// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// View models of the agent boards (the runs list, the run page, spend and the approvals log). Everything that comes
// from asc is cleaned here, so a template only escapes.

// Limits on what a board draws from one answer.
const (
	MaxRequests   = 200
	MaxPlanSteps  = 64
	MaxRosterRows = 100
	MaxSpendDays  = 62
	MaxCheckpoint = 200
)

// Decision words of the request forms.
const (
	DecisionAllow   = "allow"
	DecisionDeny    = "deny"
	DecisionApprove = "approve"
	DecisionReject  = "reject"
)

// ParseDecision maps the word of a form to the wire decision and the request kind it is valid for.
func ParseDecision(word string) (ascv1.AgentDecision, ascv1.AgentRequestKind, bool) {
	switch word {
	case DecisionAllow:
		return ascv1.AgentDecision_AGENT_DECISION_ALLOW_ONCE, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION, true
	case DecisionDeny:
		return ascv1.AgentDecision_AGENT_DECISION_DENY, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION, true
	case DecisionApprove:
		return ascv1.AgentDecision_AGENT_DECISION_APPROVE_PLAN, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN, true
	case DecisionReject:
		return ascv1.AgentDecision_AGENT_DECISION_REJECT_PLAN, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN, true
	}
	return ascv1.AgentDecision_AGENT_DECISION_UNSPECIFIED, ascv1.AgentRequestKind_AGENT_REQUEST_KIND_UNSPECIFIED, false
}

// PlanStep is one step of a proposed or approved plan.
type PlanStep struct {
	Index int
	Text  string
	State string // todo, doing or done
}

// Request is a permission or plan request as drawn.
type Request struct {
	ID        uint64
	RunID     uint64
	Kind      string // permission or plan
	State     string // pending, allowed, denied, approved, rejected or expired
	Pending   bool
	Summary   string
	Detail    string
	Agent     string
	Initials  string
	Steps     []PlanStep
	At        time.Time
	DecidedBy string
	DecidedAt time.Time
	Reason    string
}

var requestStates = map[ascv1.AgentRequestState]string{
	ascv1.AgentRequestState_AGENT_REQUEST_STATE_PENDING:  "pending",
	ascv1.AgentRequestState_AGENT_REQUEST_STATE_ALLOWED:  "allowed",
	ascv1.AgentRequestState_AGENT_REQUEST_STATE_DENIED:   "denied",
	ascv1.AgentRequestState_AGENT_REQUEST_STATE_APPROVED: "approved",
	ascv1.AgentRequestState_AGENT_REQUEST_STATE_REJECTED: "rejected",
	ascv1.AgentRequestState_AGENT_REQUEST_STATE_EXPIRED:  "expired",
}

// RequestOf cleans a request from asc. A request without an id or a run, or of an unknown kind, is refused.
func RequestOf(r *ascv1.AgentRequest) (Request, bool) {
	if r == nil || r.GetId() == 0 || r.GetRunId() == 0 {
		return Request{}, false
	}
	out := Request{
		ID:        r.GetId(),
		RunID:     r.GetRunId(),
		Summary:   CleanLine(r.GetSummary(), MaxLineText),
		Detail:    CleanText(r.GetDetail(), 1024),
		Agent:     SafeName(r.GetAgent()),
		At:        Time(r.GetRequestedAt()),
		DecidedBy: SafeName(r.GetDecidedBy()),
		DecidedAt: Time(r.GetDecidedAt()),
		Reason:    CleanLine(r.GetReason(), 512),
	}
	switch r.GetKind() {
	case ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PERMISSION:
		out.Kind = "permission"
	case ascv1.AgentRequestKind_AGENT_REQUEST_KIND_PLAN:
		out.Kind = "plan"
	default:
		return Request{}, false
	}
	st, ok := requestStates[r.GetState()]
	if !ok {
		return Request{}, false
	}
	out.State = st
	out.Pending = st == "pending"
	out.Initials = initialsOf(out.Agent)
	for i, s := range r.GetPlan() {
		if i >= MaxPlanSteps {
			break
		}
		state := s.GetState()
		if state != "todo" && state != "doing" && state != "done" {
			state = "todo"
		}
		out.Steps = append(out.Steps, PlanStep{Index: i + 1, Text: CleanLine(s.GetText(), MaxLineText), State: state})
	}
	return out, true
}

// Requests cleans a list. A malformed row is skipped, never drawn.
func Requests(in []*ascv1.AgentRequest) []Request {
	var out []Request
	for _, r := range in {
		if len(out) >= MaxRequests {
			break
		}
		if q, ok := RequestOf(r); ok {
			out = append(out, q)
		}
	}
	return out
}

// RequestCounts counts the pending requests by kind.
func RequestCounts(rs []Request) (permissions, plans int) {
	for _, r := range rs {
		if !r.Pending {
			continue
		}
		if r.Kind == "plan" {
			plans++
		} else {
			permissions++
		}
	}
	return
}

func initialsOf(name string) string {
	r := []rune(strings.ToLower(name))
	if len(r) == 0 {
		return "?"
	}
	if last := r[len(r)-1]; len(r) > 1 && last >= '0' && last <= '9' {
		return string([]rune{r[0], last})
	}
	if len(r) > 1 {
		return string(r[:2])
	}
	return string(r)
}

// Roster is one agent of the "Agents on this repository" card.
type Roster struct {
	Name     string
	Initials string
	Scope    string // "src/**, tests/**" or "read-only"
	Status   string // running, waiting or idle
	Runs     uint32
	Landed   uint32
	Spend    string
}

// RosterOf cleans the agents of a repository, busiest first as asc sent them.
func RosterOf(in []*ascv1.RepoAgentInfo) []Roster {
	var out []Roster
	for _, a := range in {
		if len(out) >= MaxRosterRows {
			break
		}
		name := SafeName(a.GetAgent())
		if name == "" {
			continue
		}
		scope := make([]string, 0, len(a.GetPaths()))
		for _, p := range a.GetPaths() {
			if p = CleanLine(p, MaxGlobBytes); p != "" {
				scope = append(scope, p)
			}
		}
		r := Roster{Name: name, Initials: initialsOf(name), Runs: a.GetRuns(), Landed: a.GetLanded_30D(), Spend: FormatUSD(a.GetCostMicroUsdMonth())}
		if len(scope) == 0 {
			r.Scope = "read-only"
		} else {
			r.Scope = strings.Join(scope, ", ")
		}
		switch {
		case a.GetPendingRequests() > 0:
			r.Status = "waiting"
		case a.GetActive() > 0:
			r.Status = "running"
		default:
			r.Status = "idle"
		}
		out = append(out, r)
	}
	return out
}

// Percent is part of whole, 0 to 100, in integer arithmetic. A zero whole is 0.
func Percent(part, whole uint64) int {
	if whole == 0 {
		return 0
	}
	if part >= whole {
		return 100
	}
	hi, lo := bits.Mul64(part, 100)
	q, _ := bits.Div64(hi, lo, whole)
	return int(q)
}

// Progress is the step bar of a run.
type Progress struct {
	Done, Total int
	Segments    []bool // one per step, true when done; at most 8 are drawn
	Known       bool
}

// ProgressOf draws "3 of 4 steps". A run without a plan has no total: the done count is shown as turns.
func ProgressOf(p *ascv1.AgentRunProgress) Progress {
	if p == nil {
		return Progress{}
	}
	out := Progress{Done: int(min(p.GetStepsDone(), 100000)), Total: int(min(p.GetStepsTotal(), 100000)), Known: p.GetStepsTotal() > 0}
	if out.Known {
		n := min(out.Total, 8)
		out.Segments = make([]bool, n)
		for i := range n {
			// Scale to the drawn count so a 20-step plan still shows its share.
			out.Segments[i] = (i+1)*out.Total <= out.Done*n
		}
	}
	return out
}

// Sparkline turns a series into the points of an SVG polyline in a width x height box. An empty or flat series
// gives a flat line. The numbers are formatted here, so a template never builds markup from them.
func Sparkline(values []uint64, width, height int) string {
	if len(values) == 0 {
		return ""
	}
	var maxV uint64
	for _, v := range values {
		maxV = max(maxV, v)
	}
	var b strings.Builder
	for i, v := range values {
		x := 0
		if len(values) > 1 {
			x = i * width / (len(values) - 1)
		}
		y := height - 1
		if maxV > 0 {
			y = height - 1 - int(uint64(height-2)*v/maxV)
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.Itoa(x) + "," + strconv.Itoa(y))
	}
	return b.String()
}

// SpendDay is one day of the spend chart.
type SpendDay struct {
	Day    string
	Cost   string
	Tokens string
	Pct    int // of the largest day
	Micro  uint64
}

// SpendAgent is the spend of one agent in the month.
type SpendAgent struct {
	Name     string
	Initials string
	Cost     string
	Tokens   string
	Requests uint64
	Cap      string // "" when there is no cap
	CapPct   int
}

// Spend is the month of a repository as drawn.
type Spend struct {
	Month    string
	Cost     string
	Micro    uint64
	Tokens   string
	Cap      string // "" when none
	CapMicro uint64
	CapPct   int
	Days     []SpendDay
	Agents   []SpendAgent
	Spark    string
	Over     bool
}

// SpendOf cleans the answer of GetSpend. The month must look like YYYY-MM and the days like YYYY-MM-DD.
func SpendOf(r *ascv1.GetAgentSpendResponse) Spend {
	out := Spend{
		Month:    cleanMonth(r.GetMonth()),
		Micro:    r.GetCostMicroUsd(),
		Cost:     FormatUSD(r.GetCostMicroUsd()),
		Tokens:   FormatCount(r.GetTokens()),
		CapMicro: r.GetMonthlyCapMicroUsd(),
	}
	if out.CapMicro > 0 {
		out.Cap = FormatUSD(out.CapMicro)
		out.CapPct = Percent(out.Micro, out.CapMicro)
		out.Over = out.Micro >= out.CapMicro
	}
	var peak uint64
	days := r.GetDays()
	if len(days) > MaxSpendDays {
		days = days[len(days)-MaxSpendDays:]
	}
	for _, d := range days {
		peak = max(peak, d.GetCostMicroUsd())
	}
	series := make([]uint64, 0, len(days))
	for _, d := range days {
		day := cleanDay(d.GetDay())
		if day == "" {
			continue
		}
		series = append(series, d.GetCostMicroUsd())
		out.Days = append(out.Days, SpendDay{Day: day, Cost: FormatUSD(d.GetCostMicroUsd()), Tokens: FormatCount(d.GetTokens()), Pct: Percent(d.GetCostMicroUsd(), peak), Micro: d.GetCostMicroUsd()})
	}
	if len(series) > 1 {
		out.Spark = Sparkline(series, 100, 28)
	}
	for _, a := range r.GetAgents() {
		name := SafeName(a.GetAgent())
		if name == "" || len(out.Agents) >= MaxRosterRows {
			continue
		}
		sa := SpendAgent{Name: name, Initials: initialsOf(name), Cost: FormatUSD(a.GetCostMicroUsd()), Tokens: FormatCount(a.GetTokens()), Requests: a.GetRequests()}
		if c := a.GetMonthlyCapMicroUsd(); c > 0 {
			sa.Cap = FormatUSD(c)
			sa.CapPct = Percent(a.GetCostMicroUsd(), c)
		}
		out.Agents = append(out.Agents, sa)
	}
	return out
}

func cleanMonth(s string) string {
	if len(s) != 7 || s[4] != '-' {
		return ""
	}
	if _, err := time.Parse("2006-01", s); err != nil {
		return ""
	}
	return s
}

func cleanDay(s string) string {
	if len(s) != 10 {
		return ""
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

// MonthDayOf is the 1-based day of the month of `now` in UTC, for "day 19".
func MonthDayOf(now time.Time) int { return now.UTC().Day() }

// Checkpoint is one point a paused run can go back to.
type Checkpoint struct {
	ID         uint64
	Seq        uint32
	Label      string
	At         time.Time
	Changes    int
	Tip        string
	RolledBack bool
	RolledBy   string
	RolledAt   time.Time
}

// CheckpointsOf cleans the checkpoints of a run, newest first.
func CheckpointsOf(in []*ascv1.AgentCheckpoint, runID uint64) []Checkpoint {
	var out []Checkpoint
	for _, c := range in {
		if c.GetId() == 0 || c.GetRunId() != runID || len(out) >= MaxCheckpoint {
			continue
		}
		tip := ""
		if t := c.GetTip(); len(t) > 0 {
			tip = fmt.Sprintf("%x", t)
			if len(tip) > 10 {
				tip = tip[:10]
			}
		}
		out = append(out, Checkpoint{
			ID: c.GetId(), Seq: c.GetSeq(), Label: CleanLine(c.GetLabel(), MaxLineText), At: Time(c.GetAt()),
			Changes: len(c.GetChanges()), Tip: tip, RolledBack: c.GetRolledBackAt() > 0,
			RolledBy: SafeName(c.GetRolledBackBy()), RolledAt: Time(c.GetRolledBackAt()),
		})
	}
	// asc sends them in creation order; the card shows the newest on top.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
