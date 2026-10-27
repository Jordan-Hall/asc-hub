// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

func itoa(n int) string { return strconv.Itoa(n) }

// PolicyInput is everything the policy page draws from. Every field but Source may be nil: the page then draws
// from the text of the file, and says less.
type PolicyInput struct {
	Source  *ascv1.GetPolicySourceResponse
	Land    *ascv1.PolicyLandRequirements
	Secrets *ascv1.ListSecretRulesResponse
	// You is the answer of Policy.Evaluate (WRITE) for the signed-in person over YouPaths.
	You      *ascv1.EvaluatePolicyResponse
	YouPaths []string
}

// Protection is one switch of the "Protections" block. Changing it proposes a change to the policy file.
type Protection struct {
	Key   string // the form value: signed, secrets or quarantine
	Title string
	Help  string
	On    bool
}

// Cell is one cell of the who-can-write table. Tone is ok, no, via, never or ro and picks the colour; the text
// is always there too, so colour is never the only signal.
type Cell struct {
	Text  string
	Tone  string
	Title string
}

// WhoRow is one row of the who-can-write table.
type WhoRow struct {
	Who    string
	Detail string
	Cells  []Cell
}

// WhoTable is the who-can-write table.
type WhoTable struct {
	Columns []string
	Rows    []WhoRow
}

// PolicyView is the policy page.
type PolicyView struct {
	Present     bool
	Usable      bool   // present and parsed without error
	Error       string // a fixed text, never the parser's own
	Text        string
	Source      string // the first 12 hex digits of the changeset the file was read from
	Maintainer  bool   // the signed-in person is a maintainer
	Meaning     []string
	Table       WhoTable
	Protections []Protection
	MergeNote   string
}

const (
	maxPathColumns = 5
	maxRoleRows    = 8
	maxDetailNames = 4
	otherPathProbe = "zz-other/zz-file"
)

// BuildPolicyView builds the page from the answers.
func BuildPolicyView(in PolicyInput) *PolicyView {
	v := &PolicyView{}
	src := in.Source
	if src == nil {
		return v
	}
	v.Present = src.GetPresent()
	v.Text = CleanBlock(string(src.GetText()), MaxPolicyBytes)
	if len(src.GetSource()) >= 6 {
		v.Source = hexPrefix(src.GetSource(), 12)
	}
	parsed := src.GetParsed()
	v.Maintainer = parsed.GetMaintainer()
	v.Usable = v.Present && parsed != nil && parsed.GetError() == ""
	if v.Present && parsed != nil && parsed.GetError() != "" {
		v.Error = "The policy file on trunk cannot be used, so the server refuses every update of trunk except one that repairs the file."
	}
	if v.Usable || !v.Present {
		// The sentences are about a policy that is in force. A file that cannot be used has none.
		v.Meaning = Meaning(parsed, v.Text, in.Land, in.Secrets, v.Present)
		v.Protections = protections(v.Text, in.Land, in.Secrets)
	}
	if v.Usable {
		v.Table = whoTable(parsed, in)
		v.MergeNote = mergeSentence(strategyOf(parsed, in.Land))
	}
	return v
}

func hexPrefix(b []byte, n int) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	if len(out) > n {
		out = out[:n]
	}
	return string(out)
}

func strategyOf(parsed *ascv1.GetPolicyResponse, land *ascv1.PolicyLandRequirements) string {
	if land != nil && land.GetMergeStrategy() != "" {
		return land.GetMergeStrategy()
	}
	return parsed.GetMergeStrategy()
}

func mergeSentence(strategy string) string {
	switch strategy {
	case "rebase":
		return "Changes land by being rebased onto trunk."
	case "rebase-disjoint":
		return "Changes land by being rebased onto trunk, and only when they touch different files from the changes that landed meanwhile."
	case "fast-forward":
		return "Changes land only when they already sit on top of trunk."
	}
	return ""
}

func protections(text string, land *ascv1.PolicyLandRequirements, secrets *ascv1.ListSecretRulesResponse) []Protection {
	signed, _ := TomlBool(text, "signing", "require_signed")
	if land != nil {
		signed = land.GetSigned()
	}
	block := true
	if v, ok := TomlBool(text, "secrets", "block_on_push"); ok {
		block = v
	}
	if secrets != nil {
		block = secrets.GetBlockOnPush()
	}
	quarantine, _ := TomlBool(text, "ci", "allow_quarantine")
	return []Protection{
		{Key: "signed", Title: "Require signed changes", Help: "Landing on trunk needs a valid signature on every change. Unsigned changes are refused.", On: signed},
		{Key: "secrets", Title: "Block pushes that contain secrets", Help: "Private keys, cloud and token patterns. The push is refused with the file and the rule, never the secret.", On: block},
		{Key: "quarantine", Title: "Allow quarantining flaky tests", Help: "A maintainer can set a failing test aside so that it does not block landing. The quarantine is recorded and can be undone.", On: quarantine},
	}
}

