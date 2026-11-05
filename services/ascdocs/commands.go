// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdocs

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Command is one row of the command reference (CLI.md): `asc <name>`, what it does, and the group it is listed under.
type Command struct {
	Name    string // "asc restack"
	Summary string // the first sentence of "What it does", without markdown marks
	Group   string // "Stacks of changes"
	Anchor  string // the heading of the group in the reference page: "6-stacks-of-changes"
}

var (
	cmdOnce  sync.Once
	commands []Command
	vocab    map[string]bool

	cmdGroup = regexp.MustCompile(`^## (\d+)\. (.+)$`)
	cmdRow   = regexp.MustCompile(`^\| ` + "`([^`]+)`" + ` \| ([^|]+) \|`)
	sentence = regexp.MustCompile(`^(.+?\.)(\s|$)`)
	wordRe   = regexp.MustCompile(`[a-z][a-z0-9-]{2,}`)
)

// anchorOf is the id the markdown renderer gives a heading: lower case, spaces as dashes, other marks dropped.
func anchorOf(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

func loadCommands() {
	once.Do(load)
	raw, err := content.ReadFile("content/CLI.md")
	if err != nil {
		return
	}
	group, anchor := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if m := cmdGroup.FindStringSubmatch(line); m != nil {
			group, anchor = m[2], anchorOf(m[1]+". "+m[2])
			continue
		}
		m := cmdRow.FindStringSubmatch(line)
		if m == nil || group == "" {
			continue
		}
		what := strings.TrimSpace(mdNoise.ReplaceAllString(m[2], ""))
		if s := sentence.FindStringSubmatch(what); s != nil {
			what = s[1]
		}
		name := strings.TrimSpace(m[1])
		if !strings.HasPrefix(name, "asc ") {
			name = "asc " + name
		}
		commands = append(commands, Command{Name: name, Summary: what, Group: group, Anchor: anchor})
	}
	vocab = map[string]bool{}
	add := func(s string) {
		for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
			vocab[w] = true
		}
	}
	for _, c := range commands {
		add(c.Name + " " + c.Summary)
	}
	for _, d := range docs {
		add(d.page.Title + " " + d.page.Summary + " " + d.body)
	}
}

// SearchCommands finds the commands whose name or summary has every word of the query, a name match first.
func SearchCommands(q string, limit int) []Command {
	cmdOnce.Do(loadCommands)
	words := strings.Fields(strings.ToLower(strings.TrimSpace(q)))
	if len(words) == 0 || len(words) > 8 {
		return nil
	}
	type scored struct {
		c     Command
		score int
	}
	var hits []scored
	for _, c := range commands {
		name, text := strings.ToLower(c.Name), strings.ToLower(c.Summary)
		score, ok := 0, true
		for _, w := range words {
			switch {
			case strings.Contains(name, w):
				score += 10
			case strings.Contains(text, w):
				score++
			default:
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok {
			hits = append(hits, scored{c, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]Command, 0, min(len(hits), limit))
	for i := 0; i < len(hits) && i < limit; i++ {
		out = append(out, hits[i].c)
	}
	return out
}

// Suggest is the query with each word that the documentation does not use replaced by the closest word it does use
// (at most two edits away, words of four letters or more), or "" when no word changes.
func Suggest(q string) string {
	cmdOnce.Do(loadCommands)
	words := strings.Fields(strings.ToLower(strings.TrimSpace(q)))
	if len(words) == 0 || len(words) > 8 {
		return ""
	}
	changed := false
	for i, w := range words {
		if vocab[w] || len([]rune(w)) < 4 {
			continue
		}
		best, bestDist := "", 3
		for v := range vocab {
			// one past the best so far, so a tie is a real distance and not the cap
			if d := editDistance(w, v, min(bestDist+1, 3)); d < bestDist || (d == bestDist && d < 3 && v < best) {
				best, bestDist = v, d
			}
		}
		if best != "" {
			words[i], changed = best, true
		}
	}
	if !changed {
		return ""
	}
	return strings.Join(words, " ")
}

// editDistance is the Levenshtein distance of a and b, or limit when it is limit or more.
func editDistance(a, b string, limit int) int {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d >= limit || -d >= limit {
		return limit
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		low := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			low = min(low, cur[j])
		}
		if low >= limit {
			return limit
		}
		prev, cur = cur, prev
	}
	return min(prev[len(rb)], limit)
}
