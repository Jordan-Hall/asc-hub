// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"encoding/hex"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	git_model "forgejo.org/models/git"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The symbols behind a text search (board Search). When a word of the query is the name of a symbol declared
// in the searched version, the page says so ("Symbols matched: RetryPolicy, retry") and the files that declare
// it carry chips: the symbol, how many references it has in other files, and when the file last changed.
//
// This is advice about the answer: one DEF and one REFS call per word, each failing quietly, and only for a
// search of one version (a branch or a commit), the place a definition and a last change are about.

const (
	// ascSearchDefMaxWords words of the query are looked up, ascSearchDefMaxFiles files get chips.
	ascSearchDefMaxWords = 4
	ascSearchDefMaxFiles = 10
)

var ascSearchDefWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ascSearchDef is a symbol a file declares.
type ascSearchDef struct {
	Label string // Container::Name, or the name
	Link  string // the declaration
	// Refs counts the references in other files; RefsMore says the answer was cut, so there are more.
	Refs     int
	RefsMore bool
	RefsLink string // the definition and references page
}

// ascSearchDefWords are the words of a text query worth looking up as symbols.
func ascSearchDefWords(q ascSearchQuery) []string {
	if q.mode != ascSearchModeLexical || q.match == ascSearchMatchRegex {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(q.text) {
		if ascSearchDefWord.MatchString(w) && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	if len(out) > ascSearchDefMaxWords {
		return nil // a sentence, not names
	}
	return out
}

// ascSearchDefinitions finds the words of the query that name a declared symbol, and puts chips on the files
// of res that declare them. It returns the matched words.
func ascSearchDefinitions(ctx gocontext.Context, api ascv1.CodeIntelClient, scope *ascv1.Scope, repoLink string, q ascSearchQuery, res *ascSearchResult) []string {
	words := ascSearchDefWords(q)
	if len(words) == 0 || len(res.files) == 0 {
		return nil
	}
	byPath := map[string]int{}
	for i, f := range res.files {
		byPath[f.Path] = i
	}
	call := func(op ascv1.SymbolOp, name string) ([]ascSymbolsRow, bool, bool) {
		resp, err := api.Symbols(ctx, &ascv1.SymbolsRequest{
			Scope: scope, Op: op, Name: name, Budget: &ascv1.Budget{MaxResults: ascSymbolsMaxSymbols},
		})
		if err != nil {
			return nil, false, false
		}
		rows, truncated, err := ascSymbolsBuild(resp, ascSymbolsQuery{base: q.base})
		if err != nil {
			return nil, false, false
		}
		return rows, truncated, true
	}
	var matched []string
	chips := 0
	for _, w := range words {
		rows, _, ok := call(ascv1.SymbolOp_SYMBOL_OP_DEF, w)
		if !ok {
			continue
		}
		defs := map[string]bool{}
		var mine []ascSymbolsRow
		for _, r := range rows {
			if r.IsDef && r.Name == w {
				defs[r.Path] = true
				mine = append(mine, r)
			}
		}
		if len(mine) == 0 {
			continue
		}
		matched = append(matched, w)
		refs, more, _ := call(ascv1.SymbolOp_SYMBOL_OP_REFS, w)
		elsewhere := 0
		for _, r := range refs {
			if !r.IsDef && !defs[r.Path] {
				elsewhere++
			}
		}
		v := url.Values{"op": {"def"}, "name": {w}}
		if q.rawRef != "" {
			v.Set("ref", q.rawRef)
		}
		for _, r := range mine {
			i, ok := byPath[r.Path]
			if !ok || chips >= ascSearchDefMaxFiles {
				continue
			}
			label := r.Name
			if r.Container != "" {
				label = r.Container + "::" + r.Name
			}
			res.files[i].Defs = append(res.files[i].Defs, ascSearchDef{
				Label: label, Link: r.Link, Refs: elsewhere, RefsMore: more,
				RefsLink: repoLink + "/asc/symbols?" + v.Encode(),
			})
			chips++
		}
	}
	return matched
}

// ascSearchChanged puts on each file with chips the commit that last changed it at the searched version,
// from asc's Repo.LastCommits (one call per directory). A failure leaves the files as they are.
func ascSearchChanged(ctx *context.Context, repoAPI ascv1.RepoClient, callCtx gocontext.Context, name string, q ascSearchQuery, files []ascSearchFile) {
	dirs := map[string][]string{}
	for _, f := range files {
		if len(f.Defs) > 0 {
			dir, base := path.Split(f.Path)
			dir = strings.TrimSuffix(dir, "/")
			dirs[dir] = append(dirs[dir], base)
		}
	}
	if len(dirs) == 0 {
		return
	}
	commit := q.rev
	if b, ok := strings.CutPrefix(q.rev, "refs/heads/"); ok {
		br, err := git_model.GetBranch(ctx, ctx.Repo.Repository.ID, b)
		if err != nil {
			return
		}
		commit = br.CommitID
	}
	if !ascSymbolsSHA.MatchString(commit) {
		return
	}
	for dir, names := range dirs {
		wire := make([][]byte, len(names))
		for i, n := range names {
			wire[i] = []byte(n)
		}
		resp, err := repoAPI.LastCommits(callCtx, &ascv1.LastCommitsRequest{
			Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT}, Rev: commit, Dir: []byte(dir), Names: wire,
		})
		if err != nil {
			log.Debug("asc search: last commits of %q: %v", dir, err)
			continue
		}
		for base, entry := range resp.GetByName() {
			id := entry.GetGit()
			when := entry.GetCommitter().GetTime()
			if len(id) != 20 || when <= 0 {
				continue
			}
			p := base
			if dir != "" {
				p = dir + "/" + base
			}
			for i := range files {
				if files[i].Path == p && len(files[i].Defs) > 0 {
					files[i].Changed = time.Unix(when, 0)
					files[i].ChangedLink = ctx.Repo.RepoLink + "/commit/" + hex.EncodeToString(id)
				}
			}
		}
	}
}
