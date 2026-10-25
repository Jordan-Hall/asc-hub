// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	gocontext "context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	code_indexer "forgejo.org/modules/indexer/code"
	"forgejo.org/modules/indexer/code/asc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAscExploreCodeRRF(t *testing.T) {
	// Two repositories: ranks interleave, so each list's best hit comes before any second hit.
	got := AscRRF([][]string{{"a1", "a2", "a3"}, {"b1", "b2"}})
	assert.Equal(t, []string{"a1", "b1", "a2", "b2", "a3"}, got)

	// The same call twice gives the same order (deterministic ties).
	assert.Equal(t, got, AscRRF([][]string{{"a1", "a2", "a3"}, {"b1", "b2"}}))

	assert.Empty(t, AscRRF[string](nil))
	assert.Equal(t, []string{"x"}, AscRRF([][]string{nil, {"x"}}))

	big := make([]string, ascExploreMaxHits+10)
	assert.Len(t, AscRRF([][]string{big}), ascExploreMaxHits)
}

func TestAscExploreCodeFanOut(t *testing.T) {
	res := func(id int64) []*code_indexer.Result { return []*code_indexer.Result{{RepoID: id}} }

	t.Run("a refused or failing repository does not stop the others", func(t *testing.T) {
		out, err := AscFanOut(t.Context(), []int64{1, 2, 3}, func(_ gocontext.Context, id int64) ([]*code_indexer.Result, []*code_indexer.SearchResultLanguages, error) {
			if id == 2 {
				return nil, nil, errors.New("boom")
			}
			return res(id), []*code_indexer.SearchResultLanguages{{Language: "Go", Count: 1}}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, out.Failed)
		assert.Nil(t, out.Lists[1])
		require.Len(t, out.Languages, 1)
		assert.Equal(t, 2, out.Languages[0].Count)
		assert.Equal(t, []int64{1, 3}, []int64{AscRRF(out.Lists)[0].RepoID, AscRRF(out.Lists)[1].RepoID})
	})

	t.Run("all failing is an error", func(t *testing.T) {
		_, err := AscFanOut(t.Context(), []int64{1, 2}, func(gocontext.Context, int64) ([]*code_indexer.Result, []*code_indexer.SearchResultLanguages, error) {
			return nil, nil, errors.New("boom")
		})
		require.Error(t, err)
	})

	t.Run("a missing credential aborts the whole search", func(t *testing.T) {
		_, err := AscFanOut(t.Context(), []int64{1, 2, 3}, func(_ gocontext.Context, id int64) ([]*code_indexer.Result, []*code_indexer.SearchResultLanguages, error) {
			if id == 3 {
				return nil, nil, ascclient.ErrNoCredential
			}
			return res(id), nil, nil
		})
		require.ErrorIs(t, err, ascclient.ErrNoCredential)
	})

	t.Run("concurrency is bounded", func(t *testing.T) {
		var cur, peak atomic.Int32
		ids := make([]int64, 40)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		_, err := AscFanOut(t.Context(), ids, func(_ gocontext.Context, _ int64) ([]*code_indexer.Result, []*code_indexer.SearchResultLanguages, error) {
			n := cur.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			cur.Add(-1)
			return nil, nil, nil
		})
		require.NoError(t, err)
		assert.LessOrEqual(t, peak.Load(), int32(ascExploreConcurrency))
	})

	t.Run("too many repositories", func(t *testing.T) {
		_, err := AscFanOut(t.Context(), make([]int64, asc.MaxRepos+1), nil)
		require.Error(t, err)
	})
}

type exStream struct {
	grpc.ClientStream
	msgs []*ascv1.SearchResponse
}

func (s *exStream) Recv() (*ascv1.SearchResponse, error) {
	if len(s.msgs) == 0 {
		return nil, io.EOF
	}
	m := s.msgs[0]
	s.msgs = s.msgs[1:]
	return m, nil
}

type exAPI struct {
	ascv1.CodeIntelClient
	byRepo map[string]*ascv1.SearchResponse
}

func (a *exAPI) Search(_ gocontext.Context, in *ascv1.SearchRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[ascv1.SearchResponse], error) {
	r, ok := a.byRepo[in.GetScope().GetRepo().GetName()]
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "no")
	}
	return &exStream{msgs: []*ascv1.SearchResponse{r}}, nil
}

func exResolver(_ gocontext.Context, id int64) (string, string, error) {
	return fmt.Sprintf("o/r%d", id), "master", nil
}

// The merged page shows the server-rendered text only: a redacted line is empty, so the AWS key of the
// fixture is absent from the formatted result of the whole fan-out.
func TestAscExploreCodeRedactedLineIsAbsent(t *testing.T) {
	const key = "AKIAIOSFODNN7EXAMPLE"
	api := &exAPI{byRepo: map[string]*ascv1.SearchResponse{
		"o/r1": {Files: []*ascv1.FileHit{{
			Path: []byte("conf.go"), Lang: "Go", Redacted: true,
			Matches: []*ascv1.LineMatch{{Line: 7}}, // text and cols removed by the server
		}}},
		"o/r2": {Files: []*ascv1.FileHit{{
			Path: []byte("ok.go"), Lang: "Go",
			Matches: []*ascv1.LineMatch{{Line: 1, Cols: []uint32{0}, Text: []byte("token := 1")}},
		}}},
	}}
	idx := asc.NewIndexer(api, exResolver)
	ctx, err := ascclient.WithUserToken(t.Context(), "user-token")
	require.NoError(t, err)

	out, err := AscFanOut(ctx, []int64{1, 2, 3}, NewAscRepoSearch(idx, "token", code_indexer.SearchModeExact, "", ""))
	require.NoError(t, err)
	merged := AscRRF(out.Lists)
	require.Len(t, merged, 2) // repository 3 is refused by asc and adds nothing
	assert.Equal(t, 0, out.Failed)
	assert.EqualValues(t, 1, merged[0].RepoID)
	assert.EqualValues(t, 2, merged[1].RepoID)
	for _, r := range merged {
		for _, l := range r.Lines {
			assert.NotContains(t, string(l.FormattedContent), "AKIA")
		}
	}
	assert.NotContains(t, fmt.Sprint(merged[0].Lines), key)
}

func TestAscExploreCodeToken(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/explore/code", nil)
	assert.Empty(t, ascExploreToken(r)) // a browser session alone has no token
	r.Header.Set("Authorization", "Bearer abc")
	assert.Equal(t, "abc", ascExploreToken(r))
	r.Header.Set("Authorization", "Basic dXNlcjpwdw==")
	assert.Equal(t, "pw", ascExploreToken(r))
}
