// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	git_model "forgejo.org/models/git"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The repository search page over asc (SPEC 23.8, PLAN unit I24).
//
// `GET /{owner}/{repo}/asc/search` answers one question in one of three modes, each one CodeIntel call:
//
//	mode=lexical  q=<text> [match=literal|word|regex] [case=1]     CodeIntel.Search, matching lines of files
//	mode=symbol   q=<name>                                          CodeIntel.Symbols (FIND), declared symbols
//	mode=semantic q=<question in words>                             CodeIntel.Semantic, ranked code chunks
//
// The scope selector narrows every mode the same way: ref (a branch or a 40-digit commit id, default branch
// when empty), path (a directory or file prefix) and lang (a language name). A coverage note under the form
// says how much of that scope the server has indexed, taken from CodeIntel.IndexStatus.
//
// Trust. As with the symbol panel, the route is reached through the repository's code-read check, which this
// handler repeats, and the call carries the internal token (the browser session holds no asc token), so asc
// does not decide who may read what: Forgejo does. That is only safe because of what the request can name:
//
//   - The repository is the one of the URL, resolved by Forgejo; the page is served only for a repository
//     that lives in asc.
//   - The revision is always "refs/heads/<branch>" of a branch that exists in this repository, or a 40-digit
//     lower-case hex commit id. The browser can never send a change id, a stack top or a workspace, so a draft
//     of another user is out of reach. The path and language only narrow the scope.
//   - Every text from asc is untrusted file content: it is cut, stripped of control and bidirectional
//     characters, and left to the template to escape. Paths, lines, scores and confidences are checked and an
//     answer that fails a check fails the page. A hit or chunk the server marked redacted loses its text here
//     even if the message carried some; nothing is rebuilt from raw lines. Only the status code of an asc error
//     is used, its text never reaches the page.
const (
	tplAscSearch base.TplName = "repo/asc_search"

	ascSearchTimeout = ascclient.DefaultCallTimeout

	ascSearchMaxQueryBytes    = 4096
	ascSearchMaxSymbolBytes   = 256
	ascSearchMaxSemanticBytes = 1000
	ascSearchMaxLangBytes     = 40
	ascSearchMaxFormBytes     = ascSearchMaxQueryBytes + 1 // one past the longest accepted field, so a longer one is seen as too long

	// ascSearchMaxFiles bounds the files asked for and read, and ascSearchMaxResponses the stream messages.
	ascSearchMaxFiles     = 50
	ascSearchMaxResponses = 64
	// ascSearchMaxMatches bounds the matching lines drawn per file, ascSearchMaxAlso the extra paths.
	ascSearchMaxMatches = 10
	ascSearchMaxAlso    = 5
	// ascSearchMaxContext bounds the context lines drawn on each side of a match.
	ascSearchMaxContext = 1
	ascSearchMaxLineLen = 4096
	// ascSearchMaxChunks is the k of the semantic call and bounds the chunks read.
	ascSearchMaxChunks     = 20
	ascSearchMaxChunkLines = 40
	ascSearchMaxChunkBytes = 8192
	ascSearchMaxSources    = 4
	ascSearchMaxIndexes    = 8
	ascSearchMaxLabel      = 64

	ascSearchModeLexical  = "lexical"
	ascSearchModeSymbol   = "symbol"
	ascSearchModeSemantic = "semantic"

	ascSearchMatchLiteral = "literal"
	ascSearchMatchWord    = "word"
	ascSearchMatchRegex   = "regex"
)

var errAscSearchBadAnswer = errors.New("asc search: bad answer")

// ascSearchSeg is a piece of a matching line; Hit pieces are drawn marked.
type ascSearchSeg struct {
	Text string
	Hit  bool
}

// ascSearchLine is one line of a file hit.
type ascSearchLine struct {
	Number   uint32
	IsMatch  bool
	Segments []ascSearchSeg
	Link     string
}

// ascSearchFile is one checked file hit.
type ascSearchFile struct {
	Path     string
	Link     string
	Lang     string
	FileID   string
	Redacted bool
	Also     []ascSearchAlso
	Lines    []ascSearchLine
	// Branches names the branches of an all-branches search that have this file.
	Branches []ascSearchBranch
}

