// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// A Go port of the pure decisions of asc-core's policy.rs (Policy::check_write and check_run), run over the
// rules that Repo.GetPolicy returns. It exists to draw the "who can write here" table for principals that the
// page may not ask asc about (Policy.Evaluate for another principal needs a maintainer). It decides nothing:
// asc enforces the policy on every update. A test keeps the two in step by running the same cases as policy.rs.

const policyPath = ".asc/policy.toml"

// Who is a principal as the policy sees it. An empty Kind is a principal of no known kind, which matches `*` only.
type Who struct {
	Kind string
	Name string
}

func (w Who) isAgent() bool { return w.Kind == "agent" }

// PathRule is one `[paths."glob"]` table.
type PathRule struct {
	Glob  string
	Write []string
}

// Rules are the rules of a policy that the decisions need.
type Rules struct {
	Roles       map[string][]string
	Paths       []PathRule
	AgentScope  []string
	AgentDenied []string
	MaxChanges  uint32
	MaxPaths    uint32
}

// RulesOf copies the rules out of an answer of Repo.GetPolicy.
func RulesOf(p *ascv1.GetPolicyResponse) *Rules {
	r := &Rules{Roles: map[string][]string{}}
	if p == nil {
		return r
	}
	for _, role := range p.GetRoles() {
		r.Roles[role.GetName()] = append([]string(nil), role.GetMembers()...)
	}
	for _, rule := range p.GetPaths() {
		r.Paths = append(r.Paths, PathRule{Glob: rule.GetGlob(), Write: append([]string(nil), rule.GetWrite()...)})
	}
	if a := p.GetAgents(); a != nil {
		r.AgentScope = append([]string(nil), a.GetDefaultScope()...)
		r.AgentDenied = append([]string(nil), a.GetDeniedPaths()...)
		r.MaxChanges = a.GetMaxChangesPerRun()
		r.MaxPaths = a.GetMaxPathsPerRun()
	}
	return r
}

// nameGlob matches text against a pattern in which `*` is any run of characters (asc-core name_glob).
func nameGlob(pattern, text string) bool {
	p, t := pattern, text
	pi, ti, star, mark := 0, 0, -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case pi < len(p) && p[pi] == t[ti]:
			pi++
			ti++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// wild matches one path segment: `*` is any run, `?` one byte (asc-core wild).
func wild(p, t string) bool {
	pi, ti, star, mark := 0, 0, -1, 0
	for ti < len(t) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// asciiLower lower-cases ASCII letters only, as asc does.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// GlobMatch reports whether path matches the path pattern glob: segments split at `/`, a `**` segment matches any
// number of segments (none included), `*` and `?` match inside one segment, ASCII case ignored. The match is by
// dynamic programming, so a pattern with many `**` segments costs at most len(pattern) times len(path).
func GlobMatch(glob, path string) bool {
	pat := strings.Split(asciiLower(glob), "/")
	parts := strings.Split(asciiLower(path), "/")
	// memo[i][j]: pat[i:] matches parts[j:]; 0 unknown, 1 yes, 2 no.
	memo := make([][]uint8, len(pat)+1)
	for i := range memo {
		memo[i] = make([]uint8, len(parts)+1)
	}
	var match func(i, j int) bool
	match = func(i, j int) bool {
		if m := memo[i][j]; m != 0 {
			return m == 1
		}
		var ok bool
		switch {
		case i == len(pat):
			ok = j == len(parts)
		case pat[i] == "**":
			for n := j; n <= len(parts) && !ok; n++ {
				ok = match(i+1, n)
			}
		case j < len(parts):
			ok = wild(pat[i], parts[j]) && match(i+1, j+1)
		}
		if ok {
			memo[i][j] = 1
		} else {
			memo[i][j] = 2
		}
		return ok
	}
	return match(0, 0)
}

func globAny(globs []string, path string) bool {
	for _, g := range globs {
		if GlobMatch(g, path) {
			return true
		}
	}
	return false
}

// subjectMatches reports whether subject s names who. A subject is `*`, `<kind>:<glob>` or `role:<name>`.
func (r *Rules) subjectMatches(who Who, s string) bool {
	if s == "*" {
		return true
	}
	head, rest, ok := strings.Cut(s, ":")
	if !ok {
		return false
	}
	if head == "role" {
		return r.IsMember(who, rest)
	}
	return who.Kind != "" && head == who.Kind && nameGlob(rest, who.Name)
}

// IsMember reports whether who is one of the subjects of role.
func (r *Rules) IsMember(who Who, role string) bool {
	for _, s := range r.Roles[role] {
		if r.subjectMatches(who, s) {
			return true
		}
	}
	return false
}

// IsMaintainer reports whether who holds the role `maintainer`.
func (r *Rules) IsMaintainer(who Who) bool { return r.IsMember(who, "maintainer") }

func (r *Rules) admits(who Who, list []string) bool {
	for _, s := range list {
		if r.subjectMatches(who, s) {
			return true
		}
	}
	return false
}

func touchesPolicy(path string) bool {
	lower := asciiLower(path)
	return lower == policyPath || lower == ".asc" || strings.HasPrefix(lower, policyPath+"/")
}

// CheckWrite reports whether who may write path to a protected ref, and when not, why. The policy file needs a
// maintainer; an agent keeps to its scope and out of its denied paths; every matching `[paths]` rule must admit
// the writer; a path no rule names is open to anyone who can write the ref.
func (r *Rules) CheckWrite(who Who, path string) (bool, string) {
	if touchesPolicy(path) && !r.IsMaintainer(who) {
		return false, "the repository policy can be changed only by a maintainer"
	}
	var matching []PathRule
	for _, rule := range r.Paths {
		if GlobMatch(rule.Glob, path) {
			matching = append(matching, rule)
		}
	}
	if who.isAgent() {
		if globAny(r.AgentDenied, path) {
			return false, "agents may not write this path"
		}
		if len(r.AgentScope) > 0 {
			inScope := globAny(r.AgentScope, path)
			granted := false
			for _, rule := range matching {
				for _, s := range rule.Write {
					if strings.HasPrefix(s, "agent:") && r.subjectMatches(who, s) {
						granted = true
					}
				}
			}
			if !inScope && !granted {
				return false, "this path is outside the agent scope"
			}
		}
	}
	for _, rule := range matching {
		if !r.admits(who, rule.Write) {
			if len(rule.Write) == 0 {
				return false, "writing " + rule.Glob + " is limited to nobody"
			}
			return false, "writing " + rule.Glob + " is limited to " + strings.Join(rule.Write, ", ")
		}
	}
	return true, ""
}

// CheckRun reports whether an agent run of changes changesets touching paths paths is within budget. Anyone else
// has no budget.
func (r *Rules) CheckRun(who Who, changes, paths int) (bool, string) {
	if !who.isAgent() {
		return true, ""
	}
	if r.MaxChanges > 0 && changes > int(r.MaxChanges) {
		return false, "an agent run may land at most " + itoa(int(r.MaxChanges)) + " changes"
	}
	if r.MaxPaths > 0 && paths > int(r.MaxPaths) {
		return false, "an agent run may touch at most " + itoa(int(r.MaxPaths)) + " paths"
	}
	return true, ""
}
