// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"bytes"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func ascGID(n byte) []byte { return bytes.Repeat([]byte{n}, 20) }

func ascGRow(id byte, col uint32, to ...uint32) *ascv1.GraphRow {
	r := &ascv1.GraphRow{
		Entry:  &ascv1.LogEntry{Id: ascGID(id), Git: ascGID(id), Message: []byte("subject " + strconv.Itoa(int(id)) + "\n\nbody"), Author: &ascv1.Signature{Name: []byte("Ann"), Time: 1700000000}},
		Column: col, Overflow: col == ascGraphMaxLane,
	}
	for _, c := range to {
		r.Edges = append(r.Edges, &ascv1.GraphEdge{ToColumn: c, Parent: ascGID(id + 1)})
	}
	return r
}

func ascGMsg(next string, rows ...*ascv1.GraphRow) *ascv1.CommitGraphResponse {
	return &ascv1.CommitGraphResponse{Rows: rows, NextPageToken: next}
}

func TestAscGraphLanes(t *testing.T) {
	// m merges lane 1 into lane 0, a continues lane 0, b ends lane 1 into lane 0, base ends lane 0.
	msgs := []*ascv1.CommitGraphResponse{ascGMsg("NEXT_tok-1",
		ascGRow(1, 0, 0, 1), ascGRow(2, 0, 0), ascGRow(3, 1, 0), ascGRow(4, 0),
	)}
	rows, active, next, err := ascGraphBuild(msgs, nil)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, "NEXT_tok-1", next)
	assert.Empty(t, active, "base ends the last lane")

	x := func(c int) int { return c*ascGraphLaneWidth + ascGraphLaneWidth/2 }
	mid, bottom := ascGraphRowHeight/2, ascGraphRowHeight
	seg := func(x1, y1, x2, y2 int, col int) ascGraphSegment {
		return ascGraphSegment{x1, y1, x2, y2, ascGraphColor(col)}
	}
	// m: no lane above; edges to lanes 0 and 1.
	assert.ElementsMatch(t, []ascGraphSegment{seg(x(0), mid, x(0), bottom, 0), seg(x(0), mid, x(1), bottom, 1)}, rows[0].Segments)
	// a: lane 1 passes, lane 0 ends here and continues.
	assert.ElementsMatch(t, []ascGraphSegment{
		seg(x(1), 0, x(1), bottom, 1), seg(x(0), 0, x(0), mid, 0), seg(x(0), mid, x(0), bottom, 0),
	}, rows[1].Segments)
	// b sits in lane 1 and joins lane 0; lane 0 passes.
	assert.ElementsMatch(t, []ascGraphSegment{
		seg(x(0), 0, x(0), bottom, 0), seg(x(1), 0, x(1), mid, 1), seg(x(1), mid, x(0), bottom, 0),
	}, rows[2].Segments)
	// base: the line from above ends in the node.
	assert.Equal(t, []ascGraphSegment{seg(x(0), 0, x(0), mid, 0)}, rows[3].Segments)
	assert.Equal(t, "0101010101010101010101010101010101010101", rows[0].ID)
	assert.Equal(t, "subject 1", rows[0].Subject)
	assert.Equal(t, "Ann", rows[0].Author)
	assert.False(t, rows[0].When.IsZero())

	t.Run("a later page starts with the lanes the last page left open", func(t *testing.T) {
		rows, active, _, err := ascGraphBuild([]*ascv1.CommitGraphResponse{ascGMsg("", ascGRow(9, 2, 2))}, []int{0, 2, 5})
		require.NoError(t, err)
		assert.ElementsMatch(t, []ascGraphSegment{
			seg(x(0), 0, x(0), bottom, 0), seg(x(5), 0, x(5), bottom, 5),
			seg(x(2), 0, x(2), mid, 2), seg(x(2), mid, x(2), bottom, 2),
		}, rows[0].Segments)
		assert.Equal(t, []int{0, 2, 5}, active)
	})
}

func TestAscGraphDrawingIsBounded(t *testing.T) {
	// A row in the overflow column with 40 lines open and 40 parent lines: at most one segment per drawn lane.
	open := []int{}
	to := []uint32{}
	for i := range 40 {
		open = append(open, i)
		to = append(to, uint32(i+100))
	}
	rows, _, _, err := ascGraphBuild([]*ascv1.CommitGraphResponse{ascGMsg("", ascGRow(1, ascGraphMaxLane, to...))}, open)
	require.NoError(t, err)
	assert.True(t, rows[0].Overflow)
	assert.Equal(t, ascGraphOverflowColor, rows[0].NodeColor)
	assert.LessOrEqual(t, len(rows[0].Segments), 2*ascGraphMaxDrawnLanes)
	for _, s := range rows[0].Segments {
		for _, v := range []int{s.X1, s.X2} {
			assert.LessOrEqual(t, v, ascGraphMaxDrawnLanes*ascGraphLaneWidth)
		}
	}
	assert.LessOrEqual(t, ascGraphDrawnWidth(rows, nil), ascGraphMaxDrawnLanes*ascGraphLaneWidth)
}