// Toggles maps the form value of a protection to its place in the policy file.
var Toggles = map[string][2]string{
	"signed":     {"signing", "require_signed"},
	"secrets":    {"secrets", "block_on_push"},
	"quarantine": {"ci", "allow_quarantine"},
}

// ApplyToggle returns text with the protection key set to on. It fails for a key that is not a protection.
func ApplyToggle(text, key string, on bool) (string, error) {
	loc, ok := Toggles[key]
	if !ok {
		return "", ErrBadInput
	}
	return TomlSetBool(text, loc[0], loc[1], on), nil
}

// Meaning explains a policy in plain sentences, from the parsed rules and, for the keys that Repo.GetPolicy does
// not carry (signing, secrets, CI, budgets), from the text of the file. land and secrets, when asc answered them,
// win over the text. present false is a repository without a policy file.
func Meaning(parsed *ascv1.GetPolicyResponse, text string, land *ascv1.PolicyLandRequirements, secrets *ascv1.ListSecretRulesResponse, present bool) []string {
	if !present {
		return []string{"This repository has no policy file. Until one is added, the defaults of the server apply: no approvals or checks are required on trunk, and only the rules of the host decide who can write."}
	}
	if parsed == nil {
		return nil
	}
	var out []string

	// Trunk.
	trunk := parsed.GetTrunk()
	approvals := int(trunk.GetRequireApprovals())
	checks := trunk.GetRequireChecks()
	if land != nil {
		approvals = int(land.GetApprovals())
		checks = land.GetChecks()
	}
	branch := trunk.GetBranch()
	if branch == "" {
		branch = "trunk"
	}
	branch = CleanText(branch, 64)
	switch {
	case approvals > 0 && len(checks) > 0:
		out = append(out, "Trunk ("+branch+") needs "+plural(approvals, "approval", "approvals")+" and green "+joinWords(cleanAll(checks))+" "+noun(len(checks), "check", "checks")+".")
	case approvals > 0:
		out = append(out, "Trunk ("+branch+") needs "+plural(approvals, "approval", "approvals")+".")
	case len(checks) > 0:
		out = append(out, "Trunk ("+branch+") needs green "+joinWords(cleanAll(checks))+" "+noun(len(checks), "check", "checks")+".")
	default:
		out = append(out, "Trunk ("+branch+") needs no approvals and no checks.")
	}
	if trunk.GetAllowForce() {
		out = append(out, "Trunk can be force-pushed by anyone who may write to it.")
	} else {
		out = append(out, "Nobody can force-push trunk.")
	}

	// Signing.
	signed, _ := TomlBool(text, "signing", "require_signed")
	if land != nil {
		signed = land.GetSigned()
	}
	if signed {
		out = append(out, "Every change on trunk must be signed.")
	}

	// Path rules.
	for _, rule := range parsed.GetPaths() {
		out = append(out, pathRuleSentence(rule))
	}

	// Agents.
	if s := agentSentence(parsed.GetAgents(), text); s != "" {
		out = append(out, s)
	}
	if s := agentBudgetSentence(text); s != "" {
		out = append(out, s)
	}

	// Merge strategy.
	if s := mergeSentence(strategyOf(parsed, land)); s != "" {
		out = append(out, s)
	}

	// Secrets.
	block := true
	if v, ok := TomlBool(text, "secrets", "block_on_push"); ok {
		block = v
	}
	allow, _ := TomlStrings(text, "secrets", "allow_paths")
	if secrets != nil {
		block = secrets.GetBlockOnPush()
		allow = secrets.GetAllowPaths()
	}
	if enabled, ok := TomlBool(text, "secrets", "enabled"); ok && !enabled {
		out = append(out, "Secret scanning is switched off.")
	} else if block {
		out = append(out, "A push that contains a secret is refused, and the answer names the file and the rule, never the secret.")
	} else {
		out = append(out, "A push that contains a secret is let through, and the finding is recorded for review.")
	}
	if len(allow) > 0 {
		out = append(out, "Secret scanning skips "+joinWords(displayGlobs(allow))+".")
	}

	// CI.
	if q, _ := TomlBool(text, "ci", "allow_quarantine"); q {
		out = append(out, "A maintainer can quarantine a flaky test, so that it does not block landing.")
	}
	return out
}

func cleanAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, CleanText(s, 64))
	}
	return out
}

// noun is the word for n of something, without the number.
func noun(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}

// joinWords joins items as "a", "a and b", "a, b and c".
func joinWords(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// displayGlob shows a path pattern the way a person says it: `infra/**` is `infra/`, `**` is everything.
func displayGlob(g string) string {
	g = CleanText(g, MaxGlobLen)
	if g == "**" {
		return "everything"
	}
	if p, ok := strings.CutSuffix(g, "/**"); ok && !strings.ContainsAny(p, "*?") {
		return p + "/"
	}
	return g
}

func displayGlobs(globs []string) []string {
	out := make([]string, 0, len(globs))
	for _, g := range globs {
		out = append(out, displayGlob(g))
	}
	return out
}

// describeSubject says who a subject names.
func describeSubject(s string) string {
	s = CleanText(s, MaxSubjectLen)
	if s == "*" {
		return "anyone"
	}
	head, rest, ok := strings.Cut(s, ":")
	if !ok {
		return s
	}
	switch head {
	case "role":
		if rest == "maintainer" {
			return "maintainers"
		}
		return "the " + rest + " role"
	case "user":
		if rest == "*" {
			return "people"
		}
		return rest
	case "agent":
		if rest == "*" {
			return "agents"
		}
		return "agent " + rest
	case "deploy":
		if rest == "*" {
			return "deploy keys"
		}
		return "deploy key " + rest
	case "ci":
		if rest == "*" {
			return "CI"
		}
		return "CI " + rest
	case "host":
		return "the host"
	}
	return s
}

// MaxSubjectLen is the longest subject (asc-core MAX_SUBJECT_LEN).
const MaxSubjectLen = 128

func pathRuleSentence(rule *ascv1.PolicyPathRule) string {
	where := displayGlob(rule.GetGlob())
	who := make([]string, 0, len(rule.GetWrite()))
	for _, s := range rule.GetWrite() {
		who = append(who, describeSubject(s))
	}
	if len(who) == 0 {
		return "Nobody can touch " + where + "."
	}
	if len(who) == 1 && who[0] == "anyone" {
		return "Anyone who can write to the repository can touch " + where + "."
	}
	return "Only " + joinWords(who) + " can touch " + where + "."
}

func agentSentence(a *ascv1.PolicyAgents, text string) string {
	if a == nil {
		return ""
	}
	var parts []string
	if len(a.GetDefaultScope()) > 0 {
		s := "Agents can write " + joinWords(displayGlobs(a.GetDefaultScope()))
		if n := a.GetMaxChangesPerRun(); n > 0 {
			s += ", up to " + plural(int(n), "change", "changes") + " a run"
		}
		parts = append(parts, s+".")
	} else if n := a.GetMaxChangesPerRun(); n > 0 {
		parts = append(parts, "An agent run can make up to "+plural(int(n), "change", "changes")+".")
	}
	if n := a.GetMaxPathsPerRun(); n > 0 {
		parts = append(parts, "A run can touch up to "+plural(int(n), "path", "paths")+".")
	}
	if len(a.GetDeniedPaths()) > 0 {
		parts = append(parts, "Agents never write "+joinWords(displayGlobs(a.GetDeniedPaths()))+".")
	}
	if enabled, ok := TomlBool(text, "agents", "enabled"); ok && !enabled {
		parts = append(parts, "Agents are switched off for this repository.")
	}
	return strings.Join(parts, " ")
}

func agentBudgetSentence(text string) string {
	tokens, hasTokens := TomlInt(text, "agents.budgets", "max_tokens")
	cost, hasCost := TomlInt(text, "agents.budgets", "max_cost_micro_usd")
	switch {
	case hasTokens && hasCost:
		return "An agent run may use up to " + itoa(int(tokens)) + " tokens and " + dollars(cost) + "."
	case hasTokens:
		return "An agent run may use up to " + itoa(int(tokens)) + " tokens."
	case hasCost:
		return "An agent run may cost up to " + dollars(cost) + "."
	}
	return ""
}

// dollars writes a count of millionths of a US dollar.
func dollars(micro uint64) string {
	whole := micro / 1_000_000
	cents := (micro % 1_000_000) / 10_000
	return "$" + strconv.FormatUint(whole, 10) + "." + pad2(cents)
}

func pad2(n uint64) string {
	if n < 10 {
		return "0" + strconv.FormatUint(n, 10)
	}
	return strconv.FormatUint(n, 10)
}

// sampleOf turns a path pattern into a path it matches, for the table.
func sampleOf(glob string) string {
	parts := strings.Split(glob, "/")
	for i, p := range parts {
		if p == "**" || strings.ContainsAny(p, "*?") {
			parts[i] = strings.NewReplacer("**", "x", "*", "x", "?", "x").Replace(p)
		}
	}
	return strings.Join(parts, "/")
}

// whoTable computes who can write where, from the rules in force.
func whoTable(parsed *ascv1.GetPolicyResponse, in PolicyInput) WhoTable {
	rules := RulesOf(parsed)
	var t WhoTable
	t.Columns = append(t.Columns, "Trunk")
	var probes []string
	seen := map[string]bool{}
	for _, rule := range rules.Paths {
		if len(probes) >= maxPathColumns || seen[rule.Glob] {
			continue
		}
		seen[rule.Glob] = true
		t.Columns = append(t.Columns, displayGlob(rule.Glob))
		probes = append(probes, sampleOf(rule.Glob))
	}
	t.Columns = append(t.Columns, "Other paths")
	probes = append(probes, otherPathProbe)

	rowFor := func(label, detail string, who Who) WhoRow {
		row := WhoRow{Who: label, Detail: detail}
		allowed := make([]bool, len(probes))
		reasons := make([]string, len(probes))
		anyAllowed := false
		for i, p := range probes {
			allowed[i], reasons[i] = rules.CheckWrite(who, p)
			anyAllowed = anyAllowed || allowed[i]
		}
		switch {
		case who.isAgent():
			row.Cells = append(row.Cells, Cell{Text: "Never directly", Tone: "never", Title: "Agents never push trunk. Their work is a change under review."})
		case anyAllowed:
			row.Cells = append(row.Cells, Cell{Text: "Via a reviewed change", Tone: "via"})
		default:
			row.Cells = append(row.Cells, Cell{Text: "No", Tone: "no"})
		}
		for i := range probes {
			switch {
			case allowed[i] && who.isAgent() && (rules.MaxChanges > 0 || rules.MaxPaths > 0):
				row.Cells = append(row.Cells, Cell{Text: "Yes, budgeted", Tone: "ok"})
			case allowed[i]:
				row.Cells = append(row.Cells, Cell{Text: "Yes", Tone: "ok"})
			default:
				row.Cells = append(row.Cells, Cell{Text: "No", Tone: "no", Title: CleanText(reasons[i], MaxTextBytes)})
			}
		}
		return row
	}

	// The roles of the policy, maintainers first.
	names := make([]string, 0, len(rules.Roles))
	for name := range rules.Roles {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "maintainer") != (names[j] == "maintainer") {
			return names[i] == "maintainer"
		}
		return names[i] < names[j]
	})
	if len(names) > maxRoleRows {
		names = names[:maxRoleRows]
	}
	for _, name := range names {
		members := rules.Roles[name]
		label := CleanText(name, 64)
		detail := memberDetail(members)
		if rep, ok := representative(members); ok {
			t.Rows = append(t.Rows, rowFor(label, detail, rep))
			continue
		}
		row := WhoRow{Who: label, Detail: detail}
		for range t.Columns {
			row.Cells = append(row.Cells, Cell{Text: "No", Tone: "no", Title: "The role has no members that can be named."})
		}
		t.Rows = append(t.Rows, row)
	}

	if len(rules.AgentScope) > 0 || len(rules.AgentDenied) > 0 || rules.MaxChanges > 0 || rules.MaxPaths > 0 {
		t.Rows = append(t.Rows, rowFor("Agents with a run token", "", Who{Kind: "agent", Name: "agent"}))
	}
	t.Rows = append(t.Rows, rowFor("Other people with write access", "", Who{Kind: "user", Name: "someone"}))

	if in.You != nil && len(in.You.GetDecisions()) == len(in.YouPaths) && len(in.YouPaths) == len(probes) {
		row := WhoRow{Who: "You"}
		anyOK := false
		cells := make([]Cell, len(probes))
		for i, d := range in.You.GetDecisions() {
			if d.GetAllowed() {
				anyOK = true
				cells[i] = Cell{Text: "Yes", Tone: "ok"}
			} else {
				cells[i] = Cell{Text: "No", Tone: "no", Title: CleanText(d.GetReason(), MaxTextBytes)}
			}
		}
		if anyOK {
			row.Cells = append(row.Cells, Cell{Text: "Via a reviewed change", Tone: "via"})
		} else {
			row.Cells = append(row.Cells, Cell{Text: "No", Tone: "no"})
		}
		row.Cells = append(row.Cells, cells...)
		t.Rows = append(t.Rows, row)
	}

	readOnly := WhoRow{Who: "Everyone else", Detail: "read only"}
	readOnly.Cells = append(readOnly.Cells, Cell{Text: "No", Tone: "no"})
	for range probes {
		readOnly.Cells = append(readOnly.Cells, Cell{Text: "Read only", Tone: "ro"})
	}
	t.Rows = append(t.Rows, readOnly)
	return t
}

