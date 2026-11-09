// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/modules/timeutil"
	"forgejo.org/modules/translation"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAscDashQuoteOf(t *testing.T) {
	who := &user_model.User{Name: "user4"}
	q := ascDashQuoteOf(who, "\n  \n  Please split the retry change.  \nThe second line stays on the pull request.")
	require.NotNil(t, q)
	assert.Equal(t, "user4", q.Who)
	assert.Equal(t, "Please split the retry change.", q.Text, "the first line with text, trimmed")

	assert.Nil(t, ascDashQuoteOf(who, " \n\t\n"), "no text, no quote")

	long := strings.Repeat("é", ascDashQuoteRunes+5)
	q = ascDashQuoteOf(who, long)
	require.NotNil(t, q)
	assert.Equal(t, ascDashQuoteRunes, len([]rune(q.Text)), "cut on letters, not bytes")
	assert.True(t, strings.HasSuffix(q.Text, "…"))

	exact := strings.Repeat("a", ascDashQuoteRunes)
	assert.Equal(t, exact, ascDashQuoteOf(who, exact).Text, "a line that fits is not cut")
}

func TestAscDashReplyFromModels(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	e := db.GetEngine(t.Context())

	// pull 2 is issue 3 of user1: the newest review asking for changes is of a deleted user
	q := ascDashReplyFromModels(t.Context(), &AscInboxEntry{PullID: 2})
	require.NotNil(t, q)
	assert.Equal(t, "Ghost", q.Who)
	assert.Equal(t, "a deleted user's review", q.Text)

	now := timeutil.TimeStampNow()
	_, err := e.Insert(&issues_model.Review{Type: issues_model.ReviewTypeReject, ReviewerID: 4, IssueID: 3, Content: "Please split the retry change.\nMore on the pull request.", CreatedUnix: now + 10, UpdatedUnix: now + 10})
	require.NoError(t, err)
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	q = ascDashReplyFromModels(t.Context(), &AscInboxEntry{PullID: 2})
	require.NotNil(t, q)
	assert.Equal(t, user4.GetDisplayName(), q.Who)
	assert.Equal(t, "Please split the retry change.", q.Text)

	t.Run("a review without text and one of the author are passed over", func(t *testing.T) {
		_, err := e.Insert(&issues_model.Review{Type: issues_model.ReviewTypeReject, ReviewerID: 5, IssueID: 3, Content: "  ", CreatedUnix: now + 20, UpdatedUnix: now + 20})
		require.NoError(t, err)
		_, err = e.Insert(&issues_model.Review{Type: issues_model.ReviewTypeReject, ReviewerID: 1, IssueID: 3, Content: "my own note", CreatedUnix: now + 30, UpdatedUnix: now + 30})
		require.NoError(t, err)
		q := ascDashReplyFromModels(t.Context(), &AscInboxEntry{PullID: 2})
		require.NotNil(t, q)
		assert.Equal(t, user4.GetDisplayName(), q.Who)
	})

	t.Run("without a review asking for changes the newest comment of somebody else", func(t *testing.T) {
		pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
		require.NoError(t, pr.LoadIssue(t.Context()))
		_, err := e.Where("issue_id = ? AND type = ?", pr.IssueID, issues_model.ReviewTypeReject).Delete(&issues_model.Review{})
		require.NoError(t, err)
		other := int64(4)
		if pr.Issue.PosterID == other {
			other = 5
		}
		_, err = e.Insert(&issues_model.Comment{Type: issues_model.CommentTypeComment, PosterID: other, IssueID: pr.IssueID, Content: "Looks close, one question inline.", CreatedUnix: now + 40, UpdatedUnix: now + 40})
		require.NoError(t, err)
		_, err = e.Insert(&issues_model.Comment{Type: issues_model.CommentTypeComment, PosterID: pr.Issue.PosterID, IssueID: pr.IssueID, Content: "a note of the author", CreatedUnix: now + 50, UpdatedUnix: now + 50})
		require.NoError(t, err)
		q := ascDashReplyFromModels(t.Context(), &AscInboxEntry{PullID: 1})
		require.NotNil(t, q)
		u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: other})
		assert.Equal(t, u.GetDisplayName(), q.Who)
		assert.Equal(t, "Looks close, one question inline.", q.Text)
	})

	assert.Nil(t, ascDashReplyFromModels(t.Context(), &AscInboxEntry{PullID: 99999}), "a pull request that is gone")
}

