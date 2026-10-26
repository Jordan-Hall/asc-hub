// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func names2(r *TextResult) []string {
	var out []string
	for _, x := range r.Repos {
		out = append(out, x.Repo)
	}
	return out
}

func TestTextCross(t *testing.T) {
	const key = "AKIAIOSFODNN7EXAMPLE"
	f := &fakeAsc{crossSearch: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
		return []*ascv1.CrossSearchResponse{
			{
				Repos: []*ascv1.RepoHits{
					{Repo: ref("o/r2"), Rev: "main", Scope: &ascv1.ResolvedScope{Coverage: 0.5, Changeset: []byte{1, 2}}, Files: []*ascv1.FileHit{hit("b.go", "token \x1b[31m := 1")}},
					// asc names a repository nobody asked for; it must not be shown.
					{Repo: ref("secret/private"), Files: []*ascv1.FileHit{hit("leak.go", "token leaked")}},
					{Repo: ref("o/r1"), Files: []*ascv1.FileHit{
						hit("a.go", "token"),
						{Path: []byte("conf.env"), Redacted: true, Matches: []*ascv1.LineMatch{{Line: 1, Text: []byte("K=" + key)}}},
					}},
				},
				SearchedRepos: 2, UnindexedRepos: 1, EstTokens: 10,
			},
			{
				Repos:         []*ascv1.RepoHits{{Repo: ref("o/r2"), Files: []*ascv1.FileHit{hit("c.go", "token again")}}},
				Truncated:     true,
				NextPageToken: "tok-2",
				EstTokens:     5,
			},
		}, nil
	}}
	svc := startFake(t, f)

	res, err := svc.Text(sys(t), reposN(2), TextQuery{
		Pattern: "token", Mode: ascv1.SearchMode_SEARCH_MODE_WORD, CaseSensitive: true, Context: 2,
		Paths: []string{"src"}, Exclude: []string{"vendor"}, Langs: []string{"Go"}, Limit: 30,
	})
	require.NoError(t, err)

	require.Len(t, f.cross, 1)
	req := f.cross[0]
	assert.Equal(t, []string{"o/r1", "o/r2"}, req.GetTarget().GetRepos())
	assert.Nil(t, req.GetTarget().GetScope(), "the repositories are named, never an instance scope")
	assert.Equal(t, "token", req.GetPattern())
	assert.Equal(t, ascv1.SearchMode_SEARCH_MODE_WORD, req.GetMode())
	assert.True(t, req.GetCaseSensitive())
	assert.EqualValues(t, 2, req.GetContextLines())
	assert.Equal(t, []string{"src"}, req.GetPaths())
	assert.Equal(t, []string{"vendor"}, req.GetExcludePaths())
	assert.Equal(t, []string{"Go"}, req.GetLangs())
	assert.EqualValues(t, 30, req.GetBudget().GetMaxResults())
	assert.Equal(t, []string{"[Bearer " + testInternalToken + "]"}, f.auth, "a page search carries the internal token")

	// Repositories come back in the order of the request, files of one repository merged.
	assert.Equal(t, []string{"o/r1", "o/r2"}, names2(res))
	assert.Equal(t, 4, res.Files)
	assert.True(t, res.Truncated)
	assert.Equal(t, "tok-2", res.NextPageToken)
	assert.Equal(t, 2, res.Searched)
	assert.Equal(t, 1, res.Unindexed)
	assert.EqualValues(t, 15, res.EstTokens)
	assert.False(t, res.Fallback)
	r2 := res.Repos[1]
	require.Len(t, r2.Files, 2)
	assert.Equal(t, "token [31m := 1", r2.Files[0].Matches[0].Text, "control characters are gone")
	assert.NotContains(t, r2.Files[0].Matches[0].Text, "\x1b")
	assert.Equal(t, "0102", r2.Scope.Changeset)

	// A foreign repository and a redacted line never reach the result.
	all := fmt.Sprintf("%+v", res)
	assert.NotContains(t, all, "leak")
	assert.NotContains(t, all, "secret/private")
	assert.NotContains(t, all, key)
	redacted := res.Repos[0].Files[1]
	assert.True(t, redacted.Redacted)
	assert.Empty(t, redacted.Matches[0].Text)
}

