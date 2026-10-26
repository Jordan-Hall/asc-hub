// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The code intelligence API (SPEC 23.8, PLAN unit I26):
//
//	GET /api/v1/repos/{owner}/{repo}/asc/search
//	GET /api/v1/repos/{owner}/{repo}/asc/symbols
//	GET /api/v1/repos/{owner}/{repo}/asc/semantic
//
// Each route proxies one CodeIntel call with the token of the caller, never the internal token, so asc
// decides what the caller may read. The answer is the server-rendered (already redacted) message as JSON.
// Forgejo never rebuilds a line from anything else: a hit or chunk the server marked redacted loses
// every text field here even if the message carried one, and all text is made valid UTF-8 without
// control or bidirectional characters. The status text of an error never reaches the client.
//
// The routes are registered in api.go under reqRepoReader(unit.TypeCode). The client is the process's
// connection to asc serve (ascsearch.Default) unless SetAscCodeIntel sets another one.

// Bounds on one request and on what is read back.
const (
	ascCIMaxQueryBytes = 4096
	ascCIMaxNameBytes  = 512
	ascCIMaxRevBytes   = 255
	ascCIMaxPathBytes  = 1024
	ascCIMaxPaths      = 16
	ascCIMaxLangs      = 8
	ascCIMaxKinds      = 8
	ascCIMaxTokenBytes = 8192
	ascCIMaxLimit      = 200
	ascCIDefaultLimit  = 50
	ascCIMaxContext    = 5
	ascCIMaxMatches    = 50
	ascCIMaxLineBytes  = 4096
	ascCIMaxResponses  = 64
	ascCIMaxSemanticK  = 50
	ascCITimeout       = 30 * time.Second
)

type ascCIHolder struct{ api ascv1.CodeIntelClient }

var ascCIClient atomic.Pointer[ascCIHolder]

// SetAscCodeIntel sets the CodeIntel client the routes use; nil switches the routes off (501).
func SetAscCodeIntel(api ascv1.CodeIntelClient) {
	if api == nil {
		ascCIClient.Store(nil)
		return
	}
	ascCIClient.Store(&ascCIHolder{api: api})
}

// ascCIToken returns the caller's token from the Authorization header ("Bearer", "token" or the password
// of "Basic"). It is not validated here: ascclient.WithUserToken refuses anything but visible ASCII.
func ascCIToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, rest, ok := strings.Cut(h, " ")
	if !ok {
		return ""
	}
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(scheme) {
	case "bearer", "token":
		return rest
	case "basic":
		raw, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return ""
		}
		_, pw, _ := strings.Cut(string(raw), ":")
		return pw
	}
	return ""
}

// ascCIClean makes server text safe to show: valid UTF-8, no control characters except tab, no bidi marks.
func ascCIClean(b []byte) string {
	if len(b) > ascCIMaxLineBytes {
		b = b[:ascCIMaxLineBytes]
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return r
		case unicode.IsControl(r), r == 0x061C, r == 0x200E, r == 0x200F, r == 0x2028, r == 0x2029,
			r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, strings.ToValidUTF8(string(b), "�"))
}

func ascCICleanAll(bs [][]byte, limit int) []string {
	if len(bs) > limit {
		bs = bs[:limit]
	}
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, ascCIClean(b))
	}
	return out
}

func ascCIHex(b []byte) string {
	if len(b) > 64 {
		return ""
	}
	return hex.EncodeToString(b)
}

