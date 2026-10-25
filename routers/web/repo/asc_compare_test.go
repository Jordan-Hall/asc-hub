// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeBadgeServer struct {
	ascv1.UnimplementedChangesServer
	ascv1.UnimplementedRepoServer
	mu       sync.Mutex
	overlaps func(*ascv1.OverlapsRequest) (*ascv1.OverlapsResponse, error)
	merge    func(*ascv1.MergeabilityRequest) (*ascv1.MergeabilityResponse, error)
	compare  func(*ascv1.CompareRequest) (*ascv1.CompareResponse, error)
	calls    int
	lastOv   *ascv1.OverlapsRequest
	lastCmp  *ascv1.CompareRequest
}

func (f *fakeBadgeServer) Overlaps(_ gocontext.Context, r *ascv1.OverlapsRequest) (*ascv1.OverlapsResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastOv = r
	f.mu.Unlock()
	return f.overlaps(r)
}

func (f *fakeBadgeServer) Mergeability(_ gocontext.Context, r *ascv1.MergeabilityRequest) (*ascv1.MergeabilityResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.merge(r)
}

func (f *fakeBadgeServer) Compare(_ gocontext.Context, r *ascv1.CompareRequest) (*ascv1.CompareResponse, error) {
	f.mu.Lock()
	f.calls++
	f.lastCmp = r
	f.mu.Unlock()
	return f.compare(r)
}

func (f *fakeBadgeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func startFakeBadges(t *testing.T) *fakeBadgeServer {
	t.Helper()
	f := &fakeBadgeServer{}
	srv := grpc.NewServer()
	ascv1.RegisterChangesServer(srv, f)
	ascv1.RegisterRepoServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return f
}

func ascTestChange(c byte) string { return strings.Repeat(string(rune(c)), 32) }

func TestAscOverlapChangeIdRoundTrip(t *testing.T) {
	for _, s := range []string{ascTestChange('k'), ascTestChange('z'), "kzlymxnwovpuotqsrrsqtpuovnwmxlyk"} {
		raw := ascChangeBytes(s)
		require.Len(t, raw, 16)
		assert.Equal(t, s, ascChangeText(raw))
	}
	assert.Nil(t, ascChangeBytes("short"))
	assert.Nil(t, ascChangeBytes(strings.Repeat("a", 32)))
	assert.Equal(t, "", ascChangeText([]byte{1}))
}

func badgePull(t *testing.T) (*issues_model.PullRequest, string) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	change := ascTestChange('l')
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), pull, change, 0))
	require.NoError(t, pull.LoadIssue(t.Context()))
	require.NoError(t, pull.LoadBaseRepo(t.Context()))
	return pull, change
}

func runBadges(t *testing.T, pull *issues_model.PullRequest) *context.Context {
	t.Helper()
	ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/2")
	contexttest.LoadRepo(t, ctx, pull.BaseRepoID)
	prepareAscBadges(ctx, pull)
	return ctx
}

func renderBadges(t *testing.T, ctx *context.Context) string {
	t.Helper()
	ctx2, resp := contexttest.MockContext(t, "user2/repo1/pulls/2")
	ctx2.Render = templates.HTMLRenderer()
	ctx2.Data["AscBadges"] = ctx.Data["AscBadges"]
	ctx2.HTML(http.StatusOK, "repo/pulls/asc_badges")
	return resp.Body.String()
}

