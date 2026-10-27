// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"
)

// The scope switcher of the repository search page (design board Search): what a search looks at.
//
//	scope=version   this version: a branch or a commit id (ref), the default branch when empty
//	scope=change    one change: the change of the stacked pull request pull, at its current head
//	scope=stack     a stack: the top of the stack that ends in the change of the pull request pull;
//	                with diff=1 only the files the stack changes
//	scope=branches  all branches: every branch of the repository, up to ascSearchMaxBranches, with the
//	                same file on several branches shown once
//	scope=repos     all repositories: the explore search page with the same question
//
// Trust. The pages use the internal token, so the scopes are restricted to what Forgejo resolved itself.
// A change is never taken from the request: it comes from the row that links a pull request of this
// repository to its change, and the viewer must be able to read the pull requests. A branch must exist in the
// repository (the branches come from Forgejo's own list, not the request), and a commit id is checked as 40
// hex digits. A draft of someone else is out of reach because a change that has no pull request here is
// never searched.
const (
	ascSearchScopeVersion  = "version"
	ascSearchScopeChange   = "change"
	ascSearchScopeStack    = "stack"
	ascSearchScopeBranches = "branches"
	ascSearchScopeRepos    = "repos"

	// ascSearchMaxBranches bounds the branches of an all-branches search: one search each.
	ascSearchMaxBranches = 20
	// ascSearchBranchConcurrency bounds the parallel searches of an all-branches search.
	ascSearchBranchConcurrency = 4
	ascSearchMaxPull           = 1 << 40
)

// ascSearchTarget is one place a search looks at: a scope for asc, a label for the person, and the prefix
// of the links to its files.
type ascSearchTarget struct {
	scope  *ascv1.Scope
	label  string
	base   string
	branch string // the branch, when the target is a branch of an all-branches search
	link   string // a link to the branch tree, for the same
}

// ascSearchBranch names a branch the same hit was found on.
type ascSearchBranch struct {
	Name string
	Link string
}

// ascSearchScopeOK checks the raw scope field.
func ascSearchScopeOK(s string) string {
	switch s {
	case ascSearchScopeChange, ascSearchScopeStack, ascSearchScopeBranches, ascSearchScopeRepos:
		return s
	}
	return ascSearchScopeVersion
}

// ascSearchExploreURL is the address of the explore search page with this question.
func ascSearchExploreURL(q ascSearchQuery) string {
	v := url.Values{}
	v.Set("q", q.text)
	switch q.mode {
	case ascSearchModeSymbol:
		v.Set("mode", "symbol")
	case ascSearchModeSemantic:
		v.Set("mode", "meaning")
	default:
		v.Set("mode", "text")
		v.Set("match", q.match)
		if q.caseSensitive {
			v.Set("case", "1")
		}
	}
	if q.lang != "" {
		v.Set("lang", q.lang)
	}
	if q.path != "" {
		v.Set("path", q.path)
	}
	return setting.AppSubURL + "/explore/code?" + v.Encode()
}

// ascSearchURL is the address of this page with the query, changed by set (a key set to "" is dropped).
func ascSearchURL(repoLink string, q ascSearchQuery, set map[string]string) string {
	v := url.Values{}
	add := func(k, val string) {
		if o, ok := set[k]; ok {
			val = o
		}
		if val != "" {
			v.Set(k, val)
		}
	}
	add("q", q.text)
	add("mode", q.mode)
	if q.mode == ascSearchModeLexical {
		add("match", q.match)
		if q.caseSensitive {
			add("case", "1")
		}
	}
	add("scope", q.scope)
	add("ref", q.rawRef)
	add("pull", q.rawPull)
	if q.stackDiff {
		add("diff", "1")
	}
	add("lang", q.lang)
	add("path", q.path)
	return repoLink + "/asc/search?" + v.Encode()
}