// ascSearchAlso is another path of the same file.
type ascSearchAlso struct {
	Path string
	Link string
}

// ascSearchChunk is one checked semantic hit.
type ascSearchChunk struct {
	Path      string
	Link      string
	StartLine uint32
	EndLine   uint32
	Symbol    string
	Score     string
	// ScorePct is the score as a percentage of the best score of the answer (0 to 100), for the relevance bar.
	ScorePct int
	Sources  []string
	Text      string
	Redacted  bool
	Branches  []ascSearchBranch
}

// ascSearchCoverage is the state of one index.
type ascSearchCoverage struct {
	Index   string
	Percent int
	Lag     int64
}

// ascSearchQuery is the checked query string. The raw fields are what the form shows again.
type ascSearchQuery struct {
	mode          string
	text          string
	match         string
	caseSensitive bool
	rawRef        string
	rawPath       string
	rawLang       string
	scope         string // version, change, stack, branches or repos
	rawPull       string
	stackDiff     bool

	path  string
	lang  string
	rev   string // what asc gets: refs/heads/<branch> or a commit id
	label string // what the person sees: the branch or the short commit id
	base  string // link prefix for files: .../src/branch/<b> or .../src/commit/<sha>
}

// ascSearchResult is everything the page draws from the answer.
type ascSearchResult struct {
	files     []ascSearchFile
	symbols   []ascSymbolsRow
	chunks    []ascSearchChunk
	truncated bool
	model     string
	// resolved is the coverage of the resolved scope, or -1 when the answer carried none.
	resolved float32
}

// AscSearch serves `GET /{owner}/{repo}/asc/search`.
func AscSearch(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("AscSearch", nil)
		return
	}
	if !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("AscSearch", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("asc_rp.s_label")
	ctx.Data["AscSearchDefaultBranch"] = repo.DefaultBranch
	ctx.Data["PageIsAscSearch"] = true

	q := ascSearchRawQuery(ctx)
	ascSearchShowQuery(ctx, q)
	if q.text == "" {
		ctx.HTML(http.StatusOK, tplAscSearch) // the empty form
		return
	}
	if code, ok := ascSearchParse(ctx, &q); !ok {
		ascSearchFail(ctx, code)
		return
	}
	if q.scope == ascSearchScopeRepos {
		ctx.Redirect(ascSearchExploreURL(q))
		return
	}

	name, err := ascGraphRepoName(repo)
	if err != nil {
		ctx.NotFound("AscSearch", nil)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc search: no connection to asc serve: %v", err)
		ascSearchFail(ctx, http.StatusBadGateway)
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascSearchTimeout)
	defer cancel()
	api := ascv1.NewCodeIntelClient(conn)

	targets, code := ascSearchTargets(ctx, repo, name, &q)
	if code != 0 {
		ascSearchFail(ctx, code)
		return
	}
	res, failed, err := ascSearchRun(callCtx, api, q, targets)
	if err != nil {
		ascSearchMapError(ctx, err)
		return
	}

	ctx.Data["AscSearchRev"] = ascSearchTargetLabel(q, targets)
	ctx.Data["AscSearchDone"] = true
	ctx.Data["AscSearchFiles"] = res.files
	ctx.Data["AscSearchSymbols"] = res.symbols
	ctx.Data["AscSearchChunks"] = res.chunks
	ctx.Data["AscSearchTruncated"] = res.truncated
	ctx.Data["AscSearchModel"] = res.model
	ctx.Data["AscSearchCount"] = len(res.files) + len(res.symbols) + len(res.chunks)
	ctx.Data["AscSearchFailed"] = failed
	ctx.Data["AscSearchTargets"] = len(targets)
	ctx.Data["AscSearchLangFacets"], ctx.Data["AscSearchDirFacets"] = ascSearchFacets(ctx.Repo.RepoLink, q, res)

	// The coverage note is advice about the answer, so its failure never fails the page. It is read for the
	// first place searched; an all-branches search says so.
	cov, ok := ascSearchCoverageOf(callCtx, api, targets[0].scope, q.mode, res.resolved)
	ctx.Data["AscSearchCoverageKnown"] = ok
	ctx.Data["AscSearchCoverage"] = cov
	ctx.Data["AscSearchComplete"] = ok && ascSearchCovComplete(cov) && failed == 0
	ctx.Data["AscSearchCoverageOf"] = targets[0].label
	ctx.HTML(http.StatusOK, tplAscSearch)
}

