// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"slices"
	"strings"
)

// The Form view of the policy page: the keys a person most often changes, as fields over the text of the policy.
// Reading is by the line-based reader of toml.go and writing replaces the lines of one key (or adds the key or its
// section), so comments, order and every key the form does not know stay as they are. asc parses the result
// strictly in Policy.Validate and Policy.Propose; the form is never the authority.

// PolicyForm is the fields of the Form view.
type PolicyForm struct {
	RequireApprovals int64
	RequireChecks    []string
	AllowForce       bool
	AuthorMayApprove bool // [review] author_may_approve, off by default
	KeepApprovals    bool // [review] keep_approvals_on_restack, on by default
	RequireSigned    bool
	MergeStrategy    string // rebase | rebase-disjoint | fast-forward, "" for the default
	MergeQueue       bool
	MergeBatch       int64 // [merge] batch, 1 to MaxMergeBatch; 0 when the file does not say
	PostMergeChecks  bool
	AgentsEnabled    bool
	AgentScope       []string
	AgentDenied      []string
	MaxRuntimeSecs   int64
	MaxChangesPerRun int64
	MaxTokens        int64
	MaxStackDepth    int64 // [stacks] max_depth, 1 to MaxStackDepth; 0 for no limit
	CleanupDays      int64 // [stacks] cleanup_days, 1 to MaxCleanupDays; 0 for the server's setting
	RestackOnMove    bool  // [stacks] restack_on_main_move, off by default; the hub restacks open stacks
	WritersMayPush   bool  // [stacks] collaborators_may_push, off by default; writers may update any stack
	// HumanApproval is [review] human_approval, on by default: an agent's approval does not count. The form shows it
	// locked; only the TOML view changes it, and ApplyForm leaves it as the text says.
	HumanApproval bool
}

// MaxRuntimeMinutes is MaxRuntimeSecs in whole minutes, rounded up, for the minutes field of the form.
func (f PolicyForm) MaxRuntimeMinutes() int64 {
	if f.MaxRuntimeSecs <= 0 {
		return 0
	}
	return (f.MaxRuntimeSecs + 59) / 60
}

// RuntimeSecsOf is the max_runtime_seconds the minutes field means. was is the value the form was drawn with: when
// the minutes are what was shows (rounded up), the seconds stay as they were, so a policy of 90 seconds is not turned
// into 120 by saving the form unchanged. A negative minutes value stays negative, for ApplyForm to refuse.
func RuntimeSecsOf(minutes, was int64) int64 {
	if minutes < 0 {
		return -1
	}
	if was > 0 && minutes == (PolicyForm{MaxRuntimeSecs: was}).MaxRuntimeMinutes() {
		return was
	}
	if minutes > (1<<62)/60 {
		return -1
	}
	return minutes * 60
}