// ProbePaths returns the paths the who-can-write table probes, in column order, so that the handler can ask
// Policy.Evaluate about the same ones for the signed-in person.
func ProbePaths(parsed *ascv1.GetPolicyResponse) []string {
	rules := RulesOf(parsed)
	var probes []string
	seen := map[string]bool{}
	for _, rule := range rules.Paths {
		if len(probes) >= maxPathColumns || seen[rule.Glob] {
			continue
		}
		seen[rule.Glob] = true
		probes = append(probes, sampleOf(rule.Glob))
	}
	return append(probes, otherPathProbe)
}

// representative picks a principal that stands for the members of a role: the first member that names a kind
// and a name. A glob in the name is replaced by a concrete name.
func representative(members []string) (Who, bool) {
	for _, m := range members {
		head, rest, ok := strings.Cut(m, ":")
		if !ok || head == "role" {
			continue
		}
		name := strings.ReplaceAll(rest, "*", "x")
		if name == "" {
			continue
		}
		return Who{Kind: head, Name: name}, true
	}
	return Who{}, false
}

func memberDetail(members []string) string {
	var names []string
	for _, m := range members {
		if head, rest, ok := strings.Cut(m, ":"); ok && head == "user" && !strings.Contains(rest, "*") {
			names = append(names, CleanText(rest, 64))
		}
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) > maxDetailNames {
		rest := len(names) - maxDetailNames
		return strings.Join(names[:maxDetailNames], ", ") + " and " + itoa(rest) + " more"
	}
	return strings.Join(names, ", ")
}