// ascSearchShowQuery hands the form what it shows again. The values are escaped by the template.
func ascSearchShowQuery(ctx *context.Context, q ascSearchQuery) {
	ctx.Data["AscSearchMode"] = q.mode
	ctx.Data["AscSearchText"] = q.text
	ctx.Data["AscSearchMatch"] = q.match
	ctx.Data["AscSearchCase"] = q.caseSensitive
	ctx.Data["AscSearchRefInput"] = q.rawRef
	ctx.Data["AscSearchPathInput"] = q.rawPath
	ctx.Data["AscSearchLangInput"] = q.rawLang
	ctx.Data["AscSearchScope"] = q.scope
	ctx.Data["AscSearchPullInput"] = q.rawPull
	ctx.Data["AscSearchStackDiff"] = q.stackDiff
}

// ascSearchRawQuery reads the query string without trusting it: what it returns is only echoed into the form.
func ascSearchRawQuery(ctx *context.Context) ascSearchQuery {
	short := func(key string) string {
		s := ctx.FormString(key)
		if len(s) > ascSearchMaxFormBytes {
			s = s[:ascSearchMaxFormBytes]
		}
		return strings.ToValidUTF8(s, "")
	}
	q := ascSearchQuery{
		mode:          short("mode"),
		text:          strings.TrimSpace(short("q")),
		match:         short("match"),
		caseSensitive: ctx.FormBool("case"),
		rawRef:        short("ref"),
		rawPath:       short("path"),
		rawLang:       short("lang"),
		scope:         ascSearchScopeOK(short("scope")),
		rawPull:       short("pull"),
		stackDiff:     ctx.FormBool("diff"),
	}
	switch q.mode {
	case ascSearchModeLexical, ascSearchModeSymbol, ascSearchModeSemantic:
	default:
		q.mode = ascSearchModeLexical
	}
	switch q.match {
	case ascSearchMatchLiteral, ascSearchMatchWord, ascSearchMatchRegex:
	default:
		q.match = ascSearchMatchLiteral
	}
	return q
}

func ascSearchFail(ctx *context.Context, code int) {
	ctx.Data["AscSearchError"] = code
	ctx.HTML(code, tplAscSearch)
}

// ascSearchMapError turns a failed call or a bad answer into a page. Only the status code of an asc error is used.
func ascSearchMapError(ctx *context.Context, err error) {
	if errors.Is(err, errAscSearchBadAnswer) {
		log.Error("asc search: %v", err)
		ascSearchFail(ctx, http.StatusBadGateway)
		return
	}
	if ctx.Req.Context().Err() != nil {
		return // the browser went away
	}
	st, ok := status.FromError(err)
	if !ok {
		log.Error("asc search: call failed: %v", err)
		ascSearchFail(ctx, http.StatusBadGateway)
		return
	}
	switch st.Code() {
	case codes.NotFound:
		ascSearchFail(ctx, http.StatusNotFound)
	case codes.InvalidArgument:
		ascSearchFail(ctx, http.StatusBadRequest)
	case codes.ResourceExhausted:
		ascSearchFail(ctx, http.StatusTooManyRequests)
	case codes.DeadlineExceeded, codes.Canceled:
		ascSearchFail(ctx, http.StatusGatewayTimeout)
	case codes.Unavailable, codes.FailedPrecondition, codes.Unimplemented:
		ascSearchFail(ctx, http.StatusServiceUnavailable)
	default:
		log.Error("asc search: asc answered %s", st.Code())
		ascSearchFail(ctx, http.StatusBadGateway)
	}
}

