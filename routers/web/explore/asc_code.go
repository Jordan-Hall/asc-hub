// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	gocontext "context"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	code_indexer "forgejo.org/modules/indexer/code"
	"forgejo.org/modules/indexer/code/asc"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/common"
	"forgejo.org/services/context"
)

// The global code search over asc (SPEC 23.8, PLAN unit I26): `/explore/code` searches every repository the
// user can read, one CodeIntel.Search per repository at its default branch, and merges the per-repository
// lists with reciprocal rank fusion.
//
// Trust: the repository list comes from Forgejo's own access check, but asc decides again with the user's
// token (the context of every call carries it; there is no fallback to the internal token). A repository
// asc refuses adds nothing. The text on the page is whatever the server rendered: the per-repository
// search is asc.Indexer.Search, which drops invalid UTF-8, control and bidirectional characters again and
// shows redacted lines empty; the formatter escapes the HTML.
//
// TODO(I26-wire): routers/web/web.go must route `/explore/code` to AscExploreCode when
// REPO_INDEXER_TYPE = asc, passing asc.NewIndexer(ascclient.CodeIntel(), nil); that file belongs to another
// unit. The page needs the caller's token in the Authorization header until session-to-token minting
// exists (a browser session alone answers "needs a token", never an internal-token search).

const (
	// ascRRFK is the constant of reciprocal rank fusion, score = sum 1/(k+rank).
	ascRRFK = 60
	// ascExploreConcurrency bounds the parallel per-repository searches.
	ascExploreConcurrency = 8
	// ascExploreTimeout bounds the whole fan-out.
	ascExploreTimeout = 30 * time.Second
	// ascExploreMaxPerRepo bounds the hits one repository adds to the merge.
	ascExploreMaxPerRepo = 100
	// ascExploreMaxHits bounds the merged list.
	ascExploreMaxHits = 2000
)

// AscRepoSearchFunc searches one repository and returns its hits, best first, already formatted.
type AscRepoSearchFunc func(ctx gocontext.Context, repoID int64) ([]*code_indexer.Result, []*code_indexer.SearchResultLanguages, error)

// NewAscRepoSearch binds the asc indexer and the search to one repository at a time.
func NewAscRepoSearch(idx *asc.Indexer, keyword string, mode code_indexer.SearchMode, language, path string) AscRepoSearchFunc {
	return func(ctx gocontext.Context, repoID int64) ([]*code_indexer.Result, []*code_indexer.SearchResultLanguages, error) {
		_, found, langs, err := idx.Search(ctx, &code_indexer.SearchOptions{
			RepoIDs: []int64{repoID}, Keyword: keyword, Mode: mode, Language: language, Filename: path,
		})
		if err != nil {
			return nil, nil, err
		}
		out := make([]*code_indexer.Result, 0, min(len(found), ascExploreMaxPerRepo))
		for _, r := range found {
			if len(out) >= ascExploreMaxPerRepo {
				break
			}
			res, err := idx.Formatter().Format(r)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, res)
		}
		return out, langs, nil
	}
}

// AscFanOutResult is what the fan-out gathered.
type AscFanOutResult struct {
	// Lists holds one ranked list per repository, in the order of the repository ids; failed ones are nil.
	Lists [][]*code_indexer.Result
	// Languages is the merged language count.
	Languages []*code_indexer.SearchResultLanguages
	// Failed counts repositories whose search failed for a reason other than "asc refuses or does not know it".
	Failed int
}

// AscFanOut runs search for every repository id, at most ascExploreConcurrency at once. A missing user
// token aborts the whole search; any other per-repository failure is counted and the rest still answer,
// unless every repository failed, which returns the first error.
func AscFanOut(ctx gocontext.Context, repoIDs []int64, search AscRepoSearchFunc) (*AscFanOutResult, error) {
	if len(repoIDs) > asc.MaxRepos {
		return nil, asc.ErrNoRepos
	}
	ctx, cancel := gocontext.WithTimeout(ctx, ascExploreTimeout)
	defer cancel()

	res := &AscFanOutResult{Lists: make([][]*code_indexer.Result, len(repoIDs))}
	langs := make([][]*code_indexer.SearchResultLanguages, len(repoIDs))
	errs := make([]error, len(repoIDs))
	sem := make(chan struct{}, ascExploreConcurrency)
	var wg sync.WaitGroup
	for i, id := range repoIDs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				errs[i] = ctx.Err()
				return
			}
			res.Lists[i], langs[i], errs[i] = search(ctx, id)
			if errors.Is(errs[i], ascclient.ErrNoCredential) {
				cancel() // no credential: stop the others, nothing is sent without one
			}
		}()
	}
	wg.Wait()

	var first error
	for i, err := range errs {
		if errors.Is(err, ascclient.ErrNoCredential) {
			return nil, err
		}
		if err != nil {
			res.Failed++
			res.Lists[i] = nil
			if first == nil {
				first = err
			}
		}
	}
	if len(repoIDs) > 0 && res.Failed == len(repoIDs) {
		return nil, first
	}
	byLang := map[string]*code_indexer.SearchResultLanguages{}
	for _, ls := range langs {
		for _, l := range ls {
			m, ok := byLang[l.Language]
			if !ok {
				m = &code_indexer.SearchResultLanguages{Language: l.Language, Color: l.Color}
				byLang[l.Language] = m
				res.Languages = append(res.Languages, m)
			}
			m.Count += l.Count
		}
	}
	slices.SortFunc(res.Languages, func(a, b *code_indexer.SearchResultLanguages) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Language, b.Language)
	})
	return res, nil
}

