// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"fmt"
	"math/bits"
	"regexp"
	"strconv"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// Upper bounds the forms accept. asc applies the policy and instance limits on top; these only keep a typo from
// reaching it.
const (
	maxBudgetChanges = 10_000
	maxBudgetTokens  = 1_000_000_000_000
	maxBudgetUSD     = 100_000
	maxBudgetPaths   = 1_000_000
	maxBudgetMinutes = 7 * 24 * 60
)

var usdRe = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,6})?$`)

// BudgetInput is the budget fields of a form, as typed. An empty field means "the default" (0 on the wire).
type BudgetInput struct {
	Changes string
	Tokens  string
	CostUSD string
	Paths   string
	Minutes string
}

// BudgetFields names the form fields of a BudgetInput, so a handler can read them and a template can show them again.
var BudgetFields = struct{ Changes, Tokens, CostUSD, Paths, Minutes string }{
	Changes: "max_changes", Tokens: "max_tokens", CostUSD: "max_cost_usd", Paths: "max_paths", Minutes: "max_minutes",
}

// BudgetError names the field that failed.
type BudgetError struct{ Field string }

func (e *BudgetError) Error() string { return "budget field " + e.Field + " is not valid" }

// ParseCount reads a whole number of at most max; the empty string is 0 (the default). Digits only: no sign, no
// separators.
func ParseCount(s string, max uint64) (uint64, bool) {
	return parseCount(s, max)
}

func parseCount(s string, max uint64) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	// Digits only, no sign, no separators; 20 digits would overflow uint64 anyway.
	if len(s) > 15 {
		return 0, false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n > max {
		return 0, false
	}
	return n, true
}

// ParseUSD turns "1.25" into micro-USD without floating point. The empty string is 0 (the default).
func ParseUSD(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	if !usdRe.MatchString(s) {
		return 0, false
	}
	whole, frac, _ := strings.Cut(s, ".")
	w, err := strconv.ParseUint(whole, 10, 64)
	if err != nil || w > maxBudgetUSD {
		return 0, false
	}
	frac += strings.Repeat("0", 6-len(frac))
	f, err := strconv.ParseUint(frac, 10, 64)
	if err != nil {
		return 0, false
	}
	micro := w*1_000_000 + f
	if micro > maxBudgetUSD*1_000_000 {
		return 0, false
	}
	return micro, true
}

// ParseBudget checks the budget fields and builds the limits. Zero fields stay zero, which asc reads as the
// policy default; the hub never invents a limit and never raises one.
func ParseBudget(in BudgetInput) (*ascv1.AgentBudgetLimits, error) {
	changes, ok := parseCount(in.Changes, maxBudgetChanges)
	if !ok {
		return nil, &BudgetError{BudgetFields.Changes}
	}
	tokens, ok := parseCount(in.Tokens, maxBudgetTokens)
	if !ok {
		return nil, &BudgetError{BudgetFields.Tokens}
	}
	cost, ok := ParseUSD(in.CostUSD)
	if !ok {
		return nil, &BudgetError{BudgetFields.CostUSD}
	}
	paths, ok := parseCount(in.Paths, maxBudgetPaths)
	if !ok {
		return nil, &BudgetError{BudgetFields.Paths}
	}
	minutes, ok := parseCount(in.Minutes, maxBudgetMinutes)
	if !ok {
		return nil, &BudgetError{BudgetFields.Minutes}
	}
	return &ascv1.AgentBudgetLimits{
		MaxChanges:        uint32(changes),
		MaxTokens:         tokens,
		MaxCostMicroUsd:   cost,
		MaxPaths:          uint32(paths),
		MaxRuntimeSeconds: uint32(minutes * 60),
	}, nil
}

// Meter is one line of a budget panel.
type Meter struct {
	Key       string `json:"key"` // changes, tokens, cost, paths or time
	Label     string `json:"label"`
	Used      string `json:"used"`
	Limit     string `json:"limit"`   // "" when there is no limit
	Percent   int    `json:"percent"` // 0 to 100, 0 when there is no limit
	Unlimited bool   `json:"unlimited"`
	Warn      bool   `json:"warn"` // 80 percent or more
	Over      bool   `json:"over"` // the limit is reached or passed
	Idle      bool   `json:"idle"` // no limit and nothing used: a list may leave the line out
}

// Meters builds the budget panel. Limits and usage may be nil.
func Meters(limits *ascv1.AgentBudgetLimits, usage *ascv1.AgentBudgetUsage) []Meter {
	return []Meter{
		meter("tokens", "Tokens", usage.GetTokens(), limits.GetMaxTokens(), FormatCount),
		meter("cost", "Cost", usage.GetCostMicroUsd(), limits.GetMaxCostMicroUsd(), FormatUSD),
		meter("changes", "Changes", uint64(usage.GetChanges()), uint64(limits.GetMaxChanges()), FormatCount),
		meter("time", "Time", uint64(usage.GetRuntimeSeconds()), uint64(limits.GetMaxRuntimeSeconds()), FormatDuration),
		meter("paths", "Paths", uint64(usage.GetPaths()), uint64(limits.GetMaxPaths()), FormatCount),
	}
}

func meter(key, label string, used, limit uint64, format func(uint64) string) Meter {
	m := Meter{Key: key, Label: label, Used: format(used)}
	if limit == 0 {
		m.Unlimited = true
		m.Idle = used == 0
		return m
	}
	m.Limit = format(limit)
	switch {
	case used >= limit:
		m.Percent, m.Over, m.Warn = 100, true, true
	default:
		// used < limit here, so used*100/limit is below 100 and the 128-bit division cannot overflow. Integer
		// arithmetic keeps 116000 of 200000 at exactly 58, which a float would not.
		hi, lo := bits.Mul64(used, 100)
		q, _ := bits.Div64(hi, lo, limit)
		m.Percent = int(q)
		m.Warn = m.Percent >= 80
	}
	return m
}

// FormatCount writes a count with a k, M or G suffix once it is large: 116000 is "116k".
func FormatCount(n uint64) string {
	switch {
	case n < 10_000:
		return strconv.FormatUint(n, 10)
	case n < 1_000_000:
		return trimZero(float64(n)/1e3) + "k"
	case n < 1_000_000_000:
		return trimZero(float64(n)/1e6) + "M"
	default:
		return trimZero(float64(n)/1e9) + "G"
	}
}

func trimZero(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// FormatUSD writes micro-USD as dollars with at least two decimals: 1250000 is "$1.25".
func FormatUSD(micro uint64) string {
	whole, frac := micro/1_000_000, micro%1_000_000
	s := fmt.Sprintf("%06d", frac)
	s = strings.TrimRight(s, "0")
	for len(s) < 2 {
		s += "0"
	}
	return "$" + strconv.FormatUint(whole, 10) + "." + s
}

// FormatDuration writes seconds as "45 s", "6 min" or "2 h 5 min".
func FormatDuration(sec uint64) string {
	switch {
	case sec < 60:
		return strconv.FormatUint(sec, 10) + " s"
	case sec < 3600:
		return strconv.FormatUint(sec/60, 10) + " min"
	default:
		h, m := sec/3600, (sec%3600)/60
		if m == 0 {
			return strconv.FormatUint(h, 10) + " h"
		}
		return fmt.Sprintf("%d h %d min", h, m)
	}
}

// LoweredText says which limits asc lowered to the policy cap, with the cap and the value that was asked when both are
// known (board AgentNew: "Limit lowered. Your policy caps a run at $5.00. The limit was set to $8.00, so it was
// lowered."). lowered is the answer of asc, asked the limits of the request and got the limits of the run asc answered;
// a limit without both values, or a name asc did not use before, is named only. "" when nothing was lowered.
func LoweredText(lowered []string, asked, got *ascv1.AgentBudgetLimits) string {
	var caps, sets, named []string
	for _, code := range lowered {
		code = SafeCode(code)
		if code == "" {
			continue
		}
		c, a, ok := loweredValues(code, asked, got)
		if ok {
			caps, sets = append(caps, c), append(sets, a)
		} else {
			named = append(named, code)
		}
	}
	var out string
	switch len(caps) {
	case 0:
	case 1:
		out = "Limit lowered. Your policy caps a run at " + caps[0] + ". The limit was set to " + sets[0] + ", so it was lowered."
	default:
		out = "Limits lowered. Your policy caps a run at " + andList(caps) + ". The limits were set to " + andList(sets) +
			", so they were lowered."
	}
	if len(named) > 0 {
		if out != "" {
			out += " "
		}
		out += "Lowered to the policy cap: " + strings.Join(named, ", ") + "."
	}
	return out
}

// loweredValues writes the cap (from got) and the asked value of one limit; false when either is not set.
func loweredValues(code string, asked, got *ascv1.AgentBudgetLimits) (string, string, bool) {
	var a, g uint64
	var format func(uint64) string
	switch code {
	case "changes":
		a, g, format = uint64(asked.GetMaxChanges()), uint64(got.GetMaxChanges()), countOf("change", "changes")
	case "tokens":
		a, g, format = asked.GetMaxTokens(), got.GetMaxTokens(), countOf("token", "tokens")
	case "cost":
		a, g, format = asked.GetMaxCostMicroUsd(), got.GetMaxCostMicroUsd(), FormatUSD
	case "paths":
		a, g, format = uint64(asked.GetMaxPaths()), uint64(got.GetMaxPaths()), countOf("path", "paths")
	case "runtime":
		a, g, format = uint64(asked.GetMaxRuntimeSeconds()), uint64(got.GetMaxRuntimeSeconds()), FormatDuration
	default:
		return "", "", false
	}
	if a == 0 || g == 0 || g >= a {
		return "", "", false
	}
	return format(g), format(a), true
}

func countOf(one, many string) func(uint64) string {
	return func(n uint64) string {
		if n == 1 {
			return "1 " + one
		}
		return FormatCount(n) + " " + many
	}
}

func andList(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
