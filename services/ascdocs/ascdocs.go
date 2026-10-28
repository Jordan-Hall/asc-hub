// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascdocs is the documentation of ASC Hub, shipped inside the hub: markdown pages, a search index over their
// text, the list of what is new and the popular searches. The pages are copies of docs/asc of the source (a test fails
// when they drift; `make docs-sync` copies them again), so the documentation always matches the version that runs.
package ascdocs

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed content/*.md content/manifest.json content/changelog.json
var content embed.FS

// Page is one documentation page.
type Page struct {
	Slug    string `json:"slug"`
	File    string `json:"file"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Section string `json:"section"`
}

// Entry is one line of the "New in ASC" list.
type Entry struct {
	Date    string `json:"date"` // 2026-10-20
	Kind    string `json:"kind"` // Agents, Queue, CLI
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Slug    string `json:"slug"` // the page that tells more
}

type manifest struct {
	Pages   []Page   `json:"pages"`
	Popular []string `json:"popular"`
}

// Hit is one search result.
type Hit struct {
	Page    Page
	Snippet string
	score   int
}

type doc struct {
	page  Page
	body  string
	lower string
}

var (
	once      sync.Once
	docs      []doc
	bySlug    map[string]*doc
	popular   []string
	changelog []Entry
	loadErr   error
)

func load() {
	var m manifest
	raw, err := content.ReadFile("content/manifest.json")
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil {
		loadErr = fmt.Errorf("docs manifest: %w", err)
		return
	}
	bySlug = map[string]*doc{}
	for _, p := range m.Pages {
		b, err := content.ReadFile("content/" + p.File)
		if err != nil {
			loadErr = fmt.Errorf("docs page %s: %w", p.Slug, err)
			return
		}
		d := doc{page: p, body: string(b), lower: strings.ToLower(string(b))}
		docs = append(docs, d)
	}
	for i := range docs {
		bySlug[docs[i].page.Slug] = &docs[i]
	}
	popular = m.Popular
	raw, err = content.ReadFile("content/changelog.json")
	if err == nil {
		err = json.Unmarshal(raw, &changelog)
	}
	if err != nil {
		loadErr = fmt.Errorf("docs changelog: %w", err)
	}
}

// Pages lists the pages in the order of the manifest.
func Pages() []Page {
	once.Do(load)
	out := make([]Page, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.page)
	}
	return out
}

// Changelog lists what is new, newest first.
func Changelog() []Entry {
	once.Do(load)
	out := slicesClone(changelog)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func slicesClone(in []Entry) []Entry { return append([]Entry(nil), in...) }

// Popular lists the searches shown as chips.
func Popular() []string {
	once.Do(load)
	return append([]string(nil), popular...)
}

// Err reports a damaged manifest (the hub ships it, so this only fails in a broken build).
func Err() error {
	once.Do(load)
	return loadErr
}

var relLink = regexp.MustCompile(`\]\(([A-Za-z0-9_-]+\.md)(#[^)]*)?\)`)

// Get returns a page and its markdown with the links to sibling pages turned into links of the documentation.
func Get(slug string) (Page, string, bool) {
	once.Do(load)
	d := bySlug[slug]
	if d == nil {
		return Page{}, "", false
	}
	byFile := map[string]string{}
	for _, x := range docs {
		byFile[x.page.File] = x.page.Slug
	}
	body := relLink.ReplaceAllStringFunc(d.body, func(m string) string {
		sub := relLink.FindStringSubmatch(m)
		if s, ok := byFile[sub[1]]; ok {
			return "](/-/docs/" + s + sub[2] + ")"
		}
		// a page that is not part of the shipped documentation stays on the source
		return "](" + sourceBase + sub[1] + sub[2] + ")"
	})
	return d.page, body, true
}

const (
	maxQuery   = 100
	sourceBase = "https://github.com/Jordan-Hall/asc-hub/blob/asc/v16/docs/asc/"
)

// Search finds the pages that have every word of the query, best first. A title match counts most, then the number
// of times the words occur. At most max results; a query longer than 100 characters is cut.
func Search(q string, max int) []Hit {
	once.Do(load)
	q = strings.ToLower(strings.TrimSpace(q))
	if len(q) > maxQuery {
		q = q[:maxQuery]
	}
	words := strings.Fields(q)
	if len(words) == 0 || len(words) > 8 {
		return nil
	}
	var hits []Hit
	for _, d := range docs {
		score := 0
		ok := true
		title := strings.ToLower(d.page.Title + " " + d.page.Summary)
		for _, w := range words {
			n := strings.Count(d.lower, w)
			t := strings.Count(title, w)
			if n == 0 && t == 0 {
				ok = false
				break
			}
			score += n + 20*t
		}
		if ok {
			hits = append(hits, Hit{Page: d.page, Snippet: snippet(d.body, words[0]), score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > max {
		hits = hits[:max]
	}
	return hits
}

var mdNoise = regexp.MustCompile("[#*`>|_\\[\\]]+")

// snippet is about 160 characters of the text around the first place the word occurs, without markdown marks.
func snippet(body, word string) string {
	clean := strings.Join(strings.Fields(mdNoise.ReplaceAllString(body, " ")), " ")
	i := strings.Index(strings.ToLower(clean), word)
	if i < 0 {
		i = 0
	}
	from := max(0, i-60)
	to := min(len(clean), i+100)
	for from > 0 && clean[from]&0xC0 == 0x80 { // do not cut a character in two
		from--
	}
	for to < len(clean) && clean[to]&0xC0 == 0x80 {
		to++
	}
	s := clean[from:to]
	if from > 0 {
		s = "…" + s
	}
	if to < len(clean) {
		s += "…"
	}
	return s
}
