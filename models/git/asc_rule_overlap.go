// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package git

import (
	"context"
	"strings"
)

// ascRuleSampleCap bounds the branch names a pattern with {a,b} alternatives expands to.
const ascRuleSampleCap = 32

// ascRuleSamples returns branch names the pattern matches: each wildcard becomes one letter, a class its first
// member and each {a,b} alternative its own name. Two patterns that match a sample of each other overlap.
func ascRuleSamples(pattern string) []string {
	out := []string{""}
	add := func(parts ...string) {
		next := make([]string, 0, len(out)*len(parts))
		for _, o := range out {
			for _, p := range parts {
				if len(next) < ascRuleSampleCap {
					next = append(next, o+p)
				}
			}
		}
		out = next
	}
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
				add(pattern[i : i+1])
			}
		case '*', '?':
			for i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
			}
			add("x")
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				add("[")
				continue
			}
			class := pattern[i+1 : i+1+end]
			i += end + 1
			if strings.HasPrefix(class, "!") || class == "" {
				add("~")
			} else {
				add(class[:1])
			}
		case '{':
			end, alts := ascRuleAlternatives(pattern[i:])
			if end < 0 {
				add("{")
				continue
			}
			i += end
			var parts []string
			for _, alt := range alts {
				parts = append(parts, ascRuleSamples(alt)...)
			}
			add(parts...)
		default:
			add(pattern[i : i+1])
		}
	}
	return out
}

// ascRuleAlternatives splits "{a,b{c,d}}rest" into its top-level alternatives and returns the index of the closing
// brace, or -1 when there is none.
func ascRuleAlternatives(s string) (int, []string) {
	depth, start := 0, 1
	var alts []string
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, append(alts, s[start:i])
			}
		case ',':
			if depth == 1 {
				alts = append(alts, s[start:i])
				start = i + 1
			}
		}
	}
	return -1, nil
}

// ascRulesOverlap reports whether one branch name could fall under both patterns: either matches the other's name or
// one of its samples, or a branch of the repository matches both.
func ascRulesOverlap(a, b *ProtectedBranch, branches []string) bool {
	for _, pair := range [][2]*ProtectedBranch{{a, b}, {b, a}} {
		if pair[0].Match(pair[1].RuleName) {
			return true
		}
		for _, s := range ascRuleSamples(pair[1].RuleName) {
			if pair[0].Match(s) {
				return true
			}
		}
	}
	for _, br := range branches {
		if a.Match(br) && b.Match(br) {
			return true
		}
	}
	return false
}

// AscOverlappingRule returns the first rule of the repository, other than ruleID, that would protect a branch the
// pattern also protects, or nil. A branch must answer to one rule only (board RepoSettingsRules).
func AscOverlappingRule(ctx context.Context, repoID, ruleID int64, pattern string) (*ProtectedBranch, error) {
	rules, err := FindRepoProtectedBranchRules(ctx, repoID)
	if err != nil {
		return nil, err
	}
	candidate := &ProtectedBranch{RuleName: pattern}
	var branches []string
	for _, r := range rules {
		if r.ID == ruleID {
			continue
		}
		if branches == nil {
			if branches, err = FindAllMatchedBranches(ctx, repoID, "**"); err != nil {
				return nil, err
			}
		}
		if ascRulesOverlap(candidate, r, branches) {
			return r, nil
		}
	}
	return nil, nil
}

// AscRuleOpenCount adds up the counts of the branches the pattern protects.
func AscRuleOpenCount(pattern string, byBranch map[string]int64) int64 {
	rule := &ProtectedBranch{RuleName: pattern}
	var n int64
	for br, c := range byBranch {
		if rule.Match(br) {
			n += c
		}
	}
	return n
}