// ascSearchTargets resolves the scope of the query to the places to look at. When it fails it returns the
// HTTP status of the page.
func ascSearchTargets(ctx *context.Context, repo *repo_model.Repository, name string, q *ascSearchQuery) ([]ascSearchTarget, int) {
	repoLink := ctx.Repo.RepoLink
	at := func(scope *ascv1.Scope) *ascv1.Scope {
		if q.path != "" {
			scope.Paths = []string{q.path}
		}
		if q.lang != "" {
			scope.Langs = []string{q.lang}
		}
		return scope
	}
	ref := &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT}

	switch q.scope {
	case ascSearchScopeChange, ascSearchScopeStack:
		if !ctx.Repo.CanRead(unit.TypePullRequests) {
			return nil, http.StatusNotFound
		}
		idx, err := strconv.ParseInt(q.rawPull, 10, 64)
		if err != nil || idx <= 0 || idx > ascSearchMaxPull {
			return nil, http.StatusBadRequest
		}
		pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, idx)
		if err != nil || pr.BaseRepoID != repo.ID {
			return nil, http.StatusNotFound
		}
		stack, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
		if err != nil {
			log.Error("asc search: stack of pull request %d: %v", pr.ID, err)
			return nil, http.StatusInternalServerError
		}
		if stack == nil || stack.RepoID != repo.ID {
			return nil, http.StatusNotFound
		}
		scope, err := ascsearch.AtChange(name, stack.AscChange, q.scope == ascSearchScopeStack, q.stackDiff)
		if err != nil {
			return nil, http.StatusNotFound
		}
		// The files are linked at the head of the pull request, or at its base when the head is not known.
		base := repoLink + "/src/branch/" + util.PathEscapeSegments(pr.BaseBranch)
		if sha := ascSearchPullHead(ctx, pr); sha != "" {
			base = repoLink + "/src/commit/" + sha
		}
		label := "change of #" + strconv.FormatInt(idx, 10)
		if q.scope == ascSearchScopeStack {
			label = "stack up to #" + strconv.FormatInt(idx, 10)
			if q.stackDiff {
				label += " (changed files)"
			}
		}
		return []ascSearchTarget{{scope: at(scope), label: label, base: base}}, 0

	case ascSearchScopeBranches:
		names, err := git_model.FindBranchNames(ctx, git_model.FindBranchOptions{
			RepoID: repo.ID, IsDeletedBranch: optional.Some(false),
		})
		if err != nil {
			log.Error("asc search: branches of repository %d: %v", repo.ID, err)
			return nil, http.StatusInternalServerError
		}
		names = slices.DeleteFunc(names, func(b string) bool { return !ascSymbolsBranchOK(b) })
		slices.Sort(names)
		// The default branch first, so its answer is the one the same file is shown under.
		if i := slices.Index(names, repo.DefaultBranch); i > 0 {
			names = slices.Insert(slices.Delete(names, i, i+1), 0, repo.DefaultBranch)
		}
		if len(names) == 0 {
			return nil, http.StatusNotFound
		}
		if len(names) > ascSearchMaxBranches {
			names = names[:ascSearchMaxBranches]
			ctx.Data["AscSearchBranchesCut"] = true
		}
		out := make([]ascSearchTarget, 0, len(names))
		for _, b := range names {
			out = append(out, ascSearchTarget{
				scope:  at(&ascv1.Scope{Repo: ref, At: &ascv1.Scope_Rev{Rev: "refs/heads/" + b}}),
				label:  b,
				base:   repoLink + "/src/branch/" + util.PathEscapeSegments(b),
				branch: b,
				link:   repoLink + "/src/branch/" + util.PathEscapeSegments(b),
			})
		}
		return out, 0
	}
	return []ascSearchTarget{{
		scope: at(&ascv1.Scope{Repo: ref, At: &ascv1.Scope_Rev{Rev: q.rev}}),
		label: q.label,
		base:  q.base,
	}}, 0
}

// ascSearchPullHead is the commit id at the head of a pull request, or "" when it cannot be read.
func ascSearchPullHead(ctx *context.Context, pr *issues_model.PullRequest) string {
	if pr.BaseRepo == nil {
		if err := pr.LoadBaseRepo(ctx); err != nil {
			return ""
		}
	}
	g, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, pr.BaseRepo)
	if err != nil {
		return ""
	}
	defer closer.Close()
	id, err := g.GetRefCommitID(pr.GetGitRefName())
	if err != nil || !ascSymbolsSHA.MatchString(id) {
		return ""
	}
	return id
}

// ascSearchTargetLabel is what the page says the answer is about.
func ascSearchTargetLabel(q ascSearchQuery, targets []ascSearchTarget) string {
	if q.scope == ascSearchScopeBranches {
		return strconv.Itoa(len(targets)) + " branches"
	}
	return targets[0].label
}

// ascSearchPart is the answer for one target.
type ascSearchPart struct {
	target ascSearchTarget
	res    ascSearchResult
	err    error
}

