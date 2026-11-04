// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/gitdiff"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestAscNewChangeReviewersKeepThePeopleOfferedAndTheAgents(t *testing.T) {
	carol, erin := &user_model.User{ID: 7, Name: "Carol"}, &user_model.User{ID: 9, Name: "erin"}
	out := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	resp := &ascv1.SuggestReviewersResponse{Reviewers: []*ascv1.SuggestedReviewer{
		{Handle: "carol", Knows: []byte("src/refunds"), KnowsPercent: 75, MedianReviewSecs: 30, CanApprove: true},
		{Handle: "mallory", Owns: "src/**", CanApprove: true},
		{Handle: "pipeline-svc", IsAgent: true},
		{Handle: "erin", Owns: "tests/**\x00", Knows: []byte("tests"), KnowsPercent: 400, CanApprove: true},
		{Handle: "", CanApprove: true},
	}}
	list := ascNewChangeReviewersOf(resp, []*user_model.User{carol, nil, erin}, func(u *user_model.User) time.Time {
		if u == erin {
			return out
		}
		return time.Time{}
	})
	require.Len(t, list, 3, "a person the hub does not offer and an empty handle are left out")
	assert.Equal(t, AscNewChangeReviewer{User: carol, Handle: "carol", Knows: "src/refunds", KnowsPercent: 75, ReviewSecs: 30}, list[0])
	assert.Equal(t, AscNewChangeReviewer{Handle: "pipeline-svc", IsAgent: true, NeverApproves: true}, list[1], "an agent without an account is listed and cannot be asked")
	assert.Equal(t, AscNewChangeReviewer{User: erin, Handle: "erin", Owns: "tests/**", Knows: "tests", KnowsPercent: 100, Away: out}, list[2])

	var many []*ascv1.SuggestedReviewer
	for i := 0; i < 10; i++ {
		many = append(many, &ascv1.SuggestedReviewer{Handle: fmt.Sprintf("bot%d", i), IsAgent: true})
	}
	assert.Len(t, ascNewChangeReviewersOf(&ascv1.SuggestReviewersResponse{Reviewers: many}, nil, nil), ascNewChangeReviewersMax)
}

func TestAscNewChangeReviewerReviewTimeInWords(t *testing.T) {
	initSnapshotLocales()
	l := translation.NewLocale("en-US")
	for secs, want := range map[uint64]string{0: "", 20: "1 min", 30 * 60: "30 min", 2 * 3600: "2 h", 47 * 3600: "47 h", 3 * 86400: "3 days"} {
		assert.Equal(t, want, AscNewChangeReviewer{ReviewSecs: secs}.ReviewIn(l), secs)
	}
}

func TestAscNewChangePathsTakeTheOldNameOfARename(t *testing.T) {
	assert.Nil(t, ascNewChangePaths(nil))
	d := &gitdiff.Diff{Files: []*gitdiff.DiffFile{{Name: "a.go", OldName: "a.go"}, {Name: "b.go", OldName: "old/b.go"}, {Name: "c.go"}}}
	assert.Equal(t, [][]byte{[]byte("a.go"), []byte("b.go"), []byte("old/b.go"), []byte("c.go")}, ascNewChangePaths(d))
	d.Files = nil
	for i := 0; i < ascNewChangeReviewPaths+5; i++ {
		d.Files = append(d.Files, &gitdiff.DiffFile{Name: fmt.Sprintf("f%d", i), OldName: fmt.Sprintf("f%d", i)})
	}
	assert.Len(t, ascNewChangePaths(d), ascNewChangeReviewPaths)
}

type fakeSuggest struct {
	ascv1.UnimplementedReviewServer
	mu   sync.Mutex
	req  *ascv1.SuggestReviewersRequest
	resp func() (*ascv1.SuggestReviewersResponse, error)
}

func (f *fakeSuggest) SuggestReviewers(_ gocontext.Context, r *ascv1.SuggestReviewersRequest) (*ascv1.SuggestReviewersResponse, error) {
	f.mu.Lock()
	f.req = r
	f.mu.Unlock()
	return f.resp()
}

func startFakeSuggest(t *testing.T) *fakeSuggest {
	t.Helper()
	f := &fakeSuggest{}
	srv := grpc.NewServer()
	ascv1.RegisterReviewServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	return f
}

