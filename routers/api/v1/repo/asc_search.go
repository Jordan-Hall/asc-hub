// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	api "forgejo.org/modules/structs"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"
)

// Cross-repository search, the symbol panel and the index status (BUILDOUT section 14):
//
//	GET /api/v1/asc/search                           text over several repositories
//	GET /api/v1/asc/symbols                          a symbol by name over several repositories
//	GET /api/v1/asc/semantic                         by meaning over several repositories
//	GET /api/v1/repos/{owner}/{repo}/asc/symbol-panel  the definition, references and callers of a symbol
//	GET /api/v1/repos/{owner}/{repo}/asc/index         how much of the repository the index covers
//
// Trust. The caller's token is the credential of every call to asc, never the internal token, so asc
// decides again what the caller may read. Before that Forgejo decides for itself which repositories the
// search covers (ascsearch.Resolve: the permission of the signed-in user, reduced by the access token),
// and the call names exactly those, so an answer can only be about repositories both sides allow. A
// repository the caller cannot read, or that does not exist, is not mentioned: it is not counted and
// not listed. The text of an asc error never reaches the client.

// Defaults of the request fields shared by the cross-repository routes.
const (
	ascSearchDefaultContext = 1
	ascSearchMaxFormValues  = ascsearch.MaxRepos
)

// ascCross is a checked cross-repository request.
type ascCross struct {
	svc   *ascsearch.Service
	ctx   gocontext.Context
	res   *ascsearch.Resolved
	limit int
}

func ascCrossFail(ctx *context.APIContext, err error) {
	switch ascsearch.KindOf(err) {
	case ascsearch.KindFailed, ascsearch.KindBadAnswer, ascsearch.KindUnavailable, ascsearch.KindTimeout:
		log.Error("asc cross search: %v", err) // the status text of asc stays in the log
	}
	if ascsearch.KindOf(err) == ascsearch.KindNotFound {
		ctx.NotFound()
		return
	}
	ctx.Error(ascsearch.HTTPStatus(err), "", ascsearch.PublicMessage(err))
}

// ascCrossStart runs the checks the cross-repository routes share: the integration is on, the caller has a
// token and a signed-in user, and the repositories are resolved. maxRepos bounds them (zero means all that a
// search may cover). It writes the error and returns nil when a check fails.
func ascCrossStart(ctx *context.APIContext, maxRepos int) (*ascCross, func()) {
	if !setting.Asc.Enabled {
		ctx.Error(http.StatusNotImplemented, "", "code intelligence is not enabled")
		return nil, nil
	}
	svc, err := ascsearch.Default()
	if err != nil {
		ascCIFailStart(ctx, err)
		return nil, nil
	}
	callCtx, err := ascclient.WithUserToken(ctx.Req.Context(), ascCIToken(ctx.Req))
	if err != nil {
		ctx.Error(http.StatusUnauthorized, "", "a token is required for code intelligence")
		return nil, nil
	}
	doer := ctx.Doer()
	if doer == nil || !ctx.IsSigned() {
		ctx.Error(http.StatusUnauthorized, "", "a token is required for code intelligence")
		return nil, nil
	}
	reducer := ctx.Reducer()
	if reducer == nil { // fail closed: without a reducer the token's limits are unknown
		ctx.Error(http.StatusInternalServerError, "ascCrossStart", "no authorization reducer")
		return nil, nil
	}
	repos := ctx.FormStrings("repo")
	if len(repos) > ascSearchMaxFormValues {
		ctx.Error(http.StatusUnprocessableEntity, "", "too many repo values")
		return nil, nil
	}
	res, err := ascsearch.Resolve(ctx, doer, reducer, ascsearch.Selector{Repos: repos, Owner: ctx.FormString("owner"), Max: maxRepos})
	if err != nil {
		if ascsearch.KindOf(err) == ascsearch.KindBadRequest {
			ctx.Error(http.StatusUnprocessableEntity, "", ascsearch.PublicMessage(err))
			return nil, nil
		}
		ctx.Error(http.StatusInternalServerError, "ascsearch.Resolve", err)
		return nil, nil
	}
	if len(repos) > 0 && len(res.Repos) == 0 {
		ctx.NotFound() // none of the named repositories is readable: the same answer as "does not exist"
		return nil, nil
	}
	c, cancel := gocontext.WithTimeout(callCtx, ascCITimeout)
	return &ascCross{svc: svc, ctx: c, res: res, limit: ctx.FormInt("limit")}, cancel
}

// ascCrossScope reads the path, exclude and lang filters.
func ascCrossScope(ctx *context.APIContext) (paths, exclude, langs []string) {
	return ctx.FormStrings("path"), ctx.FormStrings("exclude"), ctx.FormStrings("lang")
}

func facets(f []api.AscFacet) []api.AscFacet {
	if f == nil {
		return []api.AscFacet{}
	}
	return f
}