func TestAscOverlapBadges(t *testing.T) {
	pull, change := badgePull(t)
	own := ascChangeBytes(change)
	other := ascChangeBytes(ascTestChange('m'))
	f := startFakeBadges(t)
	f.overlaps = func(r *ascv1.OverlapsRequest) (*ascv1.OverlapsResponse, error) {
		return &ascv1.OverlapsResponse{Overlaps: []*ascv1.Overlap{
			{Change: own, Paths: [][]byte{[]byte("self.go")}}, // never shown: a change does not overlap itself
			{Change: other, Paths: [][]byte{[]byte("a/<b>.go"), []byte("c.go")}, Review: &ascv1.ReviewState{State: "<open>", Approvals: 2}},
		}}, nil
	}
	f.merge = func(r *ascv1.MergeabilityRequest) (*ascv1.MergeabilityResponse, error) {
		return &ascv1.MergeabilityResponse{Results: []*ascv1.Mergeable{{
			Change: own, State: ascv1.Mergeable_STATE_CONFLICTS,
			Conflicts: []*ascv1.Conflict{{Path: []byte("x.go")}},
		}}}, nil
	}

	t.Run("shows overlaps and conflicts from the pull request's own change", func(t *testing.T) {
		ctx := runBadges(t, pull)
		b, ok := ctx.Data["AscBadges"].(*AscBadges)
		require.True(t, ok)
		assert.Equal(t, own, f.lastOv.GetChange())
		assert.Equal(t, "user2/repo1", f.lastOv.GetRepo().GetName())
		assert.Equal(t, pull.BaseBranch, f.lastOv.GetTarget())
		require.Len(t, b.Overlaps, 1)
		assert.Equal(t, "mmmmmmmm", b.Overlaps[0].Change)
		assert.Equal(t, []string{"a/<b>.go", "c.go"}, b.Overlaps[0].Paths)
		assert.Equal(t, uint32(2), b.Overlaps[0].Approvals)
		require.NotNil(t, b.Merge)
		assert.Equal(t, AscMergeConflicts, b.Merge.State)
		assert.Equal(t, []string{"x.go"}, b.Merge.Conflicts)
	})

	t.Run("the template escapes asc text", func(t *testing.T) {
		out := renderBadges(t, runBadges(t, pull))
		assert.Contains(t, out, "a/&lt;b&gt;.go")
		assert.NotContains(t, out, "<b>.go")
		assert.Contains(t, out, "&lt;open&gt;")
		assert.Contains(t, out, "x.go")
	})

	t.Run("a pull request that is not stacked, merged or off asc never reaches asc", func(t *testing.T) {
		before := f.count()
		notStacked := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
		assert.Nil(t, runBadges(t, notStacked).Data["AscBadges"])
		merged := *pull
		merged.HasMerged = true
		assert.Nil(t, runBadges(t, &merged).Data["AscBadges"])
		setting.Asc.Enabled = false
		assert.Nil(t, runBadges(t, pull).Data["AscBadges"])
		setting.Asc.Enabled = true
		assert.Equal(t, before, f.count())
	})

	t.Run("asc failure leaves the badges out", func(t *testing.T) {
		f.overlaps = func(*ascv1.OverlapsRequest) (*ascv1.OverlapsResponse, error) {
			return nil, status.Error(codes.Internal, "secret detail")
		}
		assert.Nil(t, runBadges(t, pull).Data["AscBadges"])
	})
}

func TestAscOverlapRefusesBadAnswers(t *testing.T) {
	own := ascChangeBytes(ascTestChange('l'))
	other := ascChangeBytes(ascTestChange('m'))
	good := func() []*ascv1.Mergeable {
		return []*ascv1.Mergeable{{Change: own, State: ascv1.Mergeable_STATE_CLEAN_MERGE}}
	}

	_, err := ascVerifyMergeability(own, good())
	require.NoError(t, err)
	for name, in := range map[string][]*ascv1.Mergeable{
		"empty":                  nil,
		"two":                    append(good(), good()...),
		"other change":           {{Change: other, State: ascv1.Mergeable_STATE_CLEAN_MERGE}},
		"unknown state":          {{Change: own, State: ascv1.Mergeable_State(99)}},
		"conflicts without path": {{Change: own, State: ascv1.Mergeable_STATE_CONFLICTS}},
		"nul in path":            {{Change: own, State: ascv1.Mergeable_STATE_CONFLICTS, Conflicts: []*ascv1.Conflict{{Path: []byte("a\x00b")}}}},
		"bad utf8":               {{Change: own, State: ascv1.Mergeable_STATE_CONFLICTS, Conflicts: []*ascv1.Conflict{{Path: []byte{0xff}}}}},
	} {
		_, err := ascVerifyMergeability(own, in)
		assert.ErrorIs(t, err, errAscBadgeAnswer, name)
	}

	for name, in := range map[string][]*ascv1.Overlap{
		"short id": {{Change: []byte{1}}},
		"bad path": {{Change: other, Paths: [][]byte{{}}}},
		"nul path": {{Change: other, Paths: [][]byte{[]byte("a\x00")}}},
	} {
		err := ascVerifyOverlaps(t.Context(), 1, "/r", own, in, &AscBadges{})
		assert.ErrorIs(t, err, errAscBadgeAnswer, name)
	}

	// Bounds: shown lists are capped, the rest is counted.
	var many []*ascv1.Overlap
	for i := 0; i < ascBadgeMaxOverlaps+5; i++ {
		id := make([]byte, 16)
		id[0], id[15] = byte(i+1), 0xAB
		many = append(many, &ascv1.Overlap{Change: id})
	}
	out := &AscBadges{}
	require.NoError(t, ascVerifyOverlaps(t.Context(), 1, "/r", own, many, out))
	assert.Len(t, out.Overlaps, ascBadgeMaxOverlaps)
	assert.Equal(t, 5, out.MoreOverlaps)
}

func TestAscOverlapNoLinkFromAsc(t *testing.T) {
	pull, change := badgePull(t)
	own := ascChangeBytes(change)
	other := ascChangeBytes(ascTestChange('m'))
	f := startFakeBadges(t)
	f.overlaps = func(*ascv1.OverlapsRequest) (*ascv1.OverlapsResponse, error) {
		return &ascv1.OverlapsResponse{Overlaps: []*ascv1.Overlap{{Change: other, Review: &ascv1.ReviewState{Url: "javascript:alert(1)", State: "open"}}}}, nil
	}
	f.merge = func(*ascv1.MergeabilityRequest) (*ascv1.MergeabilityResponse, error) {
		return &ascv1.MergeabilityResponse{Results: []*ascv1.Mergeable{{Change: own, State: ascv1.Mergeable_STATE_DISJOINT}}}, nil
	}
	ctx := runBadges(t, pull)
	b := ctx.Data["AscBadges"].(*AscBadges)
	require.Len(t, b.Overlaps, 1)
	assert.Empty(t, b.Overlaps[0].PullLink, "no open pull request of that change: no link")
	assert.NotContains(t, renderBadges(t, ctx), "javascript:")
}