func TestAscGraphRefusesBadAnswers(t *testing.T) {
	tooManyEdges := ascGRow(1, 0)
	for range ascGraphMaxEdges + 1 {
		tooManyEdges.Edges = append(tooManyEdges.Edges, &ascv1.GraphEdge{Parent: ascGID(2)})
	}
	var tooManyRows []*ascv1.GraphRow
	for range ascGraphPageRows + 1 {
		tooManyRows = append(tooManyRows, ascGRow(1, 0))
	}
	badID := ascGRow(1, 0)
	badID.Entry.Id = []byte("short")
	gitDiffers := ascGRow(1, 0)
	gitDiffers.Entry.Git = ascGID(7)
	noEntry := &ascv1.GraphRow{}
	farColumn := ascGRow(1, 0)
	farColumn.Column = ascGraphMaxLane + 1
	overflowFlag := ascGRow(1, 3)
	overflowFlag.Overflow = true
	badEdgeColumn := ascGRow(1, 0, ascGraphMaxLane+1)
	badEdgeParent := ascGRow(1, 0, 0)
	badEdgeParent.Edges[0].Parent = []byte{1}
	manyStats := ascGRow(1, 0)
	manyStats.Entry.Stats = &ascv1.DiffStat{Files: make([]*ascv1.FileStat, ascGraphMaxStatFiles+1)}

	cases := map[string][]*ascv1.CommitGraphResponse{
		"too many edges":         {ascGMsg("", tooManyEdges)},
		"too many rows":          {ascGMsg("", tooManyRows...)},
		"too many rows in parts": {ascGMsg("", tooManyRows[:60]...), ascGMsg("", tooManyRows[:60]...)},
		"bad id":                 {ascGMsg("", badID)},
		"git differs from id":    {ascGMsg("", gitDiffers)},
		"no entry":               {ascGMsg("", noEntry)},
		"column out of range":    {ascGMsg("", farColumn)},
		"overflow flag lies":     {ascGMsg("", overflowFlag)},
		"edge column":            {ascGMsg("", badEdgeColumn)},
		"edge parent":            {ascGMsg("", badEdgeParent)},
		"two tokens":             {ascGMsg("aaa", ascGRow(1, 0)), ascGMsg("bbb")},
		"token with a dot":       {ascGMsg("aa.bb")},
		"token with a space":     {ascGMsg("aa bb")},
		"token too long":         {ascGMsg(strings.Repeat("a", 8193))},
		"huge diffstat":          {ascGMsg("", manyStats)},
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := ascGraphBuild(msgs, nil)
			require.ErrorIs(t, err, errAscGraphBadAnswer)
		})
	}
}

func TestAscGraphTextIsPrintable(t *testing.T) {
	assert.Equal(t, "a b", ascGraphText("a\x00b", 10))
	assert.Equal(t, "ab...", ascGraphText("abcdef", 2))
	long := ascGraphText(strings.Repeat("é", 1000), ascGraphMaxSubjectRunes)
	assert.True(t, utf8.ValidString(long))
	assert.LessOrEqual(t, utf8.RuneCountInString(long), ascGraphMaxSubjectRunes+3)
	assert.True(t, utf8.ValidString(ascGraphText("\xff\xfe ok", 10)))
	assert.Equal(t, "first", ascGraphSubject([]byte("first\nsecond")))
}

func TestAscGraphRefLabels(t *testing.T) {
	r := ascGRow(1, 0)
	r.Refs = []string{"refs/heads/main", "refs/tags/v1"}
	for i := range 20 {
		r.Refs = append(r.Refs, "refs/heads/b"+strconv.Itoa(i))
	}
	rows, _, _, err := ascGraphBuild([]*ascv1.CommitGraphResponse{ascGMsg("", r)}, nil)
	require.NoError(t, err)
	assert.Equal(t, ascGraphRef{Name: "main"}, rows[0].Refs[0])
	assert.Equal(t, ascGraphRef{Name: "v1", IsTag: true}, rows[0].Refs[1])
	assert.Len(t, rows[0].Refs, ascGraphMaxRowRefs)
	assert.Equal(t, 22-ascGraphMaxRowRefs, rows[0].MoreRefs)
}