// ascCIPlain reports whether s is at most max bytes with no control or replacement character.
func ascCIPlain(s string, limit int) bool {
	if len(s) > limit {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}

type ascCIScopeJSON struct {
	Changeset  string  `json:"changeset,omitempty"`
	Root       string  `json:"root,omitempty"`
	Generation uint64  `json:"generation"`
	Coverage   float32 `json:"coverage"`
}

func ascCIScope(s *ascv1.ResolvedScope) *ascCIScopeJSON {
	if s == nil {
		return nil
	}
	return &ascCIScopeJSON{Changeset: ascCIHex(s.GetChangeset()), Root: ascCIHex(s.GetRoot()), Generation: s.GetGeneration(), Coverage: s.GetCoverage()}
}

type ascCIQuery struct {
	api ascv1.CodeIntelClient
	// svc has the CodeIntel client above and, when asc serve is connected, the CrossSearch client.
	svc    *ascsearch.Service
	ctx    gocontext.Context
	cancel gocontext.CancelFunc
	scope  *ascv1.Scope
	limit  int
}

// ascCIService is the client of this request: the one SetAscCodeIntel set, else the connection to asc serve.
func ascCIService() (*ascsearch.Service, error) {
	def, err := ascsearch.Default()
	if h := ascCIClient.Load(); h != nil {
		svc := &ascsearch.Service{Intel: h.api}
		if err == nil {
			svc.Cross = def.Cross
		}
		return svc, nil
	}
	return def, err
}

// ascCIFailStart answers a missing service: 501 while the integration is off, 502 when it cannot connect.
func ascCIFailStart(ctx *context.APIContext, err error) {
	if errors.Is(err, ascsearch.ErrDisabled) {
		ctx.Error(http.StatusNotImplemented, "", "code intelligence is not enabled")
		return
	}
	log.Error("asc code intelligence: no connection to asc serve: %v", err)
	ctx.Error(http.StatusBadGateway, "", "code intelligence is unavailable")
}

// ascCIStart runs the checks every route shares and builds the scope. It writes the error and returns nil
// when a check fails. The scope is a revision (rev, the default branch when empty) or, when atChange is
// true, a change (change) or the top of a stack (stack, with stack_diff_only to search only what the stack
// changes); only one of the three may be given.
func ascCIStart(ctx *context.APIContext, atChange bool) *ascCIQuery {
	svc, err := ascCIService()
	if err != nil {
		ascCIFailStart(ctx, err)
		return nil
	}
	repo := ctx.Repository()
	if repo == nil || ctx.Repo() == nil || !ctx.Repo().CanRead(unit.TypeCode) {
		ctx.NotFound()
		return nil
	}
	callCtx, err := ascclient.WithUserToken(ctx.Req.Context(), ascCIToken(ctx.Req))
	if err != nil {
		ctx.Error(http.StatusUnauthorized, "", "a token is required for code intelligence")
		return nil
	}
	name, err := ascsearch.NameOf(repo)
	if err != nil {
		ctx.NotFound()
		return nil
	}
	var scope *ascv1.Scope
	change, stack := ctx.FormString("change"), ctx.FormString("stack")
	switch {
	case change != "" || stack != "":
		if !atChange || (change != "" && (stack != "" || ctx.FormString("rev") != "")) || (stack != "" && ctx.FormString("rev") != "") {
			ctx.Error(http.StatusUnprocessableEntity, "", "give one of rev, change and stack")
			return nil
		}
		id := change
		if stack != "" {
			id = stack
		}
		scope, err = ascsearch.AtChange(name, id, stack != "", ctx.FormBool("stack_diff_only"))
		if err != nil {
			ctx.Error(http.StatusUnprocessableEntity, "", "bad change id")
			return nil
		}
	default:
		rev := ctx.FormString("rev")
		if rev == "" {
			rev = repo.DefaultBranch
		}
		if rev == "" || strings.HasPrefix(rev, "-") || !ascCIPlain(rev, ascCIMaxRevBytes) {
			ctx.Error(http.StatusUnprocessableEntity, "", "bad rev")
			return nil
		}
		scope = &ascv1.Scope{
			Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT},
			At:   &ascv1.Scope_Rev{Rev: rev},
		}
	}
	for _, f := range []struct {
		key  string
		max  int
		size int
		dst  *[]string
	}{
		{"path", ascCIMaxPaths, ascCIMaxPathBytes, &scope.Paths},
		{"exclude", ascCIMaxPaths, ascCIMaxPathBytes, &scope.ExcludePaths},
		{"lang", ascCIMaxLangs, 64, &scope.Langs},
	} {
		vals := ctx.FormStrings(f.key)
		if len(vals) > f.max {
			ctx.Error(http.StatusUnprocessableEntity, "", "too many "+f.key+" values")
			return nil
		}
		for _, v := range vals {
			if v == "" || !ascCIPlain(v, f.size) {
				ctx.Error(http.StatusUnprocessableEntity, "", "bad "+f.key)
				return nil
			}
			*f.dst = append(*f.dst, v)
		}
	}
	limit := ctx.FormInt("limit")
	switch {
	case limit <= 0:
		limit = ascCIDefaultLimit
	case limit > ascCIMaxLimit:
		limit = ascCIMaxLimit
	}
	c, cancel := gocontext.WithTimeout(callCtx, ascCITimeout)
	return &ascCIQuery{api: svc.Intel, svc: svc, ctx: c, cancel: cancel, scope: scope, limit: limit}
}

