// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const scopeChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"

func fileHit(path, id, text string) *ascv1.FileHit {
	return &ascv1.FileHit{
		Path: []byte(path), Lang: "Rust", FileId: []byte(id),
		Matches: []*ascv1.LineMatch{{Line: 4, Cols: []uint32{0}, Text: []byte(text)}},
	}
}

func TestAscSearchAllBranches(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{
		search: func(req *ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			switch req.GetScope().GetRev() {
			case "refs/heads/master":
				return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{fileHit("src/a.rs", "aa", "needle one"), fileHit("src/only_master.rs", "bb", "needle two")}}}, nil
			case "refs/heads/branch2":
				return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{
					fileHit("src/a.rs", "aa", "needle one"),          // the same file as on master
					fileHit("src/a.rs", "cc", "needle changed"),      // the same path with other content
					fileHit("docs/b2.md", "dd", "needle on branch2"), // a file only this branch has
				}}}, nil
			}
			return nil, status.Error(codes.NotFound, "unknown")
		},
		index: indexStatusOf(&ascv1.IndexStatus{Index: "files", Coverage: 1}),
	})

	page := getAscSearch(t, "?q=needle&scope=branches")
	require.Equal(t, http.StatusOK, page.status, page.body)

	var revs []string
	for _, r := range fake.searches {
		revs = append(revs, r.GetScope().GetRev())
	}
	assert.ElementsMatch(t, []string{"refs/heads/master", "refs/heads/branch2"}, revs, "every live branch, and no deleted one (foo, bar)")
	assert.Contains(t, page.body, "2 branches")
	assert.Contains(t, page.body, "4 results", "the same file on two branches is listed once: 4 distinct files")
	assert.Equal(t, 1, strings.Count(page.body, "needle one"))
	assert.Contains(t, page.body, "needle changed")
	assert.Contains(t, page.body, `href="/user2/repo1/src/branch/branch2/docs/b2.md"`)
	// The shared file says which branches have it, with a link to each.
	assert.Contains(t, page.body, `On <a class="tw-font-mono" href="/user2/repo1/src/branch/master">master</a> <a class="tw-font-mono" href="/user2/repo1/src/branch/branch2">branch2</a>`)
	assert.Contains(t, page.body, "Read for 2 branches")
	assert.Equal(t, "refs/heads/master", fake.statuses[0].GetScope().GetRev(), "the coverage is read for the first branch, the default one")

	t.Run("a branch asc does not know adds nothing and is no failure", func(t *testing.T) {
		fake.search = func(req *ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			if req.GetScope().GetRev() == "refs/heads/branch2" {
				return nil, status.Error(codes.NotFound, "unknown")
			}
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{fileHit("a.rs", "aa", "needle")}}}, nil
		}
		page := getAscSearch(t, "?q=needle&scope=branches")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.NotContains(t, page.body, "could not be searched")
		assert.Contains(t, page.body, "1 result")
	})

	t.Run("a failing branch is reported, and all failing is an error", func(t *testing.T) {
		fake.search = func(req *ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			if req.GetScope().GetRev() == "refs/heads/branch2" {
				return nil, status.Error(codes.Internal, "boom /srv/secret")
			}
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{fileHit("a.rs", "aa", "needle")}}}, nil
		}
		page := getAscSearch(t, "?q=needle&scope=branches")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.Contains(t, page.body, "1 of 2 branches could not be searched")
		assert.NotContains(t, page.body, "/srv/secret")
		assert.Contains(t, page.body, "The index does not cover all of this scope yet", "an incomplete search is not complete")

		fake.search = func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return nil, status.Error(codes.Internal, "boom /srv/secret")
		}
		page = getAscSearch(t, "?q=needle&scope=branches")
		assert.Equal(t, http.StatusBadGateway, page.status)
		assert.NotContains(t, page.body, "/srv/secret")
	})

	t.Run("symbols and chunks are merged the same way", func(t *testing.T) {
		fake.symbols = func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{ascSym("shared", "src/lib.rs", 10, true, "precise")}}, nil
		}
		page := getAscSearch(t, "?q=shared&mode=symbol&scope=branches")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.Equal(t, 1, strings.Count(page.body, `class="tw-font-mono">shared</span>`), "one symbol, two branches")
		assert.Contains(t, page.body, `href="/user2/repo1/src/branch/branch2">branch2</a>`)

		fake.semantic = func(req *ascv1.SemanticRequest) (*ascv1.SemanticResponse, error) {
			return &ascv1.SemanticResponse{Chunks: []*ascv1.Chunk{{Path: []byte("a.rs"), StartLine: 1, EndLine: 2, Score: 0.5, Text: []byte("same chunk")}}}, nil
		}
		page = getAscSearch(t, "?q=where+is+it&mode=semantic&scope=branches")
		require.Equal(t, http.StatusOK, page.status, page.body)
		assert.Equal(t, 1, strings.Count(page.body, "same chunk"))
	})
}