func TestAscGraphStatsAndReview(t *testing.T) {
	r := ascGRow(1, 0)
	r.Entry.Stats = &ascv1.DiffStat{Files: []*ascv1.FileStat{{Added: 3, Removed: 1}, {Added: 1 << 31, Removed: 1 << 31}, {Added: 1 << 31, Removed: 4}}}
	r.Review = &ascv1.ReviewState{Number: 7, State: "open\x07", Draft: true, Approvals: 2, Checks: "passing", Url: "javascript:alert(1)"}
	rows, _, _, err := ascGraphBuild([]*ascv1.CommitGraphResponse{ascGMsg("", r)}, nil)
	require.NoError(t, err)
	assert.True(t, rows[0].HasStats)
	assert.Equal(t, 3, rows[0].Files)
	assert.Equal(t, uint64(3)+2*(1<<31), rows[0].Added, "sums do not wrap")
	require.NotNil(t, rows[0].Review)
	assert.Equal(t, "open", rows[0].Review.State)

	plain := ascGRow(2, 0)
	rows, _, _, err = ascGraphBuild([]*ascv1.CommitGraphResponse{ascGMsg("", plain)}, nil)
	require.NoError(t, err)
	assert.False(t, rows[0].HasStats, "no stats means pending, not zero")
	assert.Nil(t, rows[0].Review)
}

func TestAscGraphPageToken(t *testing.T) {
	defer test.MockVariableValue(&setting.SecretKey, "unit-test-secret")()
	signed := ascGraphSignToken(1, "abc_-DEF")
	tok, ok := ascGraphVerifyToken(1, signed)
	assert.True(t, ok)
	assert.Equal(t, "abc_-DEF", tok)

	for name, bad := range map[string]string{
		"other repository": ascGraphSignToken(2, "abc_-DEF"),
		"tampered token":   "abc_-DEG" + signed[len("abc_-DEF"):],
		"no signature":     "abc_-DEF",
		"empty signature":  "abc_-DEF.",
		"only a signature": signed[len("abc_-DEF"):],
		"short signature":  "abc_-DEF.AAAA",
		"not base64":       "abc_-DEF.!!!!",
		"too long":         strings.Repeat("a", ascGraphMaxTokenBytes+1) + signed,
	} {
		_, ok := ascGraphVerifyToken(1, bad)
		assert.False(t, ok, name)
	}

	t.Run("without a secret key nothing is signed or accepted", func(t *testing.T) {
		defer test.MockVariableValue(&setting.SecretKey, "")()
		assert.Empty(t, ascGraphSignToken(1, "abc"))
		_, ok := ascGraphVerifyToken(1, signed)
		assert.False(t, ok)
	})
}

func TestAscGraphParseHelpers(t *testing.T) {
	revs, ok := ascGraphRevs("main  release/1.0 v2")
	assert.True(t, ok)
	assert.Equal(t, []string{"main", "release/1.0", "v2"}, revs)
	for _, bad := range []string{"--output=x", "a\x00b", strings.Repeat("a", ascGraphMaxRevBytes+1), strings.Repeat("a ", ascGraphMaxRevs+1), strings.Repeat("a", ascGraphMaxFormBytes+1), "\xff"} {
		_, ok := ascGraphRevs(bad)
		assert.False(t, ok, "%q", bad)
	}
	lanes, ok := ascGraphParseLanes("0,3,256")
	assert.True(t, ok)
	assert.Equal(t, []int{0, 3, 256}, lanes)
	for _, bad := range []string{"-1", "257", "a", "1,,2", "01", "1 ", strings.Repeat("1,", 300)} {
		_, ok := ascGraphParseLanes(bad)
		assert.False(t, ok, "%q", bad)
	}
}

// fakeGraphServer is an asc serve that answers CommitGraph.
type fakeGraphServer struct {
	ascv1.UnimplementedRepoServer
	mu      sync.Mutex
	reqs    []*ascv1.CommitGraphRequest
	auth    [][]string
	respond func(*ascv1.CommitGraphRequest) ([]*ascv1.CommitGraphResponse, error)
}

func (f *fakeGraphServer) CommitGraph(req *ascv1.CommitGraphRequest, stream grpc.ServerStreamingServer[ascv1.CommitGraphResponse]) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.auth = append(f.auth, md.Get("authorization"))
	f.mu.Unlock()
	msgs, err := f.respond(req)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		if err := stream.Send(m); err != nil {
			return err
		}
	}
	return nil
}

