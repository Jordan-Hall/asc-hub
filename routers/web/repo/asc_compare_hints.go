// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strconv"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/services/context"
)

// Hints of the compare page (board Compare): the ref nearest to a name that asc does not know, and the number of lines
// each change and each file adds and removes. Both are read from the repository the hub keeps beside asc; a ref or a
// diff the hub cannot read leaves the hint out, never a wrong one.

// editDistance is the Levenshtein distance of two strings, compared by byte.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// suggestRef returns the candidate nearest to given, or "" when none is near enough: within a third of the length of
// the name (at least two edits), or the name with a prefix or suffix cut. Case does not matter. An exact match is not
// a suggestion.
func suggestRef(given string, candidates []string) string {
	g := strings.ToLower(strings.TrimSpace(given))
	if g == "" {
		return ""
	}
	best, bestD := "", 1<<30
	for _, c := range candidates {
		lc := strings.ToLower(c)
		if lc == g && c == given {
			return ""
		}
		d := editDistance(g, lc)
		if len(g) >= 4 && len(lc) >= 4 && (strings.HasPrefix(lc, g) || strings.HasPrefix(g, lc) || strings.HasSuffix(lc, g)) {
			d = min(d, 1)
		}
		if d < bestD || (d == bestD && c < best) {
			best, bestD = c, d
		}
	}
	if best == "" || best == given || bestD > max(2, len(g)/3) {
		return ""
	}
	return best
}

// ascDidYouMean is a suggestion for one side of the compare form.
type ascDidYouMean struct {
	Side  string // "a" or "b"
	Given string
	Hint  string
}

// compareHints suggests, for each side that names no known ref, the nearest known ref.
func compareHints(a, b string, groups []ascRefGroup) []ascDidYouMean {
	var names []string
	known := map[string]bool{}
	for _, g := range groups {
		if g.Label == "asc_rp.cmp_current" {
			continue
		}
		for _, n := range g.Names {
			names = append(names, n)
			known[n] = true
		}
	}
	var out []ascDidYouMean
	for _, side := range []struct{ key, val string }{{"a", a}, {"b", b}} {
		if side.val == "" || known[side.val] {
			continue
		}
		if h := suggestRef(side.val, names); h != "" {
			out = append(out, ascDidYouMean{Side: side.key, Given: side.val, Hint: h})
		}
	}
	return out
}

// numstat is the lines one file or one change adds and removes; Binary is true for a file git cannot count.
type numstat struct {
	Add, Del int
	Binary   bool
}

// parseNumstat reads `git diff --numstat` output ("added\tremoved\tpath"; "-" counts for a binary file).
func parseNumstat(out string) map[string]numstat {
	res := map[string]numstat{}
	for _, line := range strings.Split(out, "\n") {
		p := strings.SplitN(line, "\t", 3)
		if len(p) != 3 || p[2] == "" {
			continue
		}
		if p[0] == "-" || p[1] == "-" {
			res[p[2]] = numstat{Binary: true}
			continue
		}
		add, e1 := strconv.Atoi(p[0])
		del, e2 := strconv.Atoi(p[1])
		if e1 != nil || e2 != nil {
			continue
		}
		res[p[2]] = numstat{Add: add, Del: del}
	}
	return res
}

// compareFileStats counts the lines of each file between a and b, in the dot form the page asked for. It returns nil when
// the hub cannot read either revision.
func compareFileStats(ctx *context.Context, a, b string, three bool) map[string]numstat {
	if ctx.Repo.GitRepo == nil {
		return nil
	}
	dots := ".."
	if three {
		dots = "..."
	}
	out, _, err := git.NewCommand(ctx, "diff", "--numstat", "--no-renames").AddDynamicArguments(a + dots + b).
		RunStdString(&git.RunOpts{Dir: ctx.Repo.GitRepo.Path})
	if err != nil {
		return nil
	}
	return parseNumstat(out)
}

// commitStat is the lines one commit adds and removes.
func commitStat(ctx *context.Context, sha string) (numstat, bool) {
	if ctx.Repo.GitRepo == nil {
		return numstat{}, false
	}
	if _, add, del, err := ctx.Repo.GitRepo.GetCommitShortStat(sha); err == nil {
		return numstat{Add: add, Del: del}, true
	}
	return numstat{}, false
}