// ascSearchParse checks the query and resolves the revision. When the query is refused it returns the HTTP
// status to answer with.
func ascSearchParse(ctx *context.Context, q *ascSearchQuery) (int, bool) {
	repo := ctx.Repo.Repository

	limit := ascSearchMaxQueryBytes
	switch q.mode {
	case ascSearchModeSymbol:
		limit = ascSearchMaxSymbolBytes
	case ascSearchModeSemantic:
		limit = ascSearchMaxSemanticBytes
	}
	if len(q.text) > limit || !ascSearchPlain(q.text) {
		return http.StatusBadRequest, false
	}

	path, ok := ascSymbolsPath(q.rawPath)
	if !ok {
		return http.StatusBadRequest, false
	}
	q.path = path

	if q.rawLang != "" {
		if !ascSearchLangOK(q.rawLang) {
			return http.StatusBadRequest, false
		}
		q.lang = q.rawLang
	}

	if q.scope != ascSearchScopeVersion {
		return 0, true // a change, a stack, the branches and the other repositories need no revision
	}

	repoLink := ctx.Repo.RepoLink
	ref := q.rawRef
	if ref == "" {
		ref = repo.DefaultBranch
	}
	if ascSymbolsSHA.MatchString(ref) {
		q.rev, q.label = ref, ref[:10]
		q.base = repoLink + "/src/commit/" + ref
		return 0, true
	}
	if !ascSymbolsBranchOK(ref) {
		return http.StatusBadRequest, false
	}
	if b, err := git_model.GetBranch(ctx, repo.ID, ref); err != nil || b.IsDeleted {
		return http.StatusNotFound, false
	}
	q.rev, q.label = "refs/heads/"+ref, ref
	q.base = repoLink + "/src/branch/" + util.PathEscapeSegments(ref)
	return 0, true
}

// ascSearchPlain reports whether s is valid UTF-8 without control or bidirectional characters.
func ascSearchPlain(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || isAscBidi(r) })
}

// ascSearchLangOK accepts the names of languages (letters, digits, space and . + # - _), so a language can not
// carry anything else into the scope.
func ascSearchLangOK(s string) bool {
	if s == "" || len(s) > ascSearchMaxLangBytes {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" .+#-_", r) {
			return false
		}
	}
	return true
}

// ascSearchClean makes an untrusted text printable on one line: valid UTF-8, control characters (tab among
// them) shown as a space, bidirectional characters dropped. The caller bounds the length.
func ascSearchClean(b []byte) string {
	return strings.Map(func(r rune) rune {
		switch {
		case isAscBidi(r):
			return -1
		case unicode.IsControl(r):
			return ' '
		}
		return r
	}, strings.ToValidUTF8(string(b), "�"))
}

// ascSearchBlock makes a multi-line untrusted text printable: lines are cleaned one by one, at most
// ascSearchMaxChunkLines of them.
func ascSearchBlock(b []byte) string {
	if len(b) > ascSearchMaxChunkBytes {
		b = b[:ascSearchMaxChunkBytes]
	}
	s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > ascSearchMaxChunkLines {
		lines = lines[:ascSearchMaxChunkLines]
	}
	for i, l := range lines {
		lines[i] = ascSearchClean([]byte(l))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n ")
}

// ascSearchLink is the link of a path in the revision, with an optional line anchor.
func ascSearchLink(q ascSearchQuery, path string, start, end uint32) string {
	link := q.base + "/" + util.PathEscapeSegments(path)
	switch {
	case start > 0 && end > start:
		link += "#L" + strconv.FormatUint(uint64(start), 10) + "-L" + strconv.FormatUint(uint64(end), 10)
	case start > 0:
		link += "#L" + strconv.FormatUint(uint64(start), 10)
	}
	return link
}

func ascSearchBad(msg string) error { return errors.Join(errAscSearchBadAnswer, errors.New(msg)) }

// ascSearchLexical runs CodeIntel.Search and reads the bounded stream.
func ascSearchLexical(ctx gocontext.Context, api ascv1.CodeIntelClient, scope *ascv1.Scope, q ascSearchQuery) (ascSearchResult, error) {
	mode := ascv1.SearchMode_SEARCH_MODE_LITERAL
	switch q.match {
	case ascSearchMatchWord:
		mode = ascv1.SearchMode_SEARCH_MODE_WORD
	case ascSearchMatchRegex:
		mode = ascv1.SearchMode_SEARCH_MODE_REGEX
	}
	ctx, cancel := gocontext.WithCancel(ctx)
	defer cancel()
	stream, err := api.Search(ctx, &ascv1.SearchRequest{
		Scope:         scope,
		Pattern:       q.text,
		Mode:          mode,
		CaseSensitive: q.caseSensitive,
		ContextLines:  ascSearchMaxContext,
		Budget:        &ascv1.Budget{MaxResults: ascSearchMaxFiles},
	})
	if err != nil {
		return ascSearchResult{}, err
	}
	res := ascSearchResult{resolved: -1}
	for range ascSearchMaxResponses {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return ascSearchResult{}, err
		}
		if resp.GetScope() != nil {
			if c, ok := ascSearchCoverageValue(resp.GetScope().GetCoverage()); ok {
				res.resolved = c
			}
		}
		if resp.GetTruncated() {
			res.truncated = true
		}
		for _, h := range resp.GetFiles() {
			if len(res.files) >= ascSearchMaxFiles {
				res.truncated = true
				return res, nil
			}
			f, err := ascSearchFileOf(h, q)
			if err != nil {
				return ascSearchResult{}, err
			}
			res.files = append(res.files, f)
		}
	}
	res.truncated = true // the stream went on past what is read
	return res, nil
}

