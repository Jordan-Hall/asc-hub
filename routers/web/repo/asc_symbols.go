// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The file-view symbol panel, the outline, and "Find usages in the base" on the pull request files tab
// (SPEC 23.8, PLAN unit I25).
//
// `GET /{owner}/{repo}/asc/symbols` answers three questions through CodeIntel.Symbols:
//
//	op=def     name=<identifier> [ref=<branch|sha>]               definition and references, with confidence
//	op=outline path=<file> [ref=<branch|sha>]                     the symbols declared in a file
//	op=usages  pull=<index> name=<identifier>[ <identifier>...]   references in the base of a pull request
//
// Trust. The route is reached through the repository's code-read check, which this handler repeats, and the
// call carries the internal token (as the commit graph and the RPC proxy do), so asc does not decide who may
// read what: Forgejo does. That is only safe because of what the request can name:
//
//   - The scope is always a plain revision that Forgejo resolved itself: "refs/heads/<branch>" of a branch that
//     exists in this repository, or a 40-digit hex commit id. The browser can never send a change id, a stack
//     top or a workspace, so a draft of another user is out of reach.
//   - "usages" takes the base from the pull request's own row (BaseRepoID and BaseBranch), never from the
//     request, and the pull request must belong to this repository.
//   - Every text from asc is untrusted file content: it is cut, stripped of control and bidirectional
//     characters, and left to the template to escape. Paths, lines and confidences are checked and an answer
//     that fails a check fails the page. The server redacts secrets (SPEC 23.7); nothing is rebuilt from raw
//     lines here. Only the status code of an asc error is used.
const (
	tplAscSymbols base.TplName = "repo/asc_symbols"

	ascSymbolsTimeout = ascclient.DefaultCallTimeout

	ascSymbolsMaxNameBytes = 256
	ascSymbolsMaxPathBytes = 4096
	ascSymbolsMaxRefBytes  = 255
	ascSymbolsMaxNames     = 10
	ascSymbolsMaxFormBytes = 4096
	// ascSymbolsMaxSymbols bounds the symbols read from one answer, and ascSymbolsMaxShown the rows drawn.
	ascSymbolsMaxSymbols  = 1000
	ascSymbolsMaxShown    = 200
	ascSymbolsMaxLine     = 1 << 24
	ascSymbolsMaxText     = 300
	ascSymbolsMaxDocRunes = 400
	ascSymbolsMaxShortTxt = 64
)

var (
	ascSymbolsSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

	// ascSymbolsConfidence lists the confidences the server defines (SPEC 23.4.1).
	ascSymbolsConfidence = map[string]bool{"precise": true, "import_match": true, "name_match": true}

	errAscSymbolsBadAnswer = errors.New("asc symbols: bad answer")
)

// ascSymbolsRow is one checked symbol, ready to show.
type ascSymbolsRow struct {
	Name       string
	Kind       string
	IsDef      bool
	Path       string
	Line       uint32
	EndLine    uint32
	Signature  string
	Container  string
	Doc        string
	Confidence string
	Link       string
	// Branches names the branches of an all-branches search that have this symbol.
	Branches []ascSearchBranch
}

// ascSymbolsSection is one list of the page.
type ascSymbolsSection struct {
	Title     string // outline, def, refs or usages
	Name      string
	Rows      []ascSymbolsRow
	Truncated bool
}

// ascSymbolsQuery is the checked query string.
type ascSymbolsQuery struct {
	op    string
	names []string
	path  string
	rev   string // what asc gets: refs/heads/<branch> or a commit id
	label string // what the person sees: the branch or the short commit id
	pull  int64
	base  string // link prefix for files: .../src/branch/<b> or .../src/commit/<sha>
}