// ascSearchRun searches every target, at most ascSearchBranchConcurrency at once, and merges the answers.
// failed counts the targets whose search failed; when all failed, or there was one, the error is returned.
func ascSearchRun(ctx gocontext.Context, api ascv1.CodeIntelClient, q ascSearchQuery, targets []ascSearchTarget) (res ascSearchResult, failed int, err error) {
	run := func(t ascSearchTarget) (ascSearchResult, error) {
		qt := q
		qt.base, qt.label = t.base, t.label
		switch q.mode {
		case ascSearchModeLexical:
			return ascSearchLexical(ctx, api, t.scope, qt)
		case ascSearchModeSymbol:
			return ascSearchSymbols(ctx, api, t.scope, qt)
		}
		return ascSearchSemantic(ctx, api, t.scope, qt)
	}
	if len(targets) == 1 {
		r, err := run(targets[0])
		return r, 0, err
	}
	parts := make([]ascSearchPart, len(targets))
	sem := make(chan struct{}, ascSearchBranchConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			parts[i].target = t
			parts[i].res, parts[i].err = run(t)
		}()
	}
	wg.Wait()

	var first error
	var ok []ascSearchPart
	for _, p := range parts {
		switch {
		case p.err == nil:
			ok = append(ok, p)
		case errors.Is(p.err, errAscSearchBadAnswer) || !ascSearchRefused(p.err):
			failed++
			if first == nil {
				first = p.err
			}
		}
		// A branch that asc does not know yet (for instance one that was just pushed) adds nothing.
	}
	if len(ok) == 0 && first != nil {
		return ascSearchResult{}, failed, first
	}
	return ascSearchMerge(ok), failed, nil
}

// ascSearchRefused reports an answer that means "nothing here": asc does not know the branch.
func ascSearchRefused(err error) bool {
	return ascsearch.KindOf(err) == ascsearch.KindNotFound
}

// ascSearchMerge joins the answers of several branches: the same file (same path and content), symbol or
// chunk on several branches is listed once, with the branches it was found on.
func ascSearchMerge(parts []ascSearchPart) ascSearchResult {
	out := ascSearchResult{resolved: -1}
	files := map[string]int{}
	syms := map[string]int{}
	chunks := map[string]int{}
	for _, p := range parts {
		br := ascSearchBranch{Name: p.target.branch, Link: p.target.link}
		out.truncated = out.truncated || p.res.truncated
		if out.model == "" {
			out.model = p.res.model
		}
		if r := p.res.resolved; r >= 0 && (out.resolved < 0 || r < out.resolved) {
			out.resolved = r
		}
		for _, f := range p.res.files {
			key := p.target.branch + "\x00" + f.Path
			if f.FileID != "" {
				key = f.FileID + "\x00" + f.Path
			}
			if i, ok := files[key]; ok {
				out.files[i].Branches = append(out.files[i].Branches, br)
				continue
			}
			files[key] = len(out.files)
			f.Branches = []ascSearchBranch{br}
			out.files = append(out.files, f)
		}
		for _, s := range p.res.symbols {
			key := strings.Join([]string{s.Path, strconv.FormatUint(uint64(s.Line), 10), s.Name, s.Kind, s.Container, s.Signature}, "\x00")
			if i, ok := syms[key]; ok {
				out.symbols[i].Branches = append(out.symbols[i].Branches, br)
				continue
			}
			syms[key] = len(out.symbols)
			s.Branches = []ascSearchBranch{br}
			out.symbols = append(out.symbols, s)
		}
		for _, c := range p.res.chunks {
			key := strings.Join([]string{c.Path, strconv.FormatUint(uint64(c.StartLine), 10), strconv.FormatUint(uint64(c.EndLine), 10), c.Text}, "\x00")
			if i, ok := chunks[key]; ok {
				out.chunks[i].Branches = append(out.chunks[i].Branches, br)
				continue
			}
			chunks[key] = len(out.chunks)
			c.Branches = []ascSearchBranch{br}
			out.chunks = append(out.chunks, c)
		}
	}
	return out
}

// ascSearchFacet is a filter link with the number of files behind it.
type ascSearchFacet struct {
	Name   string
	Count  int
	URL    string
	Active bool
}

// ascSearchFacets counts the files of the answer by language and by top-level directory, as links that
// narrow the search.
func ascSearchFacets(repoLink string, q ascSearchQuery, res ascSearchResult) (langs, dirs []ascSearchFacet) {
	fs := make([]ascsearch.FileHit, 0, len(res.files))
	for _, f := range res.files {
		fs = append(fs, ascsearch.FileHit{Path: f.Path, Lang: f.Lang})
	}
	l, d := ascsearch.Facets([]ascsearch.RepoResult{{Files: fs}})
	for _, f := range l {
		langs = append(langs, ascSearchFacet{
			Name: f.Name, Count: f.Count, Active: q.lang == f.Name,
			URL: ascSearchURL(repoLink, q, map[string]string{"lang": f.Name}),
		})
	}
	for _, f := range d {
		if f.Name == "(root)" {
			continue
		}
		dir := strings.TrimSuffix(f.Name, "/")
		dirs = append(dirs, ascSearchFacet{
			Name: f.Name, Count: f.Count, Active: q.path == dir,
			URL: ascSearchURL(repoLink, q, map[string]string{"path": dir}),
		})
	}
	return langs, dirs
}