func TestAscStackMembers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ascRenderSeed(t)
	assert.Equal(t, []int64{1, 2, 5}, ascStackMembers(t.Context(), 1))
	assert.Equal(t, []int64{1, 2, 5}, ascStackMembers(t.Context(), 5), "from the top it walks to the bottom first")
	assert.Nil(t, ascStackMembers(t.Context(), 3), "not stacked")
}

func TestAscDashKinds(t *testing.T) {
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	now := time.Now()

	median := ""
	restack := ascDashRestack{}
	restackCalls := 0
	defer test.MockVariableValue(&ascDashReplyLoad, func(_ gocontext.Context, e *AscInboxEntry) *AscDashQuote {
		return &AscDashQuote{Who: "Dana", Text: "Please split it."}
	})()
	defer test.MockVariableValue(&ascDashJoinLoad, func(gocontext.Context, *user_model.User, *AscInboxEntry) (int, string) {
		return 3, median
	})()
	defer test.MockVariableValue(&ascDashRestackLoad, func(gocontext.Context, *user_model.User, *AscInboxEntry, time.Time, map[int64]ascDashRestack) ascDashRestack {
		restackCalls++
		return restack
	})()

	t.Run("a change that came back quotes the reply", func(t *testing.T) {
		row := &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxReturned}, row, false, now, nil)
		require.NotNil(t, row.Quote)
		assert.Equal(t, "Dana", row.Quote.Who)
		assert.Empty(t, row.Note)
	})

	t.Run("a ready change says where it joins the queue", func(t *testing.T) {
		row := &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxReady}, row, true, now, nil)
		assert.Equal(t, "Joins the merge queue as number 3", row.Note)
		assert.Nil(t, row.Quote)

		median = "12 min"
		row = &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxReady}, row, true, now, nil)
		assert.Equal(t, "Joins the merge queue as number 3 · about 12 min", row.Note)

		row = &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxReady}, row, false, now, nil)
		assert.Empty(t, row.Note, "a change that does not land through the asc queue")
	})

	t.Run("a restacked change says the trunk moved", func(t *testing.T) {
		restack = ascDashRestack{Done: 2, Total: 3}
		row := &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxNeedsReview}, row, false, now, nil)
		assert.Equal(t, []AscDashChip{{"grey", "Trunk moved"}}, row.Chips)
		assert.Equal(t, "asc restacked 2 of 3", row.Note)

		row = &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxReady}, row, true, now, nil)
		assert.Equal(t, "Joins the merge queue as number 3 · about 12 min", row.Note, "the queue place is the note of a ready change")
		assert.Len(t, row.Chips, 1)

		restack = ascDashRestack{Total: 3}
		row = &AscDashRow{}
		ascDashKinds(t.Context(), l, nil, &AscInboxEntry{Section: issues_model.AscInboxNeedsReview}, row, false, now, nil)
		assert.Empty(t, row.Chips, "no restack, no chip")
		assert.Empty(t, row.Note)
	})
	assert.Equal(t, 7, restackCalls)
}

// fakeRestackAsc answers Version with its features and ListRefLog with the newest operation of each ref.
type fakeRestackAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedStacksServer
	features []string
	mu       sync.Mutex
	refs     []string
	newest   map[string]*ascv1.RefLogEntry
	fail     bool
}

func (f *fakeRestackAsc) Version(gocontext.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeRestackAsc) ListRefLog(_ gocontext.Context, r *ascv1.ListRefLogRequest) (*ascv1.ListRefLogResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refs = append(f.refs, r.GetRef())
	if f.fail {
		return nil, status.Error(codes.Unavailable, "down")
	}
	if e := f.newest[r.GetRef()]; e != nil {
		return &ascv1.ListRefLogResponse{Entries: []*ascv1.RefLogEntry{e}}, nil
	}
	return &ascv1.ListRefLogResponse{}, nil
}