// AscSymbols serves `GET /{owner}/{repo}/asc/symbols`.
func AscSymbols(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("AscSymbols", nil)
		return
	}
	if !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("AscSymbols", nil)
		return
	}
	ctx.Data["Title"] = "Symbols"
	ctx.Data["PageIsViewCode"] = true

	q, code, ok := ascSymbolsParse(ctx)
	if !ok {
		ascSymbolsFail(ctx, code)
		return
	}
	ctx.Data["AscSymbolsOp"] = q.op
	ctx.Data["AscSymbolsName"] = strings.Join(q.names, " ")
	ctx.Data["AscSymbolsPath"] = q.path
	ctx.Data["AscSymbolsRev"] = q.label
	ctx.Data["AscSymbolsPull"] = q.pull

	name, err := ascGraphRepoName(repo)
	if err != nil {
		ctx.NotFound("AscSymbols", nil)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		log.Error("asc symbols: no connection to asc serve: %v", err)
		ascSymbolsFail(ctx, http.StatusBadGateway)
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascSymbolsTimeout)
	defer cancel()
	api := ascv1.NewCodeIntelClient(conn)

	var sections []ascSymbolsSection
	add := func(title string, req *ascv1.SymbolsRequest) error {
		req.Scope = &ascv1.Scope{
			Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
			At:   &ascv1.Scope_Rev{Rev: q.rev},
		}
		req.Budget = &ascv1.Budget{MaxResults: ascSymbolsMaxSymbols}
		resp, err := api.Symbols(callCtx, req)
		if err != nil {
			return err
		}
		rows, truncated, err := ascSymbolsBuild(resp, q)
		if err != nil {
			return err
		}
		sections = append(sections, ascSymbolsSection{Title: title, Name: req.GetName(), Rows: rows, Truncated: truncated})
		return nil
	}

	switch q.op {
	case "outline":
		err = add("outline", &ascv1.SymbolsRequest{Op: ascv1.SymbolOp_SYMBOL_OP_OUTLINE, Path: []byte(q.path)})
	case "def":
		if err = add("def", &ascv1.SymbolsRequest{Op: ascv1.SymbolOp_SYMBOL_OP_DEF, Name: q.names[0]}); err == nil {
			err = add("refs", &ascv1.SymbolsRequest{Op: ascv1.SymbolOp_SYMBOL_OP_REFS, Name: q.names[0]})
		}
	case "usages":
		for _, n := range q.names {
			if err = add("usages", &ascv1.SymbolsRequest{Op: ascv1.SymbolOp_SYMBOL_OP_REFS, Name: n}); err != nil {
				break
			}
		}
	}
	if err != nil {
		ascSymbolsMapError(ctx, err)
		return
	}
	ctx.Data["AscSymbolsSections"] = sections
	ctx.HTML(http.StatusOK, tplAscSymbols)
}

func ascSymbolsFail(ctx *context.Context, code int) {
	ctx.Data["AscSymbolsError"] = code
	ctx.HTML(code, tplAscSymbols)
}

// ascSymbolsMapError turns a failed call or a bad answer into a page. Only the status code of an asc error is used.
func ascSymbolsMapError(ctx *context.Context, err error) {
	if errors.Is(err, errAscSymbolsBadAnswer) {
		log.Error("asc symbols: %v", err)
		ascSymbolsFail(ctx, http.StatusBadGateway)
		return
	}
	if ctx.Req.Context().Err() != nil {
		return // the browser went away
	}
	st, ok := status.FromError(err)
	if !ok {
		log.Error("asc symbols: call failed: %v", err)
		ascSymbolsFail(ctx, http.StatusBadGateway)
		return
	}
	switch st.Code() {
	case codes.NotFound:
		ascSymbolsFail(ctx, http.StatusNotFound)
	case codes.InvalidArgument:
		ascSymbolsFail(ctx, http.StatusBadRequest)
	case codes.ResourceExhausted:
		ascSymbolsFail(ctx, http.StatusTooManyRequests)
	case codes.DeadlineExceeded, codes.Canceled:
		ascSymbolsFail(ctx, http.StatusGatewayTimeout)
	default:
		log.Error("asc symbols: asc answered %s", st.Code())
		ascSymbolsFail(ctx, http.StatusBadGateway)
	}
}

// ascSymbolsParse checks the query string and resolves the revision. When the query is refused it returns the
// HTTP status to answer with.
func ascSymbolsParse(ctx *context.Context) (ascSymbolsQuery, int, bool) {
	repo := ctx.Repo.Repository
	q := ascSymbolsQuery{op: ctx.FormString("op")}
	if len(ctx.FormString("name")) > ascSymbolsMaxFormBytes {
		return q, http.StatusBadRequest, false
	}

	switch q.op {
	case "def", "usages":
		names, ok := ascSymbolsNames(ctx.FormString("name"))
		if !ok || len(names) == 0 || (q.op == "def" && len(names) != 1) {
			return q, http.StatusBadRequest, false
		}
		q.names = names
	case "outline":
		clean, ok := ascSymbolsPath(ctx.FormString("path"))
		if !ok || clean == "" {
			return q, http.StatusBadRequest, false
		}
		q.path = clean
	default:
		return q, http.StatusBadRequest, false
	}

	repoLink := ctx.Repo.RepoLink
	if q.op == "usages" {
		// The base comes from the pull request, never from the request.
		if !ctx.Repo.CanRead(unit.TypePullRequests) {
			return q, http.StatusNotFound, false
		}
		idx := ctx.FormInt64("pull")
		if idx <= 0 {
			return q, http.StatusBadRequest, false
		}
		pr, err := issues_model.GetPullRequestByIndex(ctx, repo.ID, idx)
		if err != nil || pr.BaseRepoID != repo.ID || !ascSymbolsBranchOK(pr.BaseBranch) {
			return q, http.StatusNotFound, false
		}
		q.pull = idx
		q.rev = "refs/heads/" + pr.BaseBranch
		q.label = pr.BaseBranch
		q.base = repoLink + "/src/branch/" + util.PathEscapeSegments(pr.BaseBranch)
		return q, 0, true
	}

	ref := ctx.FormString("ref")
	if ref == "" {
		ref = repo.DefaultBranch
	}
	if ascSymbolsSHA.MatchString(ref) {
		q.rev, q.label = ref, ref[:10]
		q.base = repoLink + "/src/commit/" + ref
		return q, 0, true
	}
	if !ascSymbolsBranchOK(ref) {
		return q, http.StatusBadRequest, false
	}
	if b, err := git_model.GetBranch(ctx, repo.ID, ref); err != nil || b.IsDeleted {
		return q, http.StatusNotFound, false
	}
	q.rev, q.label = "refs/heads/"+ref, ref
	q.base = repoLink + "/src/branch/" + util.PathEscapeSegments(ref)
	return q, 0, true
}