func ascCIPageToken(ctx *context.APIContext) (string, bool) {
	t := ctx.FormString("page_token")
	if len(t) > ascCIMaxTokenBytes {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad page_token")
		return "", false
	}
	for i := range len(t) {
		if t[i] < 0x21 || t[i] > 0x7e {
			ctx.Error(http.StatusUnprocessableEntity, "", "bad page_token")
			return "", false
		}
	}
	return t, true
}

// ascCIOutToken drops a next-page token that is not visible ASCII.
func ascCIOutToken(t string) string {
	if len(t) > ascCIMaxTokenBytes {
		return ""
	}
	for i := range len(t) {
		if t[i] < 0x21 || t[i] > 0x7e {
			return ""
		}
	}
	return t
}

// ascCIFail maps a call error to a status. The text of the server's status never leaves Forgejo.
func ascCIFail(ctx *context.APIContext, err error) {
	if errors.Is(err, ascclient.ErrNoCredential) {
		ctx.Error(http.StatusUnauthorized, "", "a token is required for code intelligence")
		return
	}
	switch status.Code(err) {
	case codes.PermissionDenied, codes.NotFound, codes.Unauthenticated:
		ctx.NotFound() // the same answer for "cannot read" and "does not exist"
	case codes.InvalidArgument, codes.OutOfRange, codes.FailedPrecondition:
		ctx.Error(http.StatusUnprocessableEntity, "", "the request was refused")
	case codes.ResourceExhausted:
		ctx.Error(http.StatusTooManyRequests, "", "too many requests")
	case codes.DeadlineExceeded:
		ctx.Error(http.StatusGatewayTimeout, "", "code intelligence timed out")
	case codes.Unavailable:
		ctx.Error(http.StatusServiceUnavailable, "", "code intelligence is unavailable")
	default:
		ctx.Error(http.StatusBadGateway, "", "code intelligence failed")
	}
}