func TestTextCrossPagingAndLimit(t *testing.T) {
	f := &fakeAsc{crossSearch: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
		var hits []*ascv1.RepoHits
		for _, n := range req.GetTarget().GetRepos() {
			hits = append(hits, &ascv1.RepoHits{Repo: ref(n), Files: []*ascv1.FileHit{hit("a.go", "x"), hit("b.go", "x"), hit("c.go", "x")}})
		}
		return []*ascv1.CrossSearchResponse{{Repos: hits, NextPageToken: req.GetPageToken() + "n"}}, nil
	}}
	svc := startFake(t, f)

	res, err := svc.Text(sys(t), reposN(3), TextQuery{Pattern: "xyz", PageToken: "p1", Limit: 4})
	require.NoError(t, err)
	assert.Equal(t, "p1n", res.NextPageToken, "the token of one call is passed through")
	assert.Equal(t, "p1", f.cross[0].GetPageToken())
	assert.Equal(t, 4, res.Files, "the limit holds over repositories")
	assert.True(t, res.Truncated)
	assert.Len(t, res.Repos, 2)

	// More than 64 repositories are searched in chunks, and then there is no page token.
	f.cross = nil
	res, err = svc.Text(sys(t), reposN(130), TextQuery{Pattern: "xyz", Limit: 200})
	require.NoError(t, err)
	assert.Len(t, f.cross, 3)
	for _, c := range f.cross {
		assert.LessOrEqual(t, len(c.GetTarget().GetRepos()), 64)
	}
	assert.Empty(t, res.NextPageToken)
	assert.Len(t, res.Repos, 67, "200 hits over 3 files each")
	assert.True(t, res.Truncated)
	assert.Equal(t, "o/r1", res.Repos[0].Repo)

	_, err = svc.Text(sys(t), reposN(130), TextQuery{Pattern: "xyz", PageToken: "p"})
	require.ErrorIs(t, err, ErrInput)
	_, err = svc.Text(sys(t), reposN(MaxRepos+1), TextQuery{Pattern: "xyz"})
	require.ErrorIs(t, err, ErrInput)
}

func TestTextChecks(t *testing.T) {
	f := &fakeAsc{crossSearch: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, nil }}
	svc := startFake(t, f)
	for name, q := range map[string]TextQuery{
		"short":      {Pattern: "ab"},
		"long":       {Pattern: strings.Repeat("a", MaxPattern+1)},
		"control":    {Pattern: "ab\ncd"},
		"bidi":       {Pattern: "abc‮"},
		"context":    {Pattern: "abc", Context: MaxContext + 1},
		"paths":      {Pattern: "abc", Paths: make([]string, MaxPaths+1)},
		"dotdot":     {Pattern: "abc", Paths: []string{"../x"}},
		"empty path": {Pattern: "abc", Exclude: []string{""}},
		"lang":       {Pattern: "abc", Langs: []string{"x;y"}},
		"langs":      {Pattern: "abc", Langs: make([]string, MaxLangs+1)},
		"token":      {Pattern: "abc", PageToken: "a b"},
	} {
		_, err := svc.Text(sys(t), reposN(1), q)
		require.ErrorIs(t, err, ErrInput, name)
	}
	assert.Empty(t, f.cross, "nothing bad is sent to asc")

	res, err := svc.Text(sys(t), nil, TextQuery{Pattern: "abc"})
	require.NoError(t, err)
	assert.Empty(t, res.Repos)
	assert.Empty(t, f.cross, "no repository, no call")
}

func TestTextErrors(t *testing.T) {
	t.Run("a refused search adds nothing", func(t *testing.T) {
		f := &fakeAsc{crossSearch: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "private/repo")
		}}
		res, err := startFake(t, f).Text(sys(t), reposN(2), TextQuery{Pattern: "abc"})
		require.NoError(t, err)
		assert.Empty(t, res.Repos)
		assert.Zero(t, res.Failed, "a refusal is not counted, so a search cannot tell that a repository exists")
	})

	t.Run("a failing search is an error", func(t *testing.T) {
		f := &fakeAsc{crossSearch: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
			return nil, status.Error(codes.Internal, "boom /srv/secret/path")
		}}
		_, err := startFake(t, f).Text(sys(t), reposN(2), TextQuery{Pattern: "abc"})
		require.Error(t, err)
		assert.Equal(t, KindFailed, KindOf(err))
	})

	t.Run("one failing chunk does not hide the others", func(t *testing.T) {
		f := &fakeAsc{crossSearch: func(req *ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
			if len(req.GetTarget().GetRepos()) < 64 {
				return nil, status.Error(codes.Internal, "boom")
			}
			return []*ascv1.CrossSearchResponse{{Repos: []*ascv1.RepoHits{{Repo: ref("o/r1"), Files: []*ascv1.FileHit{hit("a.go", "x")}}}}}, nil
		}}
		res, err := startFake(t, f).Text(sys(t), reposN(70), TextQuery{Pattern: "abc"})
		require.NoError(t, err)
		assert.Equal(t, 6, res.Failed)
		assert.Equal(t, []string{"o/r1"}, names2(res))
	})

	t.Run("too broad", func(t *testing.T) {
		f := &fakeAsc{crossSearch: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) {
			st, _ := status.New(codes.InvalidArgument, "x").WithDetails(&ascv1.ErrorDetail{Code: "SEARCH_TOO_BROAD"})
			return nil, st.Err()
		}}
		_, err := startFake(t, f).Text(sys(t), reposN(1), TextQuery{Pattern: "abc"})
		assert.Equal(t, KindTooBroad, KindOf(err))
	})

	t.Run("no credential", func(t *testing.T) {
		f := &fakeAsc{crossSearch: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, nil }}
		_, err := startFake(t, f).Text(t.Context(), reposN(1), TextQuery{Pattern: "abc"})
		require.ErrorIs(t, err, ascclient.ErrNoCredential)
		assert.Empty(t, f.cross)
	})
}