func startFakeGraph(t *testing.T, respond func(*ascv1.CommitGraphRequest) ([]*ascv1.CommitGraphResponse, error)) *fakeGraphServer {
	t.Helper()
	f := &fakeGraphServer{respond: respond}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return f
}

func (f *fakeGraphServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeGraphServer) last(t *testing.T) (*ascv1.CommitGraphRequest, []string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.NotEmpty(t, f.reqs)
	return f.reqs[len(f.reqs)-1], f.auth[len(f.auth)-1]
}

type ascGraphPage struct {
	status int
	body   string
	ctx    *context.Context
}

// getAscGraph runs the handler for repository 1 (user2/repo1) with the real templates.
func getAscGraph(t *testing.T, query string) ascGraphPage {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/graph"+query)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	AscGraph(ctx)
	return ascGraphPage{status: resp.Code, body: resp.Body.String(), ctx: ctx}
}

func TestAscGraphPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.SecretKey, "unit-test-secret")()
	setStorageBackend(t, 1, "asc")
	row1 := ascGRow(1, 0, 0)
	row1.Refs = []string{"refs/heads/main"}
	row1.Review = &ascv1.ReviewState{Number: 12, State: "open"}
	row2 := ascGRow(2, 0, 0)
	row2.Entry.Message = []byte("<script>alert(1)</script> & more")
	fake := startFakeGraph(t, func(req *ascv1.CommitGraphRequest) ([]*ascv1.CommitGraphResponse, error) {
		if req.GetPageToken() == "srv-token_1" {
			return []*ascv1.CommitGraphResponse{ascGMsg("", ascGRow(3, 0))}, nil
		}
		return []*ascv1.CommitGraphResponse{ascGMsg("srv-token_1", row1), ascGMsg("", row2)}, nil
	})

	t.Run("first page", func(t *testing.T) {
		page := getAscGraph(t, "?preset=all&stats=1&refs=main+dev&exclude=old")
		require.Equal(t, http.StatusOK, page.status, page.body)
		req, auth := fake.last(t)
		assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
		assert.Equal(t, ascv1.IdMode_ID_MODE_GIT, req.GetRepo().GetIdMode())
		assert.Equal(t, []string{"Bearer " + ascRPCTestToken}, auth)
		assert.True(t, req.GetLanes())
		assert.True(t, req.GetStats())
		assert.Equal(t, uint32(ascGraphPageRows), req.GetLimit())
		assert.Equal(t, ascv1.GraphPreset_GRAPH_PRESET_ALL, req.GetPreset())
		assert.Equal(t, []string{"main", "dev"}, req.GetInclude())
		assert.Equal(t, []string{"old"}, req.GetExclude())
		assert.Empty(t, req.GetPageToken())

		assert.Contains(t, page.body, "0101010101010101010101010101010101010101")
		assert.Contains(t, page.body, "subject 1")
		assert.Contains(t, page.body, "&lt;script&gt;alert(1)&lt;/script&gt; &amp; more")
		assert.NotContains(t, page.body, "<script>alert")
		assert.Contains(t, page.body, "/user2/repo1/commit/0202020202020202020202020202020202020202")
		assert.Contains(t, page.body, "#12")

		next, ok := page.ctx.Data["AscGraphNextURL"].(string)
		require.True(t, ok)
		u, err := url.Parse(next)
		require.NoError(t, err)
		assert.Equal(t, "/user2/repo1/asc/graph", u.Path)
		assert.Equal(t, "0", u.Query().Get("lanes"))
		assert.NotContains(t, next, "srv-token_1.srv-token_1")
		assert.Equal(t, "all", u.Query().Get("preset"))
		assert.Equal(t, "main dev", u.Query().Get("refs"))
		_, ok = ascGraphVerifyToken(1, u.Query().Get("page_token"))
		assert.True(t, ok)
	})

	t.Run("next page carries the verified token and lanes", func(t *testing.T) {
		first := getAscGraph(t, "")
		next := first.ctx.Data["AscGraphNextURL"].(string)
		u, err := url.Parse(next)
		require.NoError(t, err)
		page := getAscGraph(t, "?"+u.RawQuery)
		require.Equal(t, http.StatusOK, page.status, page.body)
		req, _ := fake.last(t)
		assert.Equal(t, "srv-token_1", req.GetPageToken())
		assert.Contains(t, page.body, "0303030303030303030303030303030303030303")
		assert.Nil(t, page.ctx.Data["AscGraphNextURL"])
	})

	t.Run("a forged or foreign token never reaches asc", func(t *testing.T) {
		before := fake.count()
		for _, tok := range []string{"srv-token_1", "srv-token_1.AAAA", ascGraphSignToken(2, "srv-token_1"), "x.y.z"} {
			page := getAscGraph(t, "?page_token="+url.QueryEscape(tok))
			assert.Equal(t, http.StatusBadRequest, page.status, tok)
			assert.Equal(t, "repo.asc_graph.error.token_gone", page.ctx.Data["AscGraphError"])
		}
		assert.Equal(t, before, fake.count())
	})

	t.Run("bad parameters never reach asc", func(t *testing.T) {
		before := fake.count()
		for _, q := range []string{"?preset=evil", "?refs=--output%3Dx", "?exclude=" + strings.Repeat("a+", 40), "?refs=a%00b"} {
			assert.Equal(t, http.StatusBadRequest, getAscGraph(t, q).status, q)
		}
		signed := ascGraphSignToken(1, "srv-token_1")
		assert.Equal(t, http.StatusBadRequest, getAscGraph(t, "?page_token="+url.QueryEscape(signed)+"&lanes=999").status)
		assert.Equal(t, before, fake.count())
	})

	t.Run("not served for a repository outside asc or with asc off", func(t *testing.T) {
		before := fake.count()
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.Equal(t, http.StatusNotFound, getAscGraph(t, "").status)
		setting.Asc.Enabled = true
		setStorageBackend(t, 1, "git")
		assert.Equal(t, http.StatusNotFound, getAscGraph(t, "").status)
		setStorageBackend(t, 1, "asc")
		assert.Equal(t, before, fake.count())
	})
}