// ascSymbolsBranchOK refuses an empty name, one that could be read as an option, and control characters.
func ascSymbolsBranchOK(s string) bool {
	return s != "" && len(s) <= ascSymbolsMaxRefBytes && utf8.ValidString(s) && !strings.HasPrefix(s, "-") &&
		!strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || isAscBidi(r) })
}

// ascSymbolsNames splits and checks the identifiers.
func ascSymbolsNames(field string) ([]string, bool) {
	out := strings.Fields(field)
	if len(out) > ascSymbolsMaxNames {
		return nil, false
	}
	for _, n := range out {
		if len(n) > ascSymbolsMaxNameBytes || !utf8.ValidString(n) || strings.ContainsFunc(n, func(r rune) bool {
			return unicode.IsControl(r) || isAscBidi(r)
		}) {
			return nil, false
		}
	}
	return out, true
}

// ascSymbolsPath checks a repository path: relative, no dot segments, no control characters, no backslash.
// The empty path is accepted and stays empty.
func ascSymbolsPath(p string) (string, bool) {
	p = strings.TrimPrefix(p, "/")
	if len(p) > ascSymbolsMaxPathBytes || !utf8.ValidString(p) || strings.ContainsFunc(p, func(r rune) bool {
		return unicode.IsControl(r) || r == '\\' || isAscBidi(r)
	}) {
		return "", false
	}
	if p == "" {
		return "", true
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return p, true
}

// isAscBidi reports the characters that reorder or split displayed text ("Trojan Source").
func isAscBidi(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F, r == 0x2028, r == 0x2029:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// ascSymbolsText makes an untrusted text printable, one line, and short.
func ascSymbolsText(b []byte, limit int) string {
	if len(b) > limit*4 {
		b = b[:limit*4]
	}
	s := strings.ToValidUTF8(string(b), "�")
	var sb strings.Builder
	n := 0
	for _, r := range s {
		if n >= limit {
			sb.WriteString("...")
			break
		}
		if unicode.IsControl(r) || isAscBidi(r) {
			r = ' '
		}
		sb.WriteRune(r)
		n++
	}
	return strings.TrimSpace(sb.String())
}

// ascSymbolsBuild checks an answer and builds the rows. A symbol that fails a check fails the page.
func ascSymbolsBuild(resp *ascv1.SymbolsResponse, q ascSymbolsQuery) ([]ascSymbolsRow, bool, error) {
	bad := func(msg string) error { return errors.Join(errAscSymbolsBadAnswer, errors.New(msg)) }
	syms := resp.GetSymbols()
	if len(syms) > ascSymbolsMaxSymbols {
		return nil, false, bad("too many symbols")
	}
	truncated := resp.GetTruncated()
	rows := make([]ascSymbolsRow, 0, min(len(syms), ascSymbolsMaxShown))
	for _, s := range syms {
		path, ok := ascSymbolsPath(string(s.GetPath()))
		if !ok || path == "" {
			return nil, false, bad("symbol with a bad path")
		}
		if s.GetLine() > ascSymbolsMaxLine || s.GetEndLine() > ascSymbolsMaxLine || (s.GetEndLine() != 0 && s.GetEndLine() < s.GetLine()) {
			return nil, false, bad("symbol with bad lines")
		}
		if !ascSymbolsConfidence[s.GetConfidence()] {
			return nil, false, bad("symbol with an unknown confidence")
		}
		if len(rows) >= ascSymbolsMaxShown {
			truncated = true
			break
		}
		link := q.base + "/" + util.PathEscapeSegments(path)
		if s.GetLine() > 0 {
			link += "#L" + strconv.FormatUint(uint64(s.GetLine()), 10)
		}
		rows = append(rows, ascSymbolsRow{
			Name:       ascSymbolsText([]byte(s.GetName()), ascSymbolsMaxNameBytes),
			Kind:       ascSymbolsText([]byte(s.GetKind()), ascSymbolsMaxShortTxt),
			IsDef:      s.GetIsDef(),
			Path:       path,
			Line:       s.GetLine(),
			EndLine:    s.GetEndLine(),
			Signature:  ascSymbolsText(s.GetSignature(), ascSymbolsMaxText),
			Container:  ascSymbolsText([]byte(s.GetContainer()), ascSymbolsMaxShortTxt),
			Doc:        ascSymbolsText(s.GetDoc(), ascSymbolsMaxDocRunes),
			Confidence: s.GetConfidence(),
			Link:       link,
		})
	}
	return rows, truncated, nil
}