// ascSearchFileOf checks a file hit and builds what is drawn. A hit that fails a check fails the page.
func ascSearchFileOf(h *ascv1.FileHit, q ascSearchQuery) (ascSearchFile, error) {
	path, ok := ascSymbolsPath(string(h.GetPath()))
	if !ok || path == "" {
		return ascSearchFile{}, ascSearchBad("file with a bad path")
	}
	f := ascSearchFile{
		Path:     path,
		Link:     ascSearchLink(q, path, 0, 0),
		Lang:     ascSymbolsText([]byte(h.GetLang()), ascSymbolsMaxShortTxt),
		FileID:   ascsearch.Hex(h.GetFileId()),
		Redacted: h.GetRedacted(),
	}
	for _, a := range h.GetAlsoAt() {
		if len(f.Also) >= ascSearchMaxAlso {
			break
		}
		if ap, ok := ascSymbolsPath(string(a)); ok && ap != "" {
			f.Also = append(f.Also, ascSearchAlso{Path: ap, Link: ascSearchLink(q, ap, 0, 0)})
		}
	}
	if f.Redacted {
		return f, nil // the path may be shown, the text never
	}

	last := uint32(0)
	for i, m := range h.GetMatches() {
		if i >= ascSearchMaxMatches {
			break
		}
		n := m.GetLine()
		if n < 1 || n > ascSymbolsMaxLine {
			return ascSearchFile{}, ascSearchBad("match with a bad line")
		}
		before, after := m.GetBefore(), m.GetAfter()
		if len(before) > ascSearchMaxContext {
			before = before[len(before)-ascSearchMaxContext:]
		}
		after = after[:min(len(after), ascSearchMaxContext)]
		add := func(num uint32, line ascSearchLine) {
			if num <= last {
				return // already drawn by the previous match
			}
			line.Number, line.Link = num, ascSearchLink(q, path, num, 0)
			f.Lines = append(f.Lines, line)
			last = num
		}
		for j, t := range before {
			if num := int64(n) - int64(len(before)) + int64(j); num >= 1 {
				add(uint32(num), ascSearchLine{Segments: []ascSearchSeg{{Text: ascSearchClean(ascSearchCut(t))}}})
			}
		}
		add(n, ascSearchLine{IsMatch: true, Segments: ascSearchSegments(ascSearchCut(m.GetText()), m.GetCols(), q)})
		for j, t := range after {
			add(n+1+uint32(j), ascSearchLine{Segments: []ascSearchSeg{{Text: ascSearchClean(ascSearchCut(t))}}})
		}
	}
	return f, nil
}