func TestAscSearchChangeAndStack(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	// Pull request 1 of the fixtures has index 2 in repository 1; make it the pull request of a change.
	require.NoError(t, db.Insert(db.DefaultContext, &issues_model.PullRequestAscStack{PullID: 1, RepoID: 1, AscChange: scopeChange}))
	// Pull request 2 (index 3) is stacked in another repository's row: it must not be reachable here.
	require.NoError(t, db.Insert(db.DefaultContext, &issues_model.PullRequestAscStack{PullID: 2, RepoID: 99, AscChange: scopeChange}))
	fake := startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{fileHit("src/a.rs", "aa", "needle")}}}, nil
		},
		index: indexStatusOf(&ascv1.IndexStatus{Index: "files", Coverage: 1}),
	})

	page := getAscSearch(t, "?q=needle&scope=change&pull=2&path=src&lang=Rust")
	require.Equal(t, http.StatusOK, page.status, page.body)
	sc := fake.searches[0].GetScope()
	assert.Len(t, sc.GetChange(), 16)
	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, sc.GetChange())
	assert.Nil(t, sc.GetStackTop())
	assert.Equal(t, []string{"src"}, sc.GetPaths())
	assert.Equal(t, []string{"Rust"}, sc.GetLangs())
	assert.Contains(t, page.body, "change of #2")
	assert.Equal(t, sc.GetChange(), fake.statuses[0].GetScope().GetChange(), "the coverage is read for the same scope")

	page = getAscSearch(t, "?q=needle&scope=stack&pull=2&diff=1")
	require.Equal(t, http.StatusOK, page.status, page.body)
	sc = fake.searches[len(fake.searches)-1].GetScope()
	assert.Len(t, sc.GetStackTop(), 16)
	assert.Nil(t, sc.GetChange())
	assert.True(t, sc.GetStackDiffOnly())
	assert.Contains(t, page.body, "stack up to #2 (changed files)")
	assert.Contains(t, page.body, `checked`)

	n := len(fake.searches)
	for name, q := range map[string]struct {
		query  string
		status int
	}{
		"no pull":             {"?q=needle&scope=change", http.StatusBadRequest},
		"zero":                {"?q=needle&scope=change&pull=0", http.StatusBadRequest},
		"negative":            {"?q=needle&scope=change&pull=-3", http.StatusBadRequest},
		"text":                {"?q=needle&scope=stack&pull=abc", http.StatusBadRequest},
		"huge":                {"?q=needle&scope=stack&pull=99999999999999999999999", http.StatusBadRequest},
		"unknown index":       {"?q=needle&scope=change&pull=777", http.StatusNotFound},
		"not stacked":         {"?q=needle&scope=change&pull=3", http.StatusNotFound},
		"row of another repo": {"?q=needle&scope=stack&pull=3", http.StatusNotFound},
	} {
		page := getAscSearch(t, q.query)
		assert.Equal(t, q.status, page.status, name)
		assert.Contains(t, page.body, `id="asc-search-form"`, "the form is still shown: %s", name)
	}
	assert.Equal(t, n, len(fake.searches), "nothing is searched for a change that is not this repository's")
}