type ascCILineMatch struct {
	Line   uint32   `json:"line"`
	Cols   []uint32 `json:"cols,omitempty"`
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

type ascCIFileHit struct {
	Path     string           `json:"path"`
	FileID   string           `json:"file_id,omitempty"`
	Lang     string           `json:"lang,omitempty"`
	AlsoAt   []string         `json:"also_at,omitempty"`
	Matches  []ascCILineMatch `json:"matches"`
	Redacted bool             `json:"redacted"`
}

type ascCISearchJSON struct {
	Scope         *ascCIScopeJSON `json:"scope,omitempty"`
	Files         []ascCIFileHit  `json:"files"`
	Truncated     bool            `json:"truncated"`
	NextPageToken string          `json:"next_page_token,omitempty"`
	EstTokens     uint32          `json:"est_tokens"`
}

func ascCIHit(h *ascv1.FileHit) ascCIFileHit {
	out := ascCIFileHit{
		Path:     ascCIClean(h.GetPath()),
		FileID:   ascCIHex(h.GetFileId()),
		Lang:     ascCIClean([]byte(h.GetLang())),
		AlsoAt:   ascCICleanAll(h.GetAlsoAt(), 5),
		Matches:  []ascCILineMatch{},
		Redacted: h.GetRedacted(),
	}
	for i, m := range h.GetMatches() {
		if i >= ascCIMaxMatches {
			break
		}
		lm := ascCILineMatch{Line: m.GetLine()}
		// A redacted hit shows its line numbers only, whatever text the message carried.
		if !h.GetRedacted() {
			lm.Cols = m.GetCols()
			lm.Text = ascCIClean(m.GetText())
			lm.Before = ascCICleanAll(m.GetBefore(), ascCIMaxContext)
			lm.After = ascCICleanAll(m.GetAfter(), ascCIMaxContext)
		}
		out.Matches = append(out.Matches, lm)
	}
	return out
}

// AscSearch serves GET /repos/{owner}/{repo}/asc/search.
func AscSearch(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/asc/search repository repoAscSearch
	// ---
	// summary: Search the text of the code of a repository
	// description: Runs the search with the token of the caller, so the server decides what the caller may
	//   read. A hit the server marked redacted (it may hold a secret) lists line numbers only. The answer
	//   is capped by limit; page_token continues it.
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repository
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: rev
	//   in: query
	//   description: the revision to read (a branch, a tag or a commit id); the default branch when empty
	//   type: string
	// - name: change
	//   in: query
	//   description: read a change at its current head instead of a revision (32 letters from k to z)
	//   type: string
	// - name: stack
	//   in: query
	//   description: read the top of the stack that ends in this change (32 letters from k to z)
	//   type: string
	// - name: stack_diff_only
	//   in: query
	//   description: with stack, read only the files the stack changes
	//   type: boolean
	// - name: path
	//   in: query
	//   description: only these paths or directories; repeat for up to 16
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: lang
	//   in: query
	//   description: only these languages; repeat for up to 8
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: q
	//   in: query
	//   description: the text to find, at most 4096 bytes
	//   type: string
	//   required: true
	// - name: mode
	//   in: query
	//   description: how to match q
	//   type: string
	//   enum: [literal, regex, word]
	// - name: case_sensitive
	//   in: query
	//   type: boolean
	// - name: exclude
	//   in: query
	//   description: leave out these paths or directories; repeat for up to 16
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: context
	//   in: query
	//   description: lines of context around a match (0 to 5, default 1)
	//   type: integer
	// - name: limit
	//   in: query
	//   description: most files to return (default 50, at most 200)
	//   type: integer
	// - name: page_token
	//   in: query
	//   description: the next_page_token of an earlier answer
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscRepoSearchResult"
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"
	//   "429":
	//     description: too many requests
	//   "501":
	//     description: code intelligence is not enabled
	//   "502":
	//     description: code intelligence failed
	//   "503":
	//     description: code intelligence is unavailable
	//   "504":
	//     description: the search took too long

	q := ascCIStart(ctx, true)
	if q == nil {
		return
	}
	defer q.cancel()
	pat := ctx.FormString("q")
	if pat == "" || len(pat) > ascCIMaxQueryBytes {
		ctx.Error(http.StatusUnprocessableEntity, "", "q is required and at most 4096 bytes")
		return
	}
	mode := ascv1.SearchMode_SEARCH_MODE_LITERAL
	switch ctx.FormString("mode") {
	case "", "literal":
	case "regex":
		mode = ascv1.SearchMode_SEARCH_MODE_REGEX
	case "word":
		mode = ascv1.SearchMode_SEARCH_MODE_WORD
	default:
		ctx.Error(http.StatusUnprocessableEntity, "", "bad mode")
		return
	}
	lines := 1
	if ctx.FormString("context") != "" {
		lines = ctx.FormInt("context")
	}
	if lines < 0 || lines > ascCIMaxContext {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad context")
		return
	}
	tok, ok := ascCIPageToken(ctx)
	if !ok {
		return
	}
	stream, err := q.api.Search(q.ctx, &ascv1.SearchRequest{
		Scope:         q.scope,
		Pattern:       pat,
		Mode:          mode,
		CaseSensitive: ctx.FormBool("case_sensitive"),
		ContextLines:  uint32(lines),
		Budget:        &ascv1.Budget{MaxResults: uint32(q.limit)},
		PageToken:     tok,
	})
	if err != nil {
		ascCIFail(ctx, err)
		return
	}
	out := ascCISearchJSON{Files: []ascCIFileHit{}}
read:
	for range ascCIMaxResponses {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			ascCIFail(ctx, err)
			return
		}
		if resp.GetScope() != nil {
			out.Scope = ascCIScope(resp.GetScope())
		}
		out.Truncated = out.Truncated || resp.GetTruncated()
		out.NextPageToken = ascCIOutToken(resp.GetNextPageToken())
		out.EstTokens += resp.GetEstTokens()
		for _, f := range resp.GetFiles() {
			if len(out.Files) >= q.limit {
				out.Truncated = true
				break read
			}
			out.Files = append(out.Files, ascCIHit(f))
		}
	}
	ctx.JSON(http.StatusOK, out)
}