func TestPrepareAscNewChangeReviewersAsksForTheFilesOfTheComparison(t *testing.T) {
	f := startFakeSuggest(t)
	env := railCtx(t, true)
	offered, err := repo_model.GetReviewers(env.ctx, env.ctx.Repo.Repository, 2, 2)
	require.NoError(t, err)
	require.NotEmpty(t, offered, "the fixtures offer someone to review repo1")
	who := offered[0]
	day := time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	require.NoError(t, ascnotify.Save(env.ctx, who.ID, ascnotify.Prefs{Away: day}))
	f.resp = func() (*ascv1.SuggestReviewersResponse, error) {
		return &ascv1.SuggestReviewersResponse{Reviewers: []*ascv1.SuggestedReviewer{
			{Handle: who.Name, Knows: []byte("."), KnowsPercent: 50, CanApprove: true},
			{Handle: "pipeline-svc", IsAgent: true},
		}}, nil
	}
	prepareAscNewChangeReviewers(env.ctx)
	list, ok := env.ctx.Data["AscNewChangeReviewers"].([]AscNewChangeReviewer)
	require.True(t, ok)
	require.Len(t, list, 2)
	assert.Equal(t, who.ID, list[0].User.ID)
	assert.Equal(t, day, list[0].Away.Format(time.DateOnly), "a person who is out says until when")
	assert.Nil(t, list[1].User)

	assert.Empty(t, f.req.GetChange())
	assert.Equal(t, [][]byte{[]byte("a.go")}, f.req.GetPaths())
	assert.Equal(t, "user2", f.req.GetAuthor())
}

func TestPrepareAscNewChangeReviewersLeavesTheCardOutWhenAscDoesNotAnswer(t *testing.T) {
	f := startFakeSuggest(t)
	f.resp = func() (*ascv1.SuggestReviewersResponse, error) { return nil, fmt.Errorf("down") }
	env := railCtx(t, true)
	prepareAscNewChangeReviewers(env.ctx)
	assert.Nil(t, env.ctx.Data["AscNewChangeReviewers"])

	f.resp = func() (*ascv1.SuggestReviewersResponse, error) { return &ascv1.SuggestReviewersResponse{}, nil }
	f.req = nil
	env = railCtx(t, false)
	prepareAscNewChangeReviewers(env.ctx)
	assert.Nil(t, env.ctx.Data["AscNewChangeReviewers"], "nobody is signed in")
	assert.Nil(t, f.req)

	env = railCtx(t, true)
	env.ctx.Data["Diff"] = &gitdiff.Diff{}
	prepareAscNewChangeReviewers(env.ctx)
	assert.Nil(t, f.req, "a comparison without files")
}

func TestAscNewChangeReviewersTemplate(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	list := []AscNewChangeReviewer{
		{User: user4, Handle: "user4", Knows: "src/refunds", KnowsPercent: 31, ReviewSecs: 2 * 3600},
		{Handle: "pipeline-svc", IsAgent: true, NeverApproves: true},
		{User: user4, Handle: "erin", Owns: "tests/**", Away: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)},
	}
	out := renderPart(t, "repo/issue/asc_newchange_reviewers", func(d map[string]any) { d["AscNewChangeReviewers"] = list })
	for _, want := range []string{
		`data-testid="asc-newchange-reviewers"`, "asc_rp.rev_title", "asc_rp.rev_chip",
		`name="asc_reviewer" value="4"`, `ds-av ds-av-40 ds-av-agent`, ">asc_rp.rev_owns · asc_rp.rev_away</span>",
	} {
		assert.Contains(t, out, want)
	}
	assert.Equal(t, 3, strings.Count(out, `data-testid="asc-ncr-row"`))
	assert.Equal(t, 2, strings.Count(out, `data-testid="asc-ncr-add"`), "an agent without an account has no Add")
	assert.Contains(t, out, "asc_rp.rev_knows · asc_rp.rev_usually</span>", "the parts of a row are joined by a dot")
	assert.Contains(t, out, ">asc_rp.rev_agent · asc_rp.rev_never_approves</span>")

	assert.Empty(t, strings.TrimSpace(renderPart(t, "repo/issue/asc_newchange_reviewers", func(map[string]any) {})))
}

func TestAscRequestNewChangeReviewersAsksThePeopleMarkedAndNamesTheOnesItCannot(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/compare/master...branch2")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 2})
	require.NoError(t, issue.LoadRepo(ctx))
	ctx.Req.PostForm = url.Values{ascNewChangeReviewerField: {"4", "4", "x", "-1", "999999", "1"}}

	ascRequestNewChangeReviewers(ctx, issue)
	unittest.AssertExistsAndLoadBean(t, &issues_model.Review{IssueID: 2, ReviewerID: 4, Type: issues_model.ReviewTypeRequest})
	assert.Equal(t, "asc_rp.rev_not_asked", ctx.Flash.WarningMsg, "the author of the change cannot review it")
	unittest.AssertNotExistsBean(t, &issues_model.Review{IssueID: 2, ReviewerID: 1, Type: issues_model.ReviewTypeRequest})
	assert.Equal(t, 1, unittest.GetCount(t, &issues_model.Review{IssueID: 2, ReviewerID: 4, Type: issues_model.ReviewTypeRequest}), "a person marked twice is asked once")

	ctx, _ = contexttest.MockContext(t, "POST /user2/repo1/compare/master...branch2")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.PostForm = url.Values{ascNewChangeReviewerField: {"5"}}
	ascRequestNewChangeReviewers(ctx, issue)
	assert.Empty(t, ctx.Flash.WarningMsg, "nothing to warn about when everyone was asked")
}