// AscCrossSearch serves GET /asc/search.
func AscCrossSearch(ctx *context.APIContext) {
	// swagger:operation GET /asc/search miscellaneous ascCrossSearch
	// ---
	// summary: Search the text of the code of several repositories
	// description: Searches the default branch of the repositories the caller can read that are stored in
	//   asc, in one call of the search service for up to 64 of them. Name repositories with repo, or limit the
	//   search to the repositories of one user or organisation with owner; with neither, every readable
	//   repository is searched (at most 200, the most recently updated first, and repos_truncated says
	//   when there are more). A repository the caller cannot read is left out without a trace. Runs
	//   with the token of the caller. A hit the server marked redacted (it may hold a secret) lists
	//   line numbers only. A pattern shorter than 3 bytes is refused.
	// produces:
	// - application/json
	// parameters:
	// - name: q
	//   in: query
	//   description: the text to find, 3 to 512 bytes
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
	// - name: repo
	//   in: query
	//   description: a repository, "owner/name"; repeat for up to 200
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: owner
	//   in: query
	//   description: only the repositories of this user or organisation
	//   type: string
	// - name: path
	//   in: query
	//   description: only these paths or directories; repeat for up to 16
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: exclude
	//   in: query
	//   description: leave out these paths or directories; repeat for up to 16
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
	// - name: context
	//   in: query
	//   description: lines of context around a match (0 to 5, default 1)
	//   type: integer
	// - name: limit
	//   in: query
	//   description: most files to return over all repositories (default 50, at most 200)
	//   type: integer
	// - name: page_token
	//   in: query
	//   description: the next_page_token of an earlier answer; only for a search of at most 64 repositories
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscCrossSearchResult"
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

	env, cancel := ascCrossStart(ctx, 0)
	if env == nil {
		return
	}
	defer cancel()

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
	lines := ascSearchDefaultContext
	if ctx.FormString("context") != "" {
		lines = ctx.FormInt("context")
	}
	if lines < 0 || lines > ascsearch.MaxContext {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad context")
		return
	}
	paths, exclude, langs := ascCrossScope(ctx)
	res, err := env.svc.Text(env.ctx, env.res.Repos, ascsearch.TextQuery{
		Pattern: ctx.FormString("q"), Mode: mode, CaseSensitive: ctx.FormBool("case_sensitive"), Context: uint32(lines),
		Paths: paths, Exclude: exclude, Langs: langs, Limit: env.limit, PageToken: ctx.FormString("page_token"),
	})
	if err != nil {
		ascCrossFail(ctx, err)
		return
	}
	repos := res.Repos
	if repos == nil {
		repos = []api.AscRepoHits{}
	}
	langFacets, dirFacets := ascsearch.Facets(repos)
	ctx.JSON(http.StatusOK, api.AscCrossSearchResult{
		Repos: repos, Files: res.Files, Truncated: res.Truncated, NextPageToken: res.NextPageToken,
		SearchedRepos: res.Searched, UnindexedRepos: res.Unindexed, FailedRepos: res.Failed,
		ReposTotal: env.res.Total, ReposTruncated: env.res.Truncated, EstTokens: res.EstTokens,
		Languages: facets(langFacets), Directories: facets(dirFacets),
	})
}