// ListOf joins the entries of a list field of the form: the ticked chips (one value each) and the text of its Add
// field (one entry per line, commas also split, in a chip as well). Blanks and repeats are left out; the order is kept.
func ListOf(chips []string, add string) []string {
	var out []string
	seen := map[string]bool{}
	var all []string
	for _, c := range chips {
		all = append(all, SplitLines(c)...)
	}
	for _, v := range append(all, SplitLines(add)...) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// MaxMergeBatch is the largest `[merge] batch` of asc-core (MAX_BATCH).
const MaxMergeBatch = 20

// MaxStackDepth and MaxCleanupDays are the largest `[stacks] max_depth` and `cleanup_days` of asc-core.
const (
	MaxStackDepth  = 100
	MaxCleanupDays = 3650
)

// MergeStrategies are the values of `[merge] strategy`. The first three keep every change of a stack;
// "squash" lands the stack as one change.
var MergeStrategies = []string{"rebase", "rebase-disjoint", "fast-forward", "squash"}

// FormOfText reads the fields of the form from a policy text.
func FormOfText(text string) PolicyForm {
	var f PolicyForm
	n := func(section, key string) int64 {
		v, _ := TomlInt(text, section, key)
		return int64(v)
	}
	f.RequireApprovals = n("trunk", "require_approvals")
	f.RequireChecks, _ = TomlStrings(text, "trunk", "require_checks")
	f.AllowForce, _ = TomlBool(text, "trunk", "allow_force")
	f.AuthorMayApprove, _ = TomlBool(text, "review", "author_may_approve")
	f.KeepApprovals = true
	if v, ok := TomlBool(text, "review", "keep_approvals_on_restack"); ok {
		f.KeepApprovals = v
	}
	if v, ok := TomlBool(text, "signing", "require_signed"); ok {
		f.RequireSigned = v
	} else {
		f.RequireSigned, _ = TomlBool(text, "trunk", "require_signed")
	}
	f.MergeQueue, _ = TomlBool(text, "merge", "queue")
	f.MergeBatch = n("merge", "batch")
	f.PostMergeChecks, _ = TomlBool(text, "merge", "post_merge_checks")
	f.AgentsEnabled = true
	if v, ok := TomlBool(text, "agents", "enabled"); ok {
		f.AgentsEnabled = v
	}
	f.AgentScope, _ = TomlStrings(text, "agents", "default_scope")
	f.AgentDenied, _ = TomlStrings(text, "agents", "denied_paths")
	f.MaxRuntimeSecs = n("agents", "max_runtime_seconds")
	f.MaxChangesPerRun = n("agents.budgets", "max_changes_per_run")
	f.MaxTokens = n("agents.budgets", "max_tokens")
	f.MaxStackDepth = n("stacks", "max_depth")
	f.CleanupDays = n("stacks", "cleanup_days")
	f.RestackOnMove, _ = TomlBool(text, "stacks", "restack_on_main_move")
	f.WritersMayPush, _ = TomlBool(text, "stacks", "collaborators_may_push")
	f.MergeStrategy, _ = tomlString(text, "merge", "strategy")
	f.HumanApproval = true
	if v, ok := TomlBool(text, "review", "human_approval"); ok {
		f.HumanApproval = v
	}
	return f
}

// tomlString reads a single-line string key of a section.
func tomlString(text, section, key string) (string, bool) {
	d := parseToml(text)
	k, ok := d.find(section, key)
	if !ok {
		return "", false
	}
	raw := strings.TrimSpace(d.raw(k))
	if len(raw) >= 2 && (raw[0] == '"' && raw[len(raw)-1] == '"' || raw[0] == '\'' && raw[len(raw)-1] == '\'') {
		return raw[1 : len(raw)-1], true
	}
	return "", false
}

// SplitLines turns the text of a list field (one entry per line, commas also split) into a list without blanks.
func SplitLines(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ApplyForm returns text with the fields of the form set. A value the form cannot say is refused with ErrBadInput
// and a sentence naming the field.
func ApplyForm(text string, f PolicyForm) (string, error) {
	if f.RequireApprovals < 0 || f.RequireApprovals > 64 {
		return "", fmt.Errorf("%w: approvals is 0 to 64", ErrBadInput)
	}
	if f.MaxRuntimeSecs < 0 || f.MaxChangesPerRun < 0 || f.MaxTokens < 0 {
		return "", fmt.Errorf("%w: a budget is a whole number, 0 or more", ErrBadInput)
	}
	if f.MergeStrategy != "" && !slices.Contains(MergeStrategies, f.MergeStrategy) {
		return "", fmt.Errorf("%w: merge strategy is one of %s", ErrBadInput, strings.Join(MergeStrategies, ", "))
	}
	for _, g := range slices.Concat(f.AgentScope, f.AgentDenied) {
		if err := ValidatePathGlob(g); err != nil {
			return "", fmt.Errorf("%w: path pattern %q: %v", ErrBadInput, g, err)
		}
	}
	if len(f.AgentScope) > MaxGlobs || len(f.AgentDenied) > MaxGlobs {
		return "", fmt.Errorf("%w: at most %d path patterns per list", ErrBadInput, MaxGlobs)
	}
	if f.MaxStackDepth < 0 || f.MaxStackDepth > MaxStackDepth {
		return "", fmt.Errorf("%w: longest stack is 1 to %d changes, or empty for no limit", ErrBadInput, MaxStackDepth)
	}
	if f.CleanupDays < 0 || f.CleanupDays > MaxCleanupDays {
		return "", fmt.Errorf("%w: clean-up is 1 to %d days, or empty for the server's setting", ErrBadInput, MaxCleanupDays)
	}
	for _, c := range f.RequireChecks {
		if c == "" || len(c) > 128 || strings.ContainsAny(c, "\n\r\x00\"") {
			return "", fmt.Errorf("%w: a check name is 1 to 128 characters without quotes", ErrBadInput)
		}
	}
	text = setLine(text, "trunk", "require_approvals", fmt.Sprint(f.RequireApprovals))
	checks := f.RequireChecks
	if checks == nil {
		checks = []string{}
	}
	text = TomlSetStrings(text, "trunk", "require_checks", checks)
	// A key at its default is not written into a file that does not have it, so a review shows only what changed.
	if f.AllowForce || hasKey(text, "trunk", "allow_force") {
		text = TomlSetBool(text, "trunk", "allow_force", f.AllowForce)
	}
	if f.AuthorMayApprove || hasKey(text, "review", "author_may_approve") {
		text = TomlSetBool(text, "review", "author_may_approve", f.AuthorMayApprove)
	}
	if !f.KeepApprovals || hasKey(text, "review", "keep_approvals_on_restack") {
		text = TomlSetBool(text, "review", "keep_approvals_on_restack", f.KeepApprovals)
	}
	// `[trunk] require_signed` is a synonym of `[signing] require_signed` and the two are ORed by asc, so a form that
	// turns signing off must turn both off; one that turns it on writes the section the file already uses.
	switch {
	case hasKey(text, "trunk", "require_signed") && !hasKey(text, "signing", "require_signed"):
		text = TomlSetBool(text, "trunk", "require_signed", f.RequireSigned)
	case f.RequireSigned || hasKey(text, "signing", "require_signed"):
		text = TomlSetBool(text, "signing", "require_signed", f.RequireSigned)
		if hasKey(text, "trunk", "require_signed") {
			text = TomlSetBool(text, "trunk", "require_signed", f.RequireSigned)
		}
	}
	if f.MergeStrategy != "" {
		text = setLine(text, "merge", "strategy", tomlQuote(f.MergeStrategy))
	}
	if f.MergeBatch < 0 || f.MergeBatch > MaxMergeBatch {
		return "", fmt.Errorf("%w: batch size is 1 to %d", ErrBadInput, MaxMergeBatch)
	}
	if f.MergeBatch > 1 || (f.MergeBatch == 1 && hasKey(text, "merge", "batch")) {
		text = setLine(text, "merge", "batch", fmt.Sprint(f.MergeBatch))
	}
	if f.PostMergeChecks || hasKey(text, "merge", "post_merge_checks") {
		text = TomlSetBool(text, "merge", "post_merge_checks", f.PostMergeChecks)
	}
	if f.MergeQueue || hasKey(text, "merge", "queue") {
		text = TomlSetBool(text, "merge", "queue", f.MergeQueue)
	}
	if !f.AgentsEnabled || hasKey(text, "agents", "enabled") {
		text = TomlSetBool(text, "agents", "enabled", f.AgentsEnabled)
	}
	if len(f.AgentScope) > 0 || hasKey(text, "agents", "default_scope") {
		text = TomlSetStrings(text, "agents", "default_scope", f.AgentScope)
	}
	if len(f.AgentDenied) > 0 || hasKey(text, "agents", "denied_paths") {
		text = TomlSetStrings(text, "agents", "denied_paths", f.AgentDenied)
	}
	if f.MaxRuntimeSecs > 0 || hasKey(text, "agents", "max_runtime_seconds") {
		text = setLine(text, "agents", "max_runtime_seconds", fmt.Sprint(f.MaxRuntimeSecs))
	}
	if f.MaxChangesPerRun > 0 || hasKey(text, "agents.budgets", "max_changes_per_run") {
		text = setLine(text, "agents.budgets", "max_changes_per_run", fmt.Sprint(f.MaxChangesPerRun))
	}
	if f.MaxTokens > 0 || hasKey(text, "agents.budgets", "max_tokens") {
		text = setLine(text, "agents.budgets", "max_tokens", fmt.Sprint(f.MaxTokens))
	}
	// Both keys mean "not set" when left out, so an empty field takes the key out rather than writing 0, which asc refuses.
	for _, s := range []struct {
		key string
		v   int64
	}{{"max_depth", f.MaxStackDepth}, {"cleanup_days", f.CleanupDays}} {
		if s.v > 0 {
			text = setLine(text, "stacks", s.key, fmt.Sprint(s.v))
		} else {
			text = removeKey(text, "stacks", s.key)
		}
	}
	if f.RestackOnMove || hasKey(text, "stacks", "restack_on_main_move") {
		text = TomlSetBool(text, "stacks", "restack_on_main_move", f.RestackOnMove)
	}
	if f.WritersMayPush || hasKey(text, "stacks", "collaborators_may_push") {
		text = TomlSetBool(text, "stacks", "collaborators_may_push", f.WritersMayPush)
	}
	return text, nil
}

func hasKey(text, section, key string) bool {
	_, ok := parseToml(text).find(normalizeSection(section), key)
	return ok
}

// MaintainerNames lists who the `[roles] maintainer` list names, for a sentence that says whom to ask: a user or a
// team is shown by its name, the prefix of the subject kind dropped.
func MaintainerNames(text string) []string {
	vals, _ := TomlStrings(text, "roles", "maintainer")
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if _, name, ok := strings.Cut(v, ":"); ok && name != "" {
			v = name
		}
		out = append(out, v)
	}
	return out
}

// DiffLine is one line of the Changes view: Op is ' ' for a line both texts have, '-' for a line only the policy in
// force has and '+' for a line only the draft has.
type DiffLine struct {
	Op   string
	Text string
}

// maxDiffLines bounds the lines compared; a larger policy is shown as a whole replacement of its changed tail.
const maxDiffLines = 2000

// LineDiff compares two texts line by line (longest common subsequence) and returns the lines with their marks.
func LineDiff(before, after string) []DiffLine {
	a := strings.Split(strings.TrimRight(strings.ReplaceAll(before, "\r\n", "\n"), "\n"), "\n")
	b := strings.Split(strings.TrimRight(strings.ReplaceAll(after, "\r\n", "\n"), "\n"), "\n")
	if len(a) > maxDiffLines {
		a = a[:maxDiffLines]
	}
	if len(b) > maxDiffLines {
		b = b[:maxDiffLines]
	}
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and b[j:]
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{" ", a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{"-", a[i]})
			i++
		default:
			out = append(out, DiffLine{"+", b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, DiffLine{"-", a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, DiffLine{"+", b[j]})
	}
	return out
}