// Diagnostics is the result of Policy.Validate, reduced to what the page draws.
type Diagnostic struct {
	Error   bool
	Code    string
	Message string
	Line    uint32
	Column  uint32
}

// DiagnosticsOf copies and cleans the diagnostics of a validation, errors first.
func DiagnosticsOf(resp *ascv1.ValidatePolicyResponse) []Diagnostic {
	var out []Diagnostic
	for i, d := range resp.GetDiagnostics() {
		if i >= MaxListItems {
			break
		}
		out = append(out, Diagnostic{
			Error:   d.GetSeverity() == ascv1.PolicySeverity_POLICY_SEVERITY_ERROR,
			Code:    CleanText(d.GetCode(), 64),
			Message: CleanText(d.GetMessage(), MaxTextBytes),
			Line:    d.GetLine(),
			Column:  d.GetColumn(),
		})
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int {
		switch {
		case a.Error && !b.Error:
			return -1
		case !a.Error && b.Error:
			return 1
		}
		return 0
	})
	return out
}

// StarterPolicy is the text the editor starts from in a repository without a policy file: the signed-in person
// is the one maintainer, and the usual rules are there as comments to switch on. It is valid as it stands.
func StarterPolicy(maintainer string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == '@', r == '+':
			return r
		}
		return -1
	}, maintainer)
	if name == "" {
		name = "your-name"
	}
	return "version = 1\n\n[roles]\nmaintainer = [" + tomlQuote("user:"+name) + "]\n\n" +
		"# [trunk]\n# require_approvals = 1\n# require_checks = [\"build\"]\n# allow_force = false\n\n" +
		"# [signing]\n# require_signed = true\n\n" +
		"# [paths.\"infra/**\"]\n# write = [\"role:maintainer\"]\n\n" +
		"# [agents]\n# default_scope = [\"src/**\", \"tests/**\"]\n"
}