// ascSearchCut bounds one line of text, at a rune boundary.
func ascSearchCut(b []byte) []byte {
	if len(b) <= ascSearchMaxLineLen {
		return b
	}
	b = b[:ascSearchMaxLineLen]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

// ascSearchSegments splits a matching line at the columns the server gave (byte offsets of the starts of
// matches). The width of a match is not on the wire: it is the length of the search text for a literal or a
// word search when the text there is that text, and one character otherwise. Each piece is cleaned on its
// own, so cleaning never moves a column. A column that is not the start of a character is ignored.
func ascSearchSegments(raw []byte, cols []uint32, q ascSearchQuery) []ascSearchSeg {
	width := func(at int) int {
		if q.match != ascSearchMatchRegex && q.mode == ascSearchModeLexical {
			if n := len(q.text); at+n <= len(raw) && strings.EqualFold(string(raw[at:at+n]), q.text) {
				return n
			}
		}
		_, w := utf8.DecodeRune(raw[at:])
		return w
	}
	var segs []ascSearchSeg
	pos := 0
	for _, c := range cols {
		at := int(c)
		if at < pos || at >= len(raw) || !utf8.RuneStart(raw[at]) {
			continue
		}
		end := min(at+width(at), len(raw))
		if !utf8.RuneStart(byteAt(raw, end)) {
			continue
		}
		if at > pos {
			segs = append(segs, ascSearchSeg{Text: ascSearchClean(raw[pos:at])})
		}
		segs = append(segs, ascSearchSeg{Text: ascSearchClean(raw[at:end]), Hit: true})
		pos = end
	}
	if pos < len(raw) || len(segs) == 0 {
		segs = append(segs, ascSearchSeg{Text: ascSearchClean(raw[pos:])})
	}
	return segs
}

// byteAt is raw[i], or the start of a character when i is past the end.
func byteAt(raw []byte, i int) byte {
	if i >= len(raw) {
		return 0
	}
	return raw[i]
}

// ascSearchSymbols runs CodeIntel.Symbols (FIND) and reuses the checks of the symbol panel.
func ascSearchSymbols(ctx gocontext.Context, api ascv1.CodeIntelClient, scope *ascv1.Scope, q ascSearchQuery) (ascSearchResult, error) {
	resp, err := api.Symbols(ctx, &ascv1.SymbolsRequest{
		Scope:  scope,
		Op:     ascv1.SymbolOp_SYMBOL_OP_FIND,
		Name:   q.text,
		Budget: &ascv1.Budget{MaxResults: ascSymbolsMaxSymbols},
	})
	if err != nil {
		return ascSearchResult{}, err
	}
	rows, truncated, err := ascSymbolsBuild(resp, ascSymbolsQuery{base: q.base})
	if err != nil {
		return ascSearchResult{}, err
	}
	res := ascSearchResult{symbols: rows, truncated: truncated, resolved: -1}
	if resp.GetScope() != nil {
		if c, ok := ascSearchCoverageValue(resp.GetScope().GetCoverage()); ok {
			res.resolved = c
		}
	}
	return res, nil
}

// ascSearchSemantic runs CodeIntel.Semantic.
func ascSearchSemantic(ctx gocontext.Context, api ascv1.CodeIntelClient, scope *ascv1.Scope, q ascSearchQuery) (ascSearchResult, error) {
	resp, err := api.Semantic(ctx, &ascv1.SemanticRequest{
		Scope: scope,
		Query: q.text,
		Mode:  ascv1.SemanticMode_SEMANTIC_MODE_HYBRID,
		K:     ascSearchMaxChunks,
	})
	if err != nil {
		return ascSearchResult{}, err
	}
	chunks := resp.GetChunks()
	if len(chunks) > ascSearchMaxChunks {
		return ascSearchResult{}, ascSearchBad("too many chunks")
	}
	res := ascSearchResult{resolved: -1, model: ascSymbolsText([]byte(resp.GetModel()), ascSearchMaxLabel)}
	scores := make([]float64, 0, len(chunks))
	for _, c := range chunks {
		path, ok := ascSymbolsPath(string(c.GetPath()))
		if !ok || path == "" {
			return ascSearchResult{}, ascSearchBad("chunk with a bad path")
		}
		if c.GetStartLine() > ascSymbolsMaxLine || c.GetEndLine() > ascSymbolsMaxLine || c.GetEndLine() < c.GetStartLine() {
			return ascSearchResult{}, ascSearchBad("chunk with bad lines")
		}
		if math.IsNaN(c.GetScore()) || math.IsInf(c.GetScore(), 0) {
			return ascSearchResult{}, ascSearchBad("chunk with a bad score")
		}
		ch := ascSearchChunk{
			Path:      path,
			Link:      ascSearchLink(q, path, c.GetStartLine(), c.GetEndLine()),
			StartLine: c.GetStartLine(),
			EndLine:   c.GetEndLine(),
			Symbol:    ascSymbolsText([]byte(c.GetSymbol()), ascSymbolsMaxNameBytes),
			Score:     strconv.FormatFloat(c.GetScore(), 'f', 3, 64),
			Redacted:  c.GetRedacted(),
		}
		for i, s := range c.GetSources() {
			if i >= ascSearchMaxSources {
				break
			}
			ch.Sources = append(ch.Sources, ascSymbolsText([]byte(s), ascSymbolsMaxShortTxt))
		}
		if !ch.Redacted {
			ch.Text = ascSearchBlock(c.GetText())
		}
		res.chunks = append(res.chunks, ch)
		scores = append(scores, c.GetScore())
	}
	for i, pct := range ascSearchRelative(scores) {
		res.chunks[i].ScorePct = pct
	}
	if resp.GetScope() != nil {
		if c, ok := ascSearchCoverageValue(resp.GetScope().GetCoverage()); ok {
			res.resolved = c
		}
	}
	return res, nil
}

// ascSearchCoverageValue accepts a coverage between 0 and 1.
func ascSearchCoverageValue(c float32) (float32, bool) {
	if math.IsNaN(float64(c)) || c < 0 || c > 1 {
		return 0, false
	}
	return c, true
}

// ascSearchCoverageOf reads the coverage of the indexes the mode uses (CodeIntel.IndexStatus). Lexical and
// symbol searches read the "files" index and the semantic one the "vectors" index; a submodule mounted in the
// scope has its own entry, labelled "files@<path>". When the call fails or no index of the mode is listed, the
// coverage of the resolved scope of the answer stands in for it; with neither the note says coverage is unknown.
func ascSearchCoverageOf(ctx gocontext.Context, api ascv1.CodeIntelClient, scope *ascv1.Scope, mode string, resolved float32) ([]ascSearchCoverage, bool) {
	index := "files"
	if mode == ascSearchModeSemantic {
		index = "vectors"
	}
	var out []ascSearchCoverage
	resp, err := api.IndexStatus(ctx, &ascv1.IndexStatusRequest{Scope: scope})
	if err != nil {
		log.Debug("asc search: index status: %v", err)
	} else {
		for _, s := range resp.GetIndexes() {
			if len(out) >= ascSearchMaxIndexes {
				break
			}
			label := s.GetIndex()
			if label != index && !strings.HasPrefix(label, index+"@") {
				continue
			}
			c, ok := ascSearchCoverageValue(s.GetCoverage())
			if !ok {
				return nil, false
			}
			out = append(out, ascSearchCoverage{
				Index:   ascSymbolsText([]byte(label), ascSearchMaxLabel),
				Percent: int(math.Floor(float64(c) * 100)),
				Lag:     max(s.GetLagSeconds(), 0),
			})
		}
	}
	if len(out) == 0 && resolved >= 0 {
		out = append(out, ascSearchCoverage{Index: index, Percent: int(math.Floor(float64(resolved) * 100))})
	}
	return out, len(out) > 0
}

// ascSearchCovComplete reports whether every listed index covers the whole scope.
func ascSearchCovComplete(cov []ascSearchCoverage) bool {
	for _, c := range cov {
		if c.Percent < 100 {
			return false
		}
	}
	return len(cov) > 0
}

// ascSearchRelative turns scores into whole percentages of the best one. The scale of a score depends on the
// ranking that made it, so the bar says how a hit compares with the best hit of the same answer. A best score that
// is not positive gives 0 for every hit (nothing to compare with).
func ascSearchRelative(scores []float64) []int {
	out := make([]int, len(scores))
	best := 0.0
	for _, s := range scores {
		best = math.Max(best, s)
	}
	if best <= 0 {
		return out
	}
	for i, s := range scores {
		out[i] = int(math.Round(math.Max(0, s) / best * 100))
	}
	return out
}