type ascCISymbolJSON struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	IsDef      bool   `json:"is_def"`
	Path       string `json:"path"`
	Line       uint32 `json:"line"`
	EndLine    uint32 `json:"end_line"`
	Signature  string `json:"signature,omitempty"`
	Container  string `json:"container,omitempty"`
	Doc        string `json:"doc,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

type ascCISymbolsJSON struct {
	Scope         *ascCIScopeJSON   `json:"scope,omitempty"`
	Symbols       []ascCISymbolJSON `json:"symbols"`
	Truncated     bool              `json:"truncated"`
	NextPageToken string            `json:"next_page_token,omitempty"`
	EstTokens     uint32            `json:"est_tokens"`
}

var ascCISymbolOps = map[string]ascv1.SymbolOp{
	"def":     ascv1.SymbolOp_SYMBOL_OP_DEF,
	"refs":    ascv1.SymbolOp_SYMBOL_OP_REFS,
	"callers": ascv1.SymbolOp_SYMBOL_OP_CALLERS,
	"outline": ascv1.SymbolOp_SYMBOL_OP_OUTLINE,
	"find":    ascv1.SymbolOp_SYMBOL_OP_FIND,
}

// AscSymbols serves GET /repos/{owner}/{repo}/asc/symbols. The query names the operation (op), the symbol
// (name) or, for outline, the file (file).
func AscSymbols(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/asc/symbols repository repoAscSymbols
	// ---
	// summary: Look up symbols in a repository
	// description: With op=find (the default) lists the declared symbols whose name matches name. With
	//   op=def, refs and callers it answers for the symbol name (def_path and def_line pick one definition
	//   when the name is ambiguous), and with op=outline it lists the symbols declared in the file file.
	//   Every symbol says how it was found in confidence: precise, import_match or name_match.
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repository
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: op
	//   in: query
	//   description: what to ask
	//   type: string
	//   enum: [find, def, refs, callers, outline]
	// - name: name
	//   in: query
	//   description: the symbol, at most 512 bytes; required except for outline
	//   type: string
	// - name: file
	//   in: query
	//   description: the file for outline
	//   type: string
	// - name: def_path
	//   in: query
	//   description: the file of the definition, to pick one of several
	//   type: string
	// - name: def_line
	//   in: query
	//   description: the line of the definition, to pick one of several
	//   type: integer
	// - name: kind
	//   in: query
	//   description: only these kinds of symbol; repeat for up to 8
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: rev
	//   in: query
	//   description: the revision to read; the default branch when empty
	//   type: string
	// - name: change
	//   in: query
	//   description: read a change at its current head instead of a revision (32 letters from k to z)
	//   type: string
	// - name: stack
	//   in: query
	//   description: read the top of the stack that ends in this change (32 letters from k to z)
	//   type: string
	// - name: stack_diff_only
	//   in: query
	//   description: with stack, read only the files the stack changes
	//   type: boolean
	// - name: path
	//   in: query
	//   description: only these paths or directories; repeat for up to 16
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: lang
	//   in: query
	//   description: only these languages; repeat for up to 8
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: limit
	//   in: query
	//   description: most symbols to return (default 50, at most 200)
	//   type: integer
	// - name: page_token
	//   in: query
	//   description: the next_page_token of an earlier answer
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscRepoSymbolsResult"
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"
	//   "429":
	//     description: too many requests
	//   "501":
	//     description: code intelligence is not enabled
	//   "502":
	//     description: code intelligence failed
	//   "503":
	//     description: code intelligence is unavailable
	//   "504":
	//     description: the call took too long

	q := ascCIStart(ctx, true)
	if q == nil {
		return
	}
	defer q.cancel()
	opName := ctx.FormString("op")
	if opName == "" {
		opName = "find"
	}
	op, ok := ascCISymbolOps[opName]
	if !ok {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad op")
		return
	}
	name, file, defPath := ctx.FormString("name"), ctx.FormString("file"), ctx.FormString("def_path")
	if !ascCIPlain(name, ascCIMaxNameBytes) || !ascCIPlain(file, ascCIMaxPathBytes) || !ascCIPlain(defPath, ascCIMaxPathBytes) {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad name or file")
		return
	}
	if (op == ascv1.SymbolOp_SYMBOL_OP_OUTLINE && file == "") || (op != ascv1.SymbolOp_SYMBOL_OP_OUTLINE && name == "") {
		ctx.Error(http.StatusUnprocessableEntity, "", "name (file for outline) is required")
		return
	}
	kinds := ctx.FormStrings("kind")
	if len(kinds) > ascCIMaxKinds {
		ctx.Error(http.StatusUnprocessableEntity, "", "too many kind values")
		return
	}
	for _, k := range kinds {
		if k == "" || !ascCIPlain(k, 64) {
			ctx.Error(http.StatusUnprocessableEntity, "", "bad kind")
			return
		}
	}
	defLine := ctx.FormInt("def_line")
	if defLine < 0 {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad def_line")
		return
	}
	tok, ok := ascCIPageToken(ctx)
	if !ok {
		return
	}
	resp, err := q.api.Symbols(q.ctx, &ascv1.SymbolsRequest{
		Scope:     q.scope,
		Op:        op,
		Name:      name,
		Path:      []byte(file),
		Kinds:     kinds,
		DefPath:   []byte(defPath),
		DefLine:   uint32(defLine),
		Budget:    &ascv1.Budget{MaxResults: uint32(q.limit)},
		PageToken: tok,
	})
	if err != nil {
		ascCIFail(ctx, err)
		return
	}
	out := ascCISymbolsJSON{
		Scope: ascCIScope(resp.GetScope()), Symbols: []ascCISymbolJSON{}, Truncated: resp.GetTruncated(),
		NextPageToken: ascCIOutToken(resp.GetNextPageToken()), EstTokens: resp.GetEstTokens(),
	}
	for _, s := range resp.GetSymbols() {
		if len(out.Symbols) >= q.limit {
			out.Truncated = true
			break
		}
		out.Symbols = append(out.Symbols, ascCISymbolJSON{
			Name: ascCIClean([]byte(s.GetName())), Kind: ascCIClean([]byte(s.GetKind())), IsDef: s.GetIsDef(),
			Path: ascCIClean(s.GetPath()), Line: s.GetLine(), EndLine: s.GetEndLine(),
			Signature: ascCIClean(s.GetSignature()), Container: ascCIClean([]byte(s.GetContainer())),
			Doc: ascCIClean(s.GetDoc()), Confidence: ascCIClean([]byte(s.GetConfidence())),
		})
	}
	ctx.JSON(http.StatusOK, out)
}

type ascCIChunkJSON struct {
	Path      string   `json:"path"`
	FileID    string   `json:"file_id,omitempty"`
	StartLine uint32   `json:"start_line"`
	EndLine   uint32   `json:"end_line"`
	Symbol    string   `json:"symbol,omitempty"`
	Score     float64  `json:"score"`
	Sources   []string `json:"sources,omitempty"`
	Text      string   `json:"text"`
	Redacted  bool     `json:"redacted"`
}

type ascCISemanticJSON struct {
	Scope        *ascCIScopeJSON  `json:"scope,omitempty"`
	Chunks       []ascCIChunkJSON `json:"chunks"`
	Model        string           `json:"model,omitempty"`
	EstTokens    uint32           `json:"est_tokens"`
	AnnExhausted bool             `json:"ann_exhausted"`
}

// AscSemantic serves GET /repos/{owner}/{repo}/asc/semantic.
func AscSemantic(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/asc/semantic repository repoAscSemantic
	// ---
	// summary: Search the code of a repository by meaning
	// description: Finds the pieces of code that best answer a question in words. A chunk the server marked
	//   redacted (it may hold a secret) has no text.
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repository
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repository
	//   type: string
	//   required: true
	// - name: q
	//   in: query
	//   description: the question, at most 4096 bytes
	//   type: string
	//   required: true
	// - name: mode
	//   in: query
	//   description: hybrid mixes text and vectors, vector uses vectors only
	//   type: string
	//   enum: [hybrid, vector]
	// - name: k
	//   in: query
	//   description: how many chunks to return (default 10, at most 50)
	//   type: integer
	// - name: rerank
	//   in: query
	//   description: rerank the chunks with the reranking model, when the server has one
	//   type: boolean
	// - name: rev
	//   in: query
	//   description: the revision to read; the default branch when empty
	//   type: string
	// - name: change
	//   in: query
	//   description: read a change at its current head instead of a revision (32 letters from k to z)
	//   type: string
	// - name: stack
	//   in: query
	//   description: read the top of the stack that ends in this change (32 letters from k to z)
	//   type: string
	// - name: stack_diff_only
	//   in: query
	//   description: with stack, read only the files the stack changes
	//   type: boolean
	// - name: path
	//   in: query
	//   description: only these paths or directories; repeat for up to 16
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: lang
	//   in: query
	//   description: only these languages; repeat for up to 8
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscRepoSemanticResult"
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"
	//   "429":
	//     description: too many requests
	//   "501":
	//     description: code intelligence is not enabled
	//   "502":
	//     description: code intelligence failed
	//   "503":
	//     description: code intelligence is unavailable
	//   "504":
	//     description: the search took too long

	q := ascCIStart(ctx, true)
	if q == nil {
		return
	}
	defer q.cancel()
	query := ctx.FormString("q")
	if query == "" || len(query) > ascCIMaxQueryBytes {
		ctx.Error(http.StatusUnprocessableEntity, "", "q is required and at most 4096 bytes")
		return
	}
	mode := ascv1.SemanticMode_SEMANTIC_MODE_HYBRID
	switch ctx.FormString("mode") {
	case "", "hybrid":
	case "vector":
		mode = ascv1.SemanticMode_SEMANTIC_MODE_VECTOR
	default:
		ctx.Error(http.StatusUnprocessableEntity, "", "bad mode")
		return
	}
	k := ctx.FormInt("k")
	switch {
	case k <= 0:
		k = 10
	case k > ascCIMaxSemanticK:
		k = ascCIMaxSemanticK
	}
	resp, err := q.api.Semantic(q.ctx, &ascv1.SemanticRequest{
		Scope: q.scope, Query: query, Mode: mode, K: uint32(k),
		Budget: &ascv1.Budget{MaxResults: uint32(k)}, Rerank: ctx.FormBool("rerank"),
	})
	if err != nil {
		ascCIFail(ctx, err)
		return
	}
	out := ascCISemanticJSON{
		Scope: ascCIScope(resp.GetScope()), Chunks: []ascCIChunkJSON{}, Model: ascCIClean([]byte(resp.GetModel())),
		EstTokens: resp.GetEstTokens(), AnnExhausted: resp.GetAnnExhausted(),
	}
	for _, c := range resp.GetChunks() {
		if len(out.Chunks) >= k {
			break
		}
		ch := ascCIChunkJSON{
			Path: ascCIClean(c.GetPath()), FileID: ascCIHex(c.GetFileId()), StartLine: c.GetStartLine(),
			EndLine: c.GetEndLine(), Symbol: ascCIClean([]byte(c.GetSymbol())), Score: c.GetScore(),
			Redacted: c.GetRedacted(),
		}
		for i, s := range c.GetSources() {
			if i >= 8 {
				break
			}
			ch.Sources = append(ch.Sources, ascCIClean([]byte(s)))
		}
		if !c.GetRedacted() { // a redacted chunk shows no text, whatever the message carried
			ch.Text = ascCIClean(c.GetText())
		}
		out.Chunks = append(out.Chunks, ch)
	}
	ctx.JSON(http.StatusOK, out)
}
