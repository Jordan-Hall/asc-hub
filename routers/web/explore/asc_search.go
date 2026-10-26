// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	gocontext "context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"
)

// The code search page of the explore tab (SPEC 23.8, BUILDOUT section 14): `GET /explore/code` searches
// the code of every repository the signed-in user, or a visitor, can read that lives in asc. The mode is the
// tab of the Search board: text (CrossSearch.Search), symbol (CrossSearch.LookupSymbol) or meaning
// (CodeIntel.Semantic of each repository). The scope is all repositories or the repositories of one owner;
// the scope of one repository (a version, a change or a stack, all branches) is on the search page of
// the repository, which the page links to.
//
// Trust. A browser session holds no asc token, so the calls carry the internal token, with which asc would
// answer for every repository. Forgejo therefore decides: ascsearch.Resolve returns the repositories this
// user may read and every call names exactly those, and everything asc answers is checked against that list.
// A repository the user cannot read is not searched, not counted and not mentioned. All text from asc is
// untrusted file content: it is cut, stripped of control and bidirectional characters, left to the template
// to escape, and empty for a hit asc marked redacted. The text of an asc error never reaches the page.
const (
	tplAscExploreSearch base.TplName = "explore/asc_search"

	ascExploreModeText    = "text"
	ascExploreModeSymbol  = "symbol"
	ascExploreModeMeaning = "meaning"

	ascExploreMatchLiteral = "literal"
	ascExploreMatchWord    = "word"
	ascExploreMatchRegex   = "regex"

	// ascExploreMaxField bounds a form field; one more byte is read so a longer one is seen as too long.
	ascExploreMaxField = 4096
	// ascExploreMaxConcurrent bounds the searches in flight; a visitor can start one without signing in.
	ascExploreMaxConcurrent = 8
	// ascExploreMaxWords and ascExploreMaxWordLen bound the identifiers looked up beside a text search.
	ascExploreMaxWords   = 3
	ascExploreMaxWordLen = 64
	// ascExploreTimeout bounds one page: the search, the symbol lookup and the coverage note.
	ascExploreTimeout = 60 * time.Second
)