// AscCrossSymbols serves GET /asc/symbols.
func AscCrossSymbols(ctx *context.APIContext) {
	// swagger:operation GET /asc/symbols miscellaneous ascCrossSymbols
	// ---
	// summary: Look a symbol up by name in several repositories
	// description: Lists the declared symbols whose name matches, in the default branch of the repositories
	//   the caller can read that are stored in asc. Name repositories with repo, or limit the lookup to the
	//   repositories of one user or organisation with owner; with neither, every readable repository is
	//   asked (at most 200, the most recently updated first). A repository the caller cannot read is left out
	//   without a trace. Runs with the token of the caller. Every symbol says how it was found in
	//   the confidence field (precise, import_match or name_match).
	// produces:
	// - application/json
	// parameters:
	// - name: name
	//   in: query
	//   description: the symbol, 1 to 256 bytes
	//   type: string
	//   required: true
	// - name: match
	//   in: query
	//   description: how the name must match (default exact)
	//   type: string
	//   enum: [exact, prefix, fuzzy]
	// - name: kind
	//   in: query
	//   description: only these kinds of symbol; repeat for up to 8
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
	// - name: definitions_only
	//   in: query
	//   description: leave out the uses of the symbol
	//   type: boolean
	// - name: repo
	//   in: query
	//   description: a repository, "owner/name"; repeat for up to 200
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: owner
	//   in: query
	//   description: only the repositories of this user or organisation
	//   type: string
	// - name: limit
	//   in: query
	//   description: most symbols to return (default 50, at most 200)
	//   type: integer
	// - name: page_token
	//   in: query
	//   description: the next_page_token of an earlier answer; only for a lookup in at most 64 repositories
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscCrossSymbolsResult"
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
	//     description: the lookup took too long

	env, cancel := ascCrossStart(ctx, 0)
	if env == nil {
		return
	}
	defer cancel()

	match := ascv1.SymbolMatch_SYMBOL_MATCH_EXACT
	switch ctx.FormString("match") {
	case "", "exact":
	case "prefix":
		match = ascv1.SymbolMatch_SYMBOL_MATCH_PREFIX
	case "fuzzy":
		match = ascv1.SymbolMatch_SYMBOL_MATCH_FUZZY
	default:
		ctx.Error(http.StatusUnprocessableEntity, "", "bad match")
		return
	}
	res, err := env.svc.Symbols(env.ctx, env.res.Repos, ascsearch.SymbolQuery{
		Name: ctx.FormString("name"), Match: match, Kinds: ctx.FormStrings("kind"), Langs: ctx.FormStrings("lang"),
		DefinitionsOnly: ctx.FormBool("definitions_only"), Limit: env.limit, PageToken: ctx.FormString("page_token"),
	})
	if err != nil {
		ascCrossFail(ctx, err)
		return
	}
	hits := res.Hits
	if hits == nil {
		hits = []api.AscSymbolHit{}
	}
	ctx.JSON(http.StatusOK, api.AscCrossSymbolsResult{
		Symbols: hits, Truncated: res.Truncated, NextPageToken: res.NextPageToken, FailedRepos: res.Failed,
		ReposTotal: env.res.Total, ReposTruncated: env.res.Truncated,
	})
}

// AscCrossSemantic serves GET /asc/semantic.
func AscCrossSemantic(ctx *context.APIContext) {
	// swagger:operation GET /asc/semantic miscellaneous ascCrossSemantic
	// ---
	// summary: Search the code of several repositories by meaning
	// description: Finds the pieces of code that best answer a question in words, in the default branch of
	//   the repositories the caller can read that are stored in asc. Every repository costs one query
	//   embedding, so a search covers at most 16 repositories, the most recently updated first, and
	//   repos_truncated says when there are more; name them with repo, or pick an owner. A repository the
	//   caller cannot read is left out without a trace. Runs with the token of the caller. A chunk the
	//   server marked redacted (it may hold a secret) has no text.
	// produces:
	// - application/json
	// parameters:
	// - name: q
	//   in: query
	//   description: the question, at most 1000 bytes
	//   type: string
	//   required: true
	// - name: k
	//   in: query
	//   description: how many chunks to return (default 10, at most 50)
	//   type: integer
	// - name: rerank
	//   in: query
	//   description: rerank the chunks with the reranking model, when the server has one
	//   type: boolean
	// - name: repo
	//   in: query
	//   description: a repository, "owner/name"; repeat for up to 16
	//   type: array
	//   items:
	//     type: string
	//   collectionFormat: multi
	// - name: owner
	//   in: query
	//   description: only the repositories of this user or organisation
	//   type: string
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
	//     "$ref": "#/responses/AscCrossSemanticResult"
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

	env, cancel := ascCrossStart(ctx, ascsearch.MaxSemanticRepos)
	if env == nil {
		return
	}
	defer cancel()

	res, err := env.svc.Meaning(env.ctx, env.res.Repos, ascsearch.MeaningQuery{
		Query: ctx.FormString("q"), K: ctx.FormInt("k"), Rerank: ctx.FormBool("rerank"),
		Paths: ctx.FormStrings("path"), Langs: ctx.FormStrings("lang"),
	})
	if err != nil {
		ascCrossFail(ctx, err)
		return
	}
	chunks := res.Chunks
	if chunks == nil {
		chunks = []api.AscChunk{}
	}
	ctx.JSON(http.StatusOK, api.AscCrossSemanticResult{
		Chunks: chunks, Model: res.Model, SearchedRepos: res.Searched, FailedRepos: res.Failed,
		ReposTotal: env.res.Total, ReposTruncated: env.res.Truncated, EstTokens: res.EstTokens, AnnExhausted: res.AnnExhausted,
	})
}

// ascRepoOf makes the ascsearch view of the repository of the request.
func ascRepoOf(ctx *context.APIContext, rev string) (ascsearch.Repo, bool) {
	repo := ctx.Repository()
	if repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound() // only a repository that lives in asc has a code index
		return ascsearch.Repo{}, false
	}
	name, err := ascsearch.NameOf(repo)
	if err != nil {
		ctx.NotFound()
		return ascsearch.Repo{}, false
	}
	return ascsearch.Repo{ID: repo.ID, Name: name, FullName: repo.FullName(), Branch: repo.DefaultBranch, Rev: rev}, true
}