func TestTextUserToken(t *testing.T) {
	f := &fakeAsc{crossSearch: func(*ascv1.CrossSearchRequest) ([]*ascv1.CrossSearchResponse, error) { return nil, nil }}
	_, err := startFake(t, f).Text(userCtx(t, "user-token-7"), reposN(1), TextQuery{Pattern: "abc"})
	require.NoError(t, err)
	assert.Equal(t, []string{"[Bearer user-token-7]"}, f.auth, "the API forwards the caller's own token")
}

func TestTextFallback(t *testing.T) {
	f := &fakeAsc{search: func(req *ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
		switch req.GetScope().GetRepo().GetName() {
		case "o/r1":
			return []*ascv1.SearchResponse{
				{Files: []*ascv1.FileHit{hit("a.go", "needle")}, Scope: &ascv1.ResolvedScope{Coverage: 1}},
				{Files: []*ascv1.FileHit{hit("b.go", "needle two")}, Truncated: true, EstTokens: 3},
			}, nil
		case "o/r2":
			return nil, status.Error(codes.NotFound, "unknown")
		case "o/r3":
			return nil, status.Error(codes.Internal, "boom")
		case "o/r4":
			return []*ascv1.SearchResponse{{Files: []*ascv1.FileHit{hit("d.go", "needle")}, Scope: &ascv1.ResolvedScope{Coverage: 0}}}, nil
		}
		return nil, nil
	}}
	svc := startFake(t, f) // CrossSearch answers Unimplemented

	res, err := svc.Text(sys(t), reposN(4), TextQuery{Pattern: "needle", Paths: []string{"src"}, Langs: []string{"Go"}, Context: 1})
	require.NoError(t, err)
	assert.True(t, res.Fallback)
	assert.Equal(t, []string{"o/r1", "o/r4"}, names2(res))
	assert.Equal(t, 1, res.Failed, "o/r3 failed; o/r2 was refused and counts nowhere")
	assert.Equal(t, 2, res.Searched)
	assert.Equal(t, 1, res.Unindexed)
	assert.True(t, res.Truncated)
	assert.Len(t, res.Repos[0].Files, 2)
	require.Len(t, f.srch, 4)
	for _, r := range f.srch {
		assert.Equal(t, "refs/heads/main", r.GetScope().GetRev())
		assert.Equal(t, []string{"src"}, r.GetScope().GetPaths())
		assert.Equal(t, []string{"Go"}, r.GetScope().GetLangs())
		assert.EqualValues(t, 1, r.GetContextLines())
	}

	_, err = svc.Text(sys(t), reposN(2), TextQuery{Pattern: "needle", PageToken: "p"})
	require.ErrorIs(t, err, ErrInput, "the fallback cannot continue a search")

	// Every repository failing is an error.
	f.search = func(*ascv1.SearchRequest) ([]*ascv1.SearchResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	_, err = svc.Text(sys(t), reposN(2), TextQuery{Pattern: "needle"})
	assert.Equal(t, KindUnavailable, KindOf(err))

	// A missing credential stops the whole fallback.
	_, err = svc.Text(t.Context(), reposN(2), TextQuery{Pattern: "needle"})
	require.ErrorIs(t, err, ascclient.ErrNoCredential)
}

func TestFanOutIsBounded(t *testing.T) {
	var cur, peak atomic.Int32
	parallel(40, 5, func(int) {
		n := cur.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(2 * time.Millisecond)
		cur.Add(-1)
	})
	assert.LessOrEqual(t, peak.Load(), int32(5))
	assert.Positive(t, peak.Load())
}