var (
	ascExploreSlots = make(chan struct{}, ascExploreMaxConcurrent)
	// ascExploreIdent is a word that can be the name of a symbol.
	ascExploreIdent = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_]*$`)
)

// ascExploreQuery is the checked query string.
type ascExploreQuery struct {
	mode  string
	text  string
	match string
	cs    bool
	owner string
	lang  string
	path  string
	token string
}

// ascExploreFacet is a filter link with the number of files behind it.
type ascExploreFacet struct {
	Name   string
	Count  int
	URL    string
	Active bool
}

// ascExploreSeg is a piece of a matching line; Hit pieces are drawn marked.
type ascExploreSeg struct {
	Text string
	Hit  bool
}

// ascExploreLine is one line of a file hit.
type ascExploreLine struct {
	Number   uint32
	Link     string
	IsMatch  bool
	Segments []ascExploreSeg
}

// ascExploreFile is one checked file hit.
type ascExploreFile struct {
	Path       string
	Link       string
	Lang       string
	Redacted   bool
	Definition bool
	SymbolLink string
	Also       []ascExploreAlso
	Lines      []ascExploreLine
}

// ascExploreAlso is another path of the same file.
type ascExploreAlso struct{ Path, Link string }

// ascExploreSymbol is one symbol hit.
type ascExploreSymbol struct {
	Name       string
	Kind       string
	IsDef      bool
	Path       string
	Line       uint32
	Link       string
	PanelLink  string
	Signature  string
	Container  string
	Doc        string
	Confidence string
}

// ascExploreChunk is one chunk of the search by meaning.
type ascExploreChunk struct {
	Path      string
	Link      string
	StartLine uint32
	EndLine   uint32
	Symbol    string
	Score     string
	Sources   []string
	Text      string
	Redacted  bool
}

// ascExploreGroup is everything found in one repository.
type ascExploreGroup struct {
	Name    string // owner/name as Forgejo shows it
	Link    string
	Files   []ascExploreFile
	Symbols []ascExploreSymbol
	Chunks  []ascExploreChunk
}

// ascExploreCoverage is the coverage note.
type ascExploreCoverage struct {
	Known     bool
	Complete  bool
	Percent   int
	Asked     int
	Reporting int
	Lag       int64
	Revision  string
	Sampled   bool
}

// CodeSearch serves `GET /explore/code`: the asc search page while the integration is on, the stock page
// (Forgejo's own code indexer) otherwise.
func CodeSearch(ctx *context.Context) {
	if setting.Asc.Enabled {
		AscExploreSearch(ctx)
		return
	}
	Code(ctx)
}

// ascExploreRaw reads one form field, bounded and valid UTF-8, so it can be shown again.
func ascExploreRaw(ctx *context.Context, key string) (string, bool) {
	s := ctx.FormString(key)
	if len(s) > ascExploreMaxField {
		return "", false
	}
	return strings.ToValidUTF8(s, ""), true
}

// ascExploreParse reads and checks the query string. ok is false when it fails a check; the query still
// carries what the form shows again.
func ascExploreParse(ctx *context.Context) (q ascExploreQuery, msg string) {
	var good = true
	get := func(key string) string {
		s, ok := ascExploreRaw(ctx, key)
		good = good && ok
		return strings.TrimSpace(s)
	}
	q = ascExploreQuery{
		mode: get("mode"), text: get("q"), match: get("match"), cs: ctx.FormBool("case"),
		owner: get("owner"), lang: get("lang"), path: get("path"), token: get("page_token"),
	}
	switch q.mode {
	case ascExploreModeText, ascExploreModeSymbol, ascExploreModeMeaning:
	default:
		q.mode = ascExploreModeText
	}
	switch q.match {
	case ascExploreMatchLiteral, ascExploreMatchWord, ascExploreMatchRegex:
	default:
		q.match = ascExploreMatchLiteral
	}
	if !good {
		return q, "A field of the search is too long."
	}
	if q.text == "" {
		return q, ""
	}
	limit, what := ascsearch.MaxPattern, "text"
	switch q.mode {
	case ascExploreModeSymbol:
		limit, what = ascsearch.MaxName, "symbol name"
	case ascExploreModeMeaning:
		limit, what = ascsearch.MaxSemantic, "question"
	}
	switch {
	case !ascsearch.Plain(q.text, limit):
		return q, "The " + what + " is too long or holds characters that cannot be searched for."
	case q.mode == ascExploreModeText && len(q.text) < ascsearch.MinPattern:
		return q, "Type at least " + strconv.Itoa(ascsearch.MinPattern) + " characters to search the text of code."
	}
	if q.owner != "" && (!ascsearch.Plain(q.owner, 255) || strings.Contains(q.owner, "/")) {
		return q, "The owner is not a name."
	}
	if q.lang != "" && !ascsearch.CheckLang(q.lang) {
		return q, "The language is not a language name."
	}
	if p, ok := ascsearch.CheckPath(q.path); ok {
		q.path = strings.TrimSuffix(p, "/")
	} else {
		return q, "The path is not a path."
	}
	if !ascsearch.CheckToken(q.token) {
		return q, "The page token is not valid."
	}
	return q, ""
}

// url builds a link to this page with the query, changed by set (a key set to "" is dropped).
func (q ascExploreQuery) url(set map[string]string) string {
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
	if q.mode == ascExploreModeText {
		add("match", q.match)
		if q.cs {
			add("case", "1")
		}
	}
	add("owner", q.owner)
	add("lang", q.lang)
	add("path", q.path)
	add("page_token", "")
	for k, val := range set {
		if val != "" && !v.Has(k) {
			v.Set(k, val)
		}
	}
	return setting.AppSubURL + "/explore/code?" + v.Encode()
}

func ascExploreShowQuery(ctx *context.Context, q ascExploreQuery) {
	ctx.Data["AscMode"] = q.mode
	ctx.Data["AscText"] = q.text
	ctx.Data["AscMatch"] = q.match
	ctx.Data["AscCase"] = q.cs
	ctx.Data["AscOwner"] = q.owner
	ctx.Data["AscLang"] = q.lang
	ctx.Data["AscPath"] = q.path
	ctx.Data["AscModeLinks"] = []ascExploreFacet{
		{Name: "Text", URL: q.url(map[string]string{"mode": ascExploreModeText}), Active: q.mode == ascExploreModeText},
		{Name: "Symbol", URL: q.url(map[string]string{"mode": ascExploreModeSymbol}), Active: q.mode == ascExploreModeSymbol},
		{Name: "Meaning", URL: q.url(map[string]string{"mode": ascExploreModeMeaning}), Active: q.mode == ascExploreModeMeaning},
	}
	if q.lang != "" {
		ctx.Data["AscClearLang"] = q.url(map[string]string{"lang": ""})
	}
	if q.path != "" {
		ctx.Data["AscClearPath"] = q.url(map[string]string{"path": ""})
	}
	if q.owner != "" {
		ctx.Data["AscClearOwner"] = q.url(map[string]string{"owner": ""})
	}
}

func ascExploreFail(ctx *context.Context, status int, msg string) {
	ctx.Data["AscError"] = status
	ctx.Data["AscErrorText"] = msg
	ctx.HTML(status, tplAscExploreSearch)
}

// ascExploreFailErr turns a failed search into a page. Only the kind of the failure is used.
func ascExploreFailErr(ctx *context.Context, op string, err error) {
	if ctx.Req.Context().Err() != nil {
		return // the browser went away
	}
	kind := ascsearch.KindOf(err)
	switch kind {
	case ascsearch.KindFailed, ascsearch.KindBadAnswer, ascsearch.KindUnavailable, ascsearch.KindTimeout, ascsearch.KindNoCredential:
		log.Error("asc explore %s: %v", op, err) // the status text of asc stays in the log
	}
	msg := "Code search is not available right now."
	switch kind {
	case ascsearch.KindBadRequest:
		msg = "This search is not valid. Check the text, the filters and the owner."
		if errors.Is(err, ascsearch.ErrInput) {
			msg = ascsearch.PublicMessage(err) + "."
		}
	case ascsearch.KindTooBroad:
		msg = "This search matches too much. Type something more specific."
	case ascsearch.KindLimited:
		msg = "Too many searches right now. Try again in a moment."
	case ascsearch.KindTimeout:
		msg = "The search took too long. Narrow it with an owner, a language or a path."
	case ascsearch.KindNotFound:
		msg = "Nothing was found."
	}
	ascExploreFail(ctx, ascsearch.PageStatus(err), msg)
}

// AscExploreSearch renders `/explore/code` from asc.
func AscExploreSearch(ctx *context.Context) {
	if setting.Service.Explore.DisableCodePage {
		ctx.Redirect(setting.AppSubURL + "/explore")
		return
	}
	ctx.Data["UsersPageIsDisabled"] = setting.Service.Explore.DisableUsersPage
	ctx.Data["OrganizationsPageIsDisabled"] = setting.Service.Explore.DisableOrganizationsPage
	ctx.Data["IsRepoIndexerEnabled"] = true
	ctx.Data["Title"] = "Search code"
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreCode"] = true
	ctx.Data["PageIsViewCode"] = true

	q, msg := ascExploreParse(ctx)
	ascExploreShowQuery(ctx, q)
	if msg != "" {
		ascExploreFail(ctx, http.StatusBadRequest, msg)
		return
	}
	if q.text == "" {
		ctx.HTML(http.StatusOK, tplAscExploreSearch) // the empty form
		return
	}

	select {
	case ascExploreSlots <- struct{}{}:
		defer func() { <-ascExploreSlots }()
	default:
		ascExploreFail(ctx, http.StatusTooManyRequests, "Too many searches right now. Try again in a moment.")
		return
	}

	svc, err := ascsearch.Default()
	if err != nil {
		log.Error("asc explore: no connection to asc serve: %v", err)
		ascExploreFail(ctx, http.StatusBadGateway, "Code search is not available right now.")
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascExploreTimeout)
	defer cancel()

	// Forgejo decides which repositories are searched.
	max := ascsearch.MaxRepos
	if q.mode == ascExploreModeMeaning {
		max = ascsearch.MaxSemanticRepos
	}
	resolved, err := ascsearch.Resolve(ctx, ctx.Doer, nil, ascsearch.Selector{Owner: q.owner, Max: max})
	if err != nil {
		if errors.Is(err, ascsearch.ErrBadSelector) {
			ascExploreFail(ctx, http.StatusBadRequest, "The owner is not a name.")
			return
		}
		ctx.ServerError("ascsearch.Resolve", err)
		return
	}
	ctx.Data["AscReposTotal"] = resolved.Total
	ctx.Data["AscReposSearched"] = len(resolved.Repos)
	ctx.Data["AscReposTruncated"] = resolved.Truncated
	ctx.Data["AscMeaningCap"] = ascsearch.MaxSemanticRepos

	start := time.Now()
	view := &ascExploreView{q: q, repos: resolved, ctx: ctx}
	switch q.mode {
	case ascExploreModeText:
		err = view.text(callCtx, svc)
	case ascExploreModeSymbol:
		err = view.symbols(callCtx, svc)
	default:
		err = view.meaning(callCtx, svc)
	}
	if err != nil {
		ascExploreFailErr(ctx, q.mode, err)
		return
	}
	ctx.Data["AscDone"] = true
	ctx.Data["AscElapsedMS"] = time.Since(start).Milliseconds()
	ctx.Data["AscGroups"] = view.groups
	ctx.Data["AscCount"] = view.count
	ctx.Data["AscFileCount"] = view.files
	ctx.Data["AscRepoCount"] = len(view.groups)
	ctx.Data["AscTruncated"] = view.truncated
	ctx.Data["AscFailed"] = view.failed
	ctx.Data["AscFallback"] = view.fallback
	ctx.Data["AscModel"] = view.model
	ctx.Data["AscMatched"] = view.matched
	ctx.Data["AscLangFacets"] = view.langs
	ctx.Data["AscDirFacets"] = view.dirs
	if view.next != "" {
		ctx.Data["AscMore"] = q.url(map[string]string{"page_token": view.next})
	}

	// The coverage note is advice about the answer, so its failure never fails the page.
	index := ascsearch.IndexFiles
	if q.mode == ascExploreModeMeaning {
		index = ascsearch.IndexVectors
	}
	cov := svc.Coverage(callCtx, resolved.Repos, index)
	ctx.Data["AscCoverage"] = ascExploreCoverage{
		Known: cov.Known > 0, Complete: cov.Complete, Percent: cov.Percent, Asked: cov.Asked, Reporting: cov.Known,
		Lag: cov.LagSeconds, Revision: cov.Revision, Sampled: cov.Sampled,
	}
	ctx.HTML(http.StatusOK, tplAscExploreSearch)
}

// ascExploreView collects what the page draws from the answers.
type ascExploreView struct {
	q     ascExploreQuery
	repos *ascsearch.Resolved
	ctx   *context.Context

	groups    []ascExploreGroup
	count     int
	files     int
	truncated bool
	failed    int
	fallback  bool
	next      string
	model     string
	matched   []string
	langs     []ascExploreFacet
	dirs      []ascExploreFacet
}

// repoOf finds the Forgejo view of an asc repository name; names asc made up are not in the list.
func (v *ascExploreView) repoOf(name string) (ascsearch.Repo, bool) {
	return v.repos.ByName(name)
}

// fileLink is the address of a path in the default branch of a repository, with an optional line anchor.
func fileLink(r ascsearch.Repo, path string, start, end uint32) string {
	link := r.Link + "/src/branch/" + util.PathEscapeSegments(r.Branch) + "/" + util.PathEscapeSegments(path)
	switch {
	case start > 0 && end > start:
		link += "#L" + strconv.FormatUint(uint64(start), 10) + "-L" + strconv.FormatUint(uint64(end), 10)
	case start > 0:
		link += "#L" + strconv.FormatUint(uint64(start), 10)
	}
	return link
}

func (v *ascExploreView) scope() (paths, langs []string) {
	if v.q.path != "" {
		paths = []string{v.q.path}
	}
	if v.q.lang != "" {
		langs = []string{v.q.lang}
	}
	return paths, langs
}

// facets turns counts into filter links.
func (v *ascExploreView) facets(langs, dirs []ascsearch.Facet) {
	for _, f := range langs {
		v.langs = append(v.langs, ascExploreFacet{
			Name: f.Name, Count: f.Count, Active: v.q.lang == f.Name,
			URL: v.q.url(map[string]string{"lang": f.Name}),
		})
	}
	for _, f := range dirs {
		dir := strings.TrimSuffix(f.Name, "/")
		if f.Name == "(root)" {
			continue // the files in the root have no directory to filter by
		}
		v.dirs = append(v.dirs, ascExploreFacet{
			Name: f.Name, Count: f.Count, Active: v.q.path == dir,
			URL: v.q.url(map[string]string{"path": dir}),
		})
	}
}

func (v *ascExploreView) text(ctx gocontext.Context, svc *ascsearch.Service) error {
	mode := ascv1.SearchMode_SEARCH_MODE_LITERAL
	switch v.q.match {
	case ascExploreMatchWord:
		mode = ascv1.SearchMode_SEARCH_MODE_WORD
	case ascExploreMatchRegex:
		mode = ascv1.SearchMode_SEARCH_MODE_REGEX
	}
	paths, langs := v.scope()
	res, err := svc.Text(ctx, v.repos.Repos, ascsearch.TextQuery{
		Pattern: v.q.text, Mode: mode, CaseSensitive: v.q.cs, Context: 1, Paths: paths, Langs: langs,
		Limit: ascsearch.DefaultLimit, PageToken: v.q.token,
	})
	if err != nil {
		return err
	}
	v.truncated, v.failed, v.fallback, v.next = res.Truncated, res.Failed, res.Fallback, res.NextPageToken
	pattern := v.q.text
	if mode == ascv1.SearchMode_SEARCH_MODE_REGEX {
		pattern = "" // the width of a regular expression match is not known
	}

	// Definitions of the words of the query, to mark the files that declare them.
	defs, matched := v.definitions(ctx, svc)
	v.matched = matched

	for _, rr := range res.Repos {
		r, ok := v.repoOf(rr.Repo)
		if !ok {
			continue
		}
		g := ascExploreGroup{Name: r.FullName, Link: r.Link}
		for _, f := range rr.Files {
			file := ascExploreFile{
				Path: f.Path, Link: fileLink(r, f.Path, 0, 0), Lang: f.Lang, Redacted: f.Redacted,
			}
			if name, isDef := defs[rr.Repo+"\x00"+f.Path]; isDef {
				file.Definition = true
				file.SymbolLink = r.Link + "/asc/symbols?op=def&name=" + url.QueryEscape(name)
			}
			for _, a := range f.AlsoAt {
				file.Also = append(file.Also, ascExploreAlso{Path: a, Link: fileLink(r, a, 0, 0)})
			}
			file.Lines = ascExploreLines(r, f, pattern)
			v.count += len(f.Matches)
			g.Files = append(g.Files, file)
		}
		v.files += len(g.Files)
		v.groups = append(v.groups, g)
	}
	langFacets, dirFacets := ascsearch.Facets(res.Repos)
	v.facets(langFacets, dirFacets)
	return nil
}

// ascExploreLines builds the lines of a file hit: context before, the matching line with its matches marked,
// context after. A line that an earlier match already drew is not drawn twice. A redacted hit has line
// numbers only, and its lines are not drawn at all.
func ascExploreLines(r ascsearch.Repo, f ascsearch.FileHit, pattern string) []ascExploreLine {
	if f.Redacted {
		return nil
	}
	var out []ascExploreLine
	last := uint32(0)
	add := func(num uint32, l ascExploreLine) {
		switch {
		case num == 0 || num < last:
			return
		case num == last:
			// A match replaces the same line drawn as context of the match before it.
			if l.IsMatch && len(out) > 0 && !out[len(out)-1].IsMatch {
				l.Number, l.Link = num, fileLink(r, f.Path, num, 0)
				out[len(out)-1] = l
			}
			return
		}
		l.Number, l.Link = num, fileLink(r, f.Path, num, 0)
		out = append(out, l)
		last = num
	}
	for _, m := range f.Matches {
		n := m.Line
		if n < 1 {
			continue
		}
		for j, t := range m.Before {
			if num := int64(n) - int64(len(m.Before)) + int64(j); num >= 1 {
				add(uint32(num), ascExploreLine{Segments: []ascExploreSeg{{Text: t}}})
			}
		}
		var segs []ascExploreSeg
		for _, s := range ascsearch.Highlight(m.Text, m.Cols, pattern) {
			segs = append(segs, ascExploreSeg{Text: s.Text, Hit: s.Hit})
		}
		add(n, ascExploreLine{IsMatch: true, Segments: segs})
		for j, t := range m.After {
			add(n+1+uint32(j), ascExploreLine{Segments: []ascExploreSeg{{Text: t}}})
		}
	}
	return out
}

// definitions looks the identifiers of a text query up as symbols, at most ascExploreMaxWords of them, and
// returns the files that declare one (keyed by repository and path) with the name, and the names that were
// found. It is a hint beside the text hits, so any failure just leaves it empty.
func (v *ascExploreView) definitions(ctx gocontext.Context, svc *ascsearch.Service) (map[string]string, []string) {
	if v.q.match == ascExploreMatchRegex || v.q.token != "" {
		return nil, nil
	}
	words := strings.Fields(v.q.text)
	if len(words) == 0 || len(words) > ascExploreMaxWords {
		return nil, nil
	}
	_, langs := v.scope()
	defs := map[string]string{}
	var matched []string
	for _, w := range words {
		if len(w) > ascExploreMaxWordLen || !ascExploreIdent.MatchString(w) {
			continue
		}
		res, err := svc.Symbols(ctx, v.repos.Repos, ascsearch.SymbolQuery{
			Name: w, Match: ascv1.SymbolMatch_SYMBOL_MATCH_EXACT, Langs: langs, DefinitionsOnly: true, Limit: 50,
		})
		if err != nil || len(res.Hits) == 0 {
			continue
		}
		found := false
		for _, h := range res.Hits {
			if h.Symbol.Name != w {
				continue
			}
			key := h.Repo + "\x00" + h.Symbol.Path
			if _, dup := defs[key]; !dup {
				defs[key] = w
			}
			found = true
		}
		if found {
			matched = append(matched, w)
		}
	}
	return defs, matched
}

func (v *ascExploreView) symbols(ctx gocontext.Context, svc *ascsearch.Service) error {
	_, langs := v.scope()
	res, err := svc.Symbols(ctx, v.repos.Repos, ascsearch.SymbolQuery{
		Name: v.q.text, Match: ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY, Langs: langs, Limit: ascsearch.DefaultLimit, PageToken: v.q.token,
	})
	if err != nil {
		return err
	}
	v.truncated, v.failed, v.fallback, v.next = res.Truncated, res.Failed, res.Fallback, res.NextPageToken
	byRepo := map[string]*ascExploreGroup{}
	var order []string
	for _, h := range res.Hits {
		r, ok := v.repoOf(h.Repo)
		if !ok {
			continue
		}
		if v.q.path != "" && h.Symbol.Path != v.q.path && !strings.HasPrefix(h.Symbol.Path, v.q.path+"/") {
			continue
		}
		g, ok := byRepo[h.Repo]
		if !ok {
			g = &ascExploreGroup{Name: r.FullName, Link: r.Link}
			byRepo[h.Repo] = g
			order = append(order, h.Repo)
		}
		s := h.Symbol
		g.Symbols = append(g.Symbols, ascExploreSymbol{
			Name: s.Name, Kind: s.Kind, IsDef: s.IsDef, Path: s.Path, Line: s.Line,
			Link:      fileLink(r, s.Path, s.Line, 0),
			PanelLink: r.Link + "/asc/symbols?op=def&name=" + url.QueryEscape(s.Name),
			Signature: s.Signature, Container: s.Container, Doc: s.Doc, Confidence: s.Confidence,
		})
		v.count++
	}
	for _, name := range order {
		v.files += len(byRepo[name].Symbols)
		v.groups = append(v.groups, *byRepo[name])
	}
	return nil
}

func (v *ascExploreView) meaning(ctx gocontext.Context, svc *ascsearch.Service) error {
	paths, langs := v.scope()
	res, err := svc.Meaning(ctx, v.repos.Repos, ascsearch.MeaningQuery{Query: v.q.text, K: 20, Paths: paths, Langs: langs})
	if err != nil {
		return err
	}
	v.failed, v.model = res.Failed, res.Model
	byRepo := map[string]*ascExploreGroup{}
	var order []string
	for _, c := range res.Chunks {
		r, ok := v.repoOf(c.Repo)
		if !ok {
			continue
		}
		g, ok := byRepo[c.Repo]
		if !ok {
			g = &ascExploreGroup{Name: r.FullName, Link: r.Link}
			byRepo[c.Repo] = g
			order = append(order, c.Repo)
		}
		g.Chunks = append(g.Chunks, ascExploreChunk{
			Path: c.Path, Link: fileLink(r, c.Path, c.StartLine, c.EndLine), StartLine: c.StartLine, EndLine: c.EndLine,
			Symbol: c.Symbol, Score: strconv.FormatFloat(c.Score, 'f', 3, 64), Sources: c.Sources, Text: c.Text, Redacted: c.Redacted,
		})
		v.count++
	}
	for _, name := range order {
		v.files += len(byRepo[name].Chunks)
		v.groups = append(v.groups, *byRepo[name])
	}
	v.truncated = v.repos.Truncated
	return nil
}