// AscRRF merges ranked lists with reciprocal rank fusion: an item at 0-based position r of a list scores
// 1/(ascRRFK+r+1), and items sort by score, highest first. Ties keep the order of the lists, then of the
// positions, so the result is deterministic. The merge is capped at ascExploreMaxHits.
func AscRRF[T any](lists [][]T) []T {
	type scored struct {
		item  T
		score float64
		list  int
		pos   int
	}
	var all []scored
	for li, l := range lists {
		for pos, item := range l {
			all = append(all, scored{item: item, score: 1 / float64(ascRRFK+pos+1), list: li, pos: pos})
		}
	}
	slices.SortStableFunc(all, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		case a.list != b.list:
			return a.list - b.list
		}
		return a.pos - b.pos
	})
	if len(all) > ascExploreMaxHits {
		all = all[:ascExploreMaxHits]
	}
	out := make([]T, len(all))
	for i, s := range all {
		out[i] = s.item
	}
	return out
}

// ascExploreToken returns the caller's token from the Authorization header ("Bearer", "token" or the
// password of "Basic"). ascclient.WithUserToken validates it.
func ascExploreToken(r *http.Request) string {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
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

// AscExploreCode renders `/explore/code` from asc. It mirrors Code and replaces its single indexer call
// with the fan-out and the merge.
func AscExploreCode(ctx *context.Context, idx *asc.Indexer) {
	if !setting.Indexer.RepoIndexerEnabled || setting.Service.Explore.DisableCodePage {
		ctx.Redirect(setting.AppSubURL + "/explore")
		return
	}
	ctx.Data["UsersPageIsDisabled"] = setting.Service.Explore.DisableUsersPage
	ctx.Data["OrganizationsPageIsDisabled"] = setting.Service.Explore.DisableOrganizationsPage
	ctx.Data["IsRepoIndexerEnabled"] = setting.Indexer.RepoIndexerEnabled
	ctx.Data["Title"] = ctx.Tr("explore")
	ctx.Data["PageIsExplore"] = true
	ctx.Data["PageIsExploreCode"] = true
	ctx.Data["PageIsViewCode"] = true

	opts := common.InitCodeSearchOptions(ctx)
	mode := common.CodeSearchIndexerMode(ctx)
	keyword, language, path := opts.Keyword, opts.Language, opts.Path

	if keyword == "" {
		ctx.HTML(http.StatusOK, tplExploreCode)
		return
	}
	page := max(ctx.FormInt("page"), 1)

	// asc decides again with the user's token; without one nothing is searched.
	callCtx, err := ascclient.WithUserToken(ctx.Req.Context(), ascExploreToken(ctx.Req))
	if err != nil {
		ctx.Data["CodeIndexerUnavailable"] = true
		ctx.Data["AscNeedsToken"] = true
		ctx.HTML(http.StatusOK, tplExploreCode)
		return
	}

	repoIDs, err := repo_model.FindUserCodeAccessibleRepoIDs(ctx, ctx.Doer)
	if err != nil {
		ctx.ServerError("FindUserCodeAccessibleRepoIDs", err)
		return
	}
	if len(repoIDs) > asc.MaxRepos {
		repoIDs = repoIDs[:asc.MaxRepos]
		ctx.Data["AscReposTruncated"] = true
	}

	var (
		total   int
		results code_indexer.Results
		langs   []*code_indexer.SearchResultLanguages
	)
	if len(repoIDs) > 0 {
		fan, err := AscFanOut(callCtx, repoIDs, NewAscRepoSearch(idx, keyword, mode, language, path))
		if err != nil {
			log.Error("asc explore code: %v", err) // the status text of asc stays in the log
			ctx.Data["CodeIndexerUnavailable"] = true
			ctx.HTML(http.StatusOK, tplExploreCode)
			return
		}
		ctx.Data["AscFailedRepos"] = fan.Failed
		merged := AscRRF(fan.Lists)
		total, langs = len(merged), fan.Languages
		size := setting.UI.RepoSearchPagingNum
		skip := min((page-1)*size, len(merged))
		results = merged[skip:min(skip+size, len(merged))]
	}
	ctx.Data["CodeIndexerUnavailable"] = false

	repoMaps, err := repo_model.GetRepositoriesMapByIDs(ctx, results.RepoIDs())
	if err != nil {
		ctx.ServerError("GetRepositoriesMapByIDs", err)
		return
	}
	ctx.Data["RepoMaps"] = repoMaps
	kept := results[:0:0]
	for _, r := range results {
		if _, ok := repoMaps[r.RepoID]; ok {
			kept = append(kept, r)
		}
	}
	ctx.Data["SearchResults"] = kept
	ctx.Data["SearchResultLanguages"] = langs

	pager := context.NewPagination(total, setting.UI.RepoSearchPagingNum, page, 5)
	pager.SetDefaultParams(ctx)
	pager.AddParam(ctx, "l", "Language")
	pager.AddParam(ctx, "mode", "CodeSearchMode")
	ctx.Data["Page"] = pager
	ctx.HTML(http.StatusOK, tplExploreCode)
}