// AscSymbolPanel serves GET /repos/{owner}/{repo}/asc/symbol-panel.
func AscSymbolPanel(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/asc/symbol-panel repository repoAscSymbolPanel
	// ---
	// summary: Get the definition, references and callers of a symbol
	// description: The panel of the file view. It holds the definition of the symbol (more than one when
	//   the name is ambiguous; name, or path and line, picks one), its references and callers in the
	//   repository grouped by file, and with cross_repo its references in other repositories that the
	//   caller can read. Runs with the token of the caller. Every occurrence says how it was found in
	//   the confidence field (precise, import_match or name_match).
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
	// - name: name
	//   in: query
	//   description: the symbol, at most 256 bytes; required unless path and line are given
	//   type: string
	// - name: path
	//   in: query
	//   description: the file of the definition, to pick one of several
	//   type: string
	// - name: line
	//   in: query
	//   description: the line of the definition, to pick one of several
	//   type: integer
	// - name: rev
	//   in: query
	//   description: the revision to read; the default branch when empty
	//   type: string
	// - name: cross_repo
	//   in: query
	//   description: also list references in other repositories the caller can read
	//   type: boolean
	// - name: limit
	//   in: query
	//   description: scales how many occurrences to list (default 50, at most 200; each list is 5 times that)
	//   type: integer
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscSymbolPanel"
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

	q := ascCIStart(ctx, false)
	if q == nil {
		return
	}
	defer q.cancel()
	if q.svc.Cross == nil {
		ctx.Error(http.StatusNotImplemented, "", "code intelligence is not enabled")
		return
	}
	repo, ok := ascRepoOf(ctx, q.scope.GetRev())
	if !ok {
		return
	}
	line := ctx.FormInt("line")
	if line < 0 {
		ctx.Error(http.StatusUnprocessableEntity, "", "bad line")
		return
	}
	doer, reducer := ctx.Doer(), ctx.Reducer()
	if reducer == nil {
		ctx.Error(http.StatusInternalServerError, "AscSymbolPanel", "no authorization reducer")
		return
	}
	query := ascsearch.PanelQuery{
		Repo: repo, Rev: repo.Rev, Name: ctx.FormString("name"), Path: ctx.FormString("path"), Line: uint32(line),
		Limit: q.limit * 5,
	}
	if ctx.FormBool("cross_repo") {
		query.CrossRepo = true
		query.Allow = func(ascName string) bool {
			_, ok := ascsearch.CanRead(ctx, doer, reducer, ascName)
			return ok
		}
		others, err := ascsearch.Resolve(ctx, doer, reducer, ascsearch.Selector{Max: ascsearch.MaxSemanticRepos + 1})
		if err != nil {
			ctx.Error(http.StatusInternalServerError, "ascsearch.Resolve", err)
			return
		}
		query.Others = others.Repos
	}
	panel, err := q.svc.Panel(q.ctx, query)
	if err != nil {
		ascCrossFail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, panel)
}

// AscIndexStatus serves GET /repos/{owner}/{repo}/asc/index.
func AscIndexStatus(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/asc/index repository repoAscIndexStatus
	// ---
	// summary: Get how much of a repository the code index covers
	// description: Says, for the index that text and symbol search read and for the one that the search by
	//   meaning reads, how much of the repository at the revision is indexed and how far the index lags
	//   behind. Results of a search are incomplete while the coverage is below 100 percent.
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
	//   description: the revision; the default branch when empty
	//   type: string
	// responses:
	//   "200":
	//     "$ref": "#/responses/AscIndexStatusResult"
	//   "401":
	//     "$ref": "#/responses/unauthorized"
	//   "404":
	//     "$ref": "#/responses/notFound"
	//   "422":
	//     "$ref": "#/responses/validationError"
	//   "501":
	//     description: code intelligence is not enabled
	//   "502":
	//     description: code intelligence failed
	//   "503":
	//     description: code intelligence is unavailable

	q := ascCIStart(ctx, false)
	if q == nil {
		return
	}
	defer q.cancel()
	repo, ok := ascRepoOf(ctx, q.scope.GetRev())
	if !ok {
		return
	}
	files := q.svc.Coverage(q.ctx, []ascsearch.Repo{repo}, ascsearch.IndexFiles)
	vectors := q.svc.Coverage(q.ctx, []ascsearch.Repo{repo}, ascsearch.IndexVectors)
	if files.Known == 0 && vectors.Known == 0 {
		// Not even one index answered: the server is down or refuses, which is not "zero percent".
		ctx.Error(http.StatusBadGateway, "", "code intelligence failed")
		return
	}
	ctx.JSON(http.StatusOK, api.AscIndexStatusResult{Repo: repo.Name, Rev: repo.Rev, Files: files, Vectors: vectors})
}