func getAscCompare(t *testing.T, query string) (int, string, *context.Context) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/compare"+query)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	AscCompare(ctx)
	return resp.Code, resp.Body.String(), ctx
}

func TestAscOverlapCompareView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	f := startFakeBadges(t)
	sha := make([]byte, 20)
	for i := range sha {
		sha[i] = 0xAB
	}
	f.compare = func(r *ascv1.CompareRequest) (*ascv1.CompareResponse, error) {
		return &ascv1.CompareResponse{
			MergeBase: sha, Ahead: 1, Behind: 0,
			AheadEntries: []*ascv1.LogEntry{{Git: sha, Message: []byte("<script>x</script>\nbody"), Author: &ascv1.Signature{Name: []byte("Ann")}}},
			Files:        []*ascv1.PathChange{{Path: []byte("a.go"), Status: "M"}, {Path: []byte("b.go"), Status: "<"}},
		}, nil
	}

	t.Run("renders a checked answer", func(t *testing.T) {
		code, body, _ := getAscCompare(t, "?a=main&b=feature&three_dot=1")
		require.Equal(t, http.StatusOK, code, body)
		assert.Equal(t, "user2/repo1", f.lastCmp.GetRepo().GetName())
		assert.Equal(t, "main", f.lastCmp.GetA())
		assert.Equal(t, "feature", f.lastCmp.GetB())
		assert.True(t, f.lastCmp.GetThreeDot())
		assert.Equal(t, uint32(ascCompareLimit), f.lastCmp.GetLimit())
		assert.Contains(t, body, "&lt;script&gt;x&lt;/script&gt;")
		assert.NotContains(t, body, "<script>x")
		assert.Contains(t, body, "a.go")
		assert.Contains(t, body, strings.Repeat("ab", 20))
	})

	t.Run("bad revisions never reach asc", func(t *testing.T) {
		before := f.count()
		for _, q := range []string{"?a=--output%3Dx&b=main", "?a=a%00b&b=main", "?a=x+y&b=main", "?a=" + strings.Repeat("a", 300) + "&b=main"} {
			code, _, _ := getAscCompare(t, q)
			assert.Equal(t, http.StatusBadRequest, code, q)
		}
		code, _, _ := getAscCompare(t, "")
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, before, f.count())
	})

	t.Run("a bad answer is refused and the status text is not shown", func(t *testing.T) {
		f.compare = func(*ascv1.CompareRequest) (*ascv1.CompareResponse, error) {
			return &ascv1.CompareResponse{Ahead: 1, AheadEntries: []*ascv1.LogEntry{{Git: []byte{1}}}}, nil
		}
		code, _, ctx := getAscCompare(t, "?a=x&b=y")
		assert.Equal(t, http.StatusBadGateway, code)
		assert.Equal(t, "repo.asc_compare.error.bad_answer", ctx.Data["AscCompareError"])

		f.compare = func(*ascv1.CompareRequest) (*ascv1.CompareResponse, error) {
			return nil, status.Error(codes.Internal, "secret detail")
		}
		code, body, _ := getAscCompare(t, "?a=x&b=y")
		assert.Equal(t, http.StatusBadGateway, code)
		assert.NotContains(t, body, "secret detail")
	})

	t.Run("not served outside asc", func(t *testing.T) {
		before := f.count()
		setStorageBackend(t, 1, "git")
		defer setStorageBackend(t, 1, "asc")
		code, _, _ := getAscCompare(t, "?a=x&b=y")
		assert.Equal(t, http.StatusNotFound, code)
		assert.Equal(t, before, f.count())
	})
}

func TestAscOverlapCompareVerify(t *testing.T) {
	sha := make([]byte, 20)
	for name, r := range map[string]*ascv1.CompareResponse{
		"nil":              nil,
		"short merge base": {MergeBase: []byte{1}},
		"too many commits": {AheadEntries: make([]*ascv1.LogEntry, ascCompareLimit+1)},
		"nul path":         {Files: []*ascv1.PathChange{{Path: []byte("a\x00")}}},
		"empty path":       {Files: []*ascv1.PathChange{{}}},
		"too many files":   {Files: make([]*ascv1.PathChange, ascCompareMaxFiles+1)},
		"bad commit id":    {BehindEntries: []*ascv1.LogEntry{{Git: sha[:3]}}},
	} {
		_, err := ascCompareVerify(r)
		assert.ErrorIs(t, err, errAscBadgeAnswer, name)
	}
}