func TestAscGraphPageErrorsAreGeneric(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	secret := "sqlite error at /var/lib/asc/asc.db password=hunter2"
	var answer func() ([]*ascv1.CommitGraphResponse, error)
	startFakeGraph(t, func(*ascv1.CommitGraphRequest) ([]*ascv1.CommitGraphResponse, error) { return answer() })

	for name, c := range map[string]struct {
		err  error
		code int
		key  string
	}{
		"not found":    {status.Error(codes.NotFound, secret), http.StatusNotFound, "repo.asc_graph.error.not_found"},
		"invalid":      {status.Error(codes.InvalidArgument, secret), http.StatusBadRequest, "repo.asc_graph.error.bad_request"},
		"token gone":   {status.Error(codes.FailedPrecondition, secret), http.StatusConflict, "repo.asc_graph.error.token_gone"},
		"too large":    {status.Error(codes.ResourceExhausted, secret), http.StatusRequestEntityTooLarge, "repo.asc_graph.error.too_large"},
		"internal":     {status.Error(codes.Internal, secret), http.StatusBadGateway, "repo.asc_graph.error.unavailable"},
		"unauthorised": {status.Error(codes.PermissionDenied, secret), http.StatusBadGateway, "repo.asc_graph.error.unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			answer = func() ([]*ascv1.CommitGraphResponse, error) { return nil, c.err }
			page := getAscGraph(t, "")
			assert.Equal(t, c.code, page.status)
			assert.Equal(t, c.key, page.ctx.Data["AscGraphError"])
			assert.NotContains(t, page.body, "hunter2")
			assert.NotContains(t, page.body, "sqlite")
			assert.Contains(t, page.body, c.key, "the page says what went wrong in the fixed words")
		})
	}

	t.Run("a bad answer fails the whole page", func(t *testing.T) {
		bad := ascGRow(2, 0)
		bad.Entry.Id = []byte("short")
		answer = func() ([]*ascv1.CommitGraphResponse, error) {
			return []*ascv1.CommitGraphResponse{ascGMsg("", ascGRow(1, 0), bad)}, nil
		}
		page := getAscGraph(t, "")
		assert.Equal(t, http.StatusBadGateway, page.status)
		assert.Equal(t, "repo.asc_graph.error.bad_answer", page.ctx.Data["AscGraphError"])
		assert.NotContains(t, page.body, "0101010101010101")
	})

	t.Run("an empty graph", func(t *testing.T) {
		answer = func() ([]*ascv1.CommitGraphResponse, error) { return nil, nil }
		page := getAscGraph(t, "")
		assert.Equal(t, http.StatusOK, page.status)
		assert.Contains(t, page.body, "repo.asc_graph.empty")
	})
}