func useFakeRestackAsc(t *testing.T, f *fakeRestackAsc) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterStacksServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	_, err = db.GetEngine(t.Context()).ID(1).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
	require.NoError(t, err)
}

func TestAscDashRestackFromAsc(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ascRenderSeed(t)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	now := time.Now()
	entry := func(seq uint64, ref, via string, at time.Time) *ascv1.RefLogEntry {
		return &ascv1.RefLogEntry{Seq: seq, Ref: ref, Via: via, Time: at.Unix(), Actor: "asc"}
	}
	f := &fakeRestackAsc{features: []string{asctrust.FeatureStacks}, newest: map[string]*ascv1.RefLogEntry{
		"refs/heads/branch1":      entry(1, "refs/heads/branch1", "restack", now.Add(-time.Hour)),
		"refs/heads/branch2":      entry(2, "refs/heads/branch2", "restack", now.Add(-2*time.Hour)),
		"refs/heads/pr-to-update": entry(3, "refs/heads/pr-to-update", "push", now.Add(-time.Hour)),
	}}
	useFakeRestackAsc(t, f)

	memo := map[int64]ascDashRestack{}
	got := ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 2}, now, memo)
	assert.Equal(t, ascDashRestack{Done: 2, Total: 3}, got, "the two changes whose newest move is a restack, of the three")
	assert.Equal(t, []string{"refs/heads/branch1", "refs/heads/branch2", "refs/heads/pr-to-update"}, f.refs)
	assert.Equal(t, map[int64]ascDashRestack{1: got, 2: got, 5: got}, memo, "every change of the stack is answered")

	t.Run("one dashboard reads a stack once", func(t *testing.T) {
		f.refs = nil
		assert.Equal(t, got, ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 5}, now, memo))
		assert.Empty(t, f.refs)
	})

	t.Run("a restack older than a day is not news", func(t *testing.T) {
		later := now.Add(ascDashRestackWindow + 90*time.Minute)
		got := ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 1}, later, map[int64]ascDashRestack{})
		assert.Equal(t, ascDashRestack{Done: 0, Total: 3}, got)
	})

	t.Run("an error of asc says nothing", func(t *testing.T) {
		f.fail = true
		defer func() { f.fail = false }()
		assert.Equal(t, ascDashRestack{}, ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 1}, now, map[int64]ascDashRestack{}))
	})

	t.Run("a change that is not stacked is not read", func(t *testing.T) {
		f.refs = nil
		assert.Equal(t, ascDashRestack{}, ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 3}, now, map[int64]ascDashRestack{}))
		assert.Empty(t, f.refs)
	})

	t.Run("without the stacks feature nothing is read", func(t *testing.T) {
		g := &fakeRestackAsc{newest: f.newest}
		useFakeRestackAsc(t, g)
		assert.Equal(t, ascDashRestack{}, ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 1}, now, map[int64]ascDashRestack{}))
		assert.Empty(t, g.refs)
	})

	t.Run("off without asc", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		assert.Equal(t, ascDashRestack{}, ascDashRestackFromAsc(t.Context(), doer, &AscInboxEntry{PullID: 1}, now, map[int64]ascDashRestack{}))
	})
}

func TestAscDashAgentQuestion(t *testing.T) {
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	l := translation.NewLocale("en-US")
	repo := &repo_model.Repository{ID: 1, Name: "repo1", OwnerName: "user2"}

	a := ascDashAgentFrom(l, repo, &ascv1.AgentRun{Id: 213, Agent: "coder-1", Title: "currency tables", State: ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT, Progress: &ascv1.AgentRunProgress{Current: "Should refunds\nuse the same policy?\x07"}})
	assert.Equal(t, "waiting", a.State.Slug)
	assert.Equal(t, "Should refunds use the same policy?", a.Question, "the step it waits on, on one line without control characters")

	a = ascDashAgentFrom(l, repo, &ascv1.AgentRun{Id: 214, Agent: "coder-2", State: ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, Progress: &ascv1.AgentRunProgress{Current: "Running the tests"}})
	assert.Empty(t, a.Question, "a run that does not wait asks nothing")
}