func TestAscSearchScopeRepos(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	fake := startFakeSearch(t, &fakeSearchServer{})

	for query, want := range map[string]url.Values{
		"?q=needle&scope=repos&lang=Rust&path=src&match=word&case=1": {"q": {"needle"}, "mode": {"text"}, "match": {"word"}, "case": {"1"}, "lang": {"Rust"}, "path": {"src"}},
		"?q=Retry&mode=symbol&scope=repos":                           {"q": {"Retry"}, "mode": {"symbol"}},
		"?q=where+is+it&mode=semantic&scope=repos":                   {"q": {"where is it"}, "mode": {"meaning"}},
	} {
		page := getAscSearch(t, query)
		require.Equal(t, http.StatusSeeOther, page.status, query)
		loc, err := url.Parse(page.ctx.Resp.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "/explore/code", loc.Path)
		for k, v := range want {
			assert.Equal(t, v, loc.Query()[k], "%s %s", query, k)
		}
	}
	assert.Zero(t, fake.calls(), "this repository is not searched")

	// Without a question the form is shown, whatever the scope.
	page := getAscSearch(t, "?scope=repos")
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, `value="repos" checked`)

	// An unknown scope is this version.
	page = getAscSearch(t, "?scope=nonsense")
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, `value="version" checked`)
}

func TestAscSearchFacetsAndForm(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	startFakeSearch(t, &fakeSearchServer{
		search: func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{
				fileHit("src/a.rs", "aa", "needle"), fileHit("src/b.rs", "bb", "needle"), fileHit("docs/c.md", "cc", "needle"), fileHit("top.txt", "dd", "needle"),
			}}}, nil
		},
	})
	page := getAscSearch(t, "?q=needle&ref=master&lang=Rust")
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `<span>Rust</span><span class="asc-count">4</span>`)
	assert.Contains(t, page.body, `<span>src/</span><span class="asc-count">2</span>`)
	assert.Contains(t, page.body, `<span>docs/</span><span class="asc-count">1</span>`)
	assert.NotContains(t, page.body, `<span>(root)</span>`)
	assert.Contains(t, page.body, "/user2/repo1/asc/search?lang=Rust&amp;match=literal&amp;mode=lexical&amp;path=src&amp;q=needle&amp;ref=master")
	assert.Contains(t, page.body, `aria-current="true"`)
	// The scope switcher and the narrowing fields.
	for _, v := range []string{"version", "change", "stack", "branches", "repos"} {
		assert.Contains(t, page.body, `name="scope" value="`+v+`"`)
	}
	assert.Contains(t, page.body, `id="asc-search-pull"`)
	assert.Contains(t, page.body, `id="asc-search-diff"`)
	assert.Contains(t, page.body, "This version (master)")
}

func TestAscSearchURLs(t *testing.T) {
	q := ascSearchQuery{mode: "lexical", text: "a b&c", match: "regex", caseSensitive: true, scope: "stack", rawRef: "dev", rawPull: "7", stackDiff: true, lang: "Go", path: "src"}
	u, err := url.Parse(ascSearchURL("/o/r", q, map[string]string{"lang": "", "path": "docs"}))
	require.NoError(t, err)
	assert.Equal(t, "/o/r/asc/search", u.Path)
	v := u.Query()
	assert.Equal(t, "a b&c", v.Get("q"))
	assert.Equal(t, "regex", v.Get("match"))
	assert.Equal(t, "1", v.Get("case"))
	assert.Equal(t, "stack", v.Get("scope"))
	assert.Equal(t, "7", v.Get("pull"))
	assert.Equal(t, "1", v.Get("diff"))
	assert.Equal(t, "docs", v.Get("path"))
	assert.False(t, v.Has("lang"))

	sym := ascSearchQuery{mode: "symbol", text: "x", match: "regex", caseSensitive: true, scope: "version"}
	v, _ = url.ParseQuery(strings.SplitN(ascSearchURL("/o/r", sym, nil), "?", 2)[1])
	assert.False(t, v.Has("match"))
	assert.False(t, v.Has("case"))

	assert.Equal(t, "version", ascSearchScopeOK(""))
	assert.Equal(t, "version", ascSearchScopeOK("x"))
	assert.Equal(t, "branches", ascSearchScopeOK("branches"))
}
