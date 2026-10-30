// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"sync"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// fakeReview records what is sent to asc's Review service and answers from a thread list.
type fakeReview struct {
	ascv1.ReviewClient
	mu       sync.Mutex
	added    []*ascv1.AddCommentRequest
	resolved []*ascv1.ResolveThreadRequest
	requests []*ascv1.RequestReviewRequest
	threads  []*ascv1.ReviewThread
	failAdd  bool
}

func (f *fakeReview) AddComment(_ context.Context, in *ascv1.AddCommentRequest, _ ...grpc.CallOption) (*ascv1.AddCommentResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, in)
	if f.failAdd {
		return nil, assert.AnError
	}
	id := in.GetThreadId()
	if id == 0 {
		id = uint64(100 + len(f.threads))
		f.threads = append(f.threads, &ascv1.ReviewThread{Id: id, Comments: []*ascv1.ReviewComment{{ExternalId: in.GetExternalId()}}})
	}
	return &ascv1.AddCommentResponse{CommentId: 1}, nil
}

func (f *fakeReview) ListThreads(context.Context, *ascv1.ListThreadsRequest, ...grpc.CallOption) (*ascv1.ListThreadsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &ascv1.ListThreadsResponse{Threads: f.threads}, nil
}

func (f *fakeReview) ResolveThread(_ context.Context, in *ascv1.ResolveThreadRequest, _ ...grpc.CallOption) (*ascv1.ResolveThreadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, in)
	return &ascv1.ResolveThreadResponse{}, nil
}

func (f *fakeReview) RequestReview(_ context.Context, in *ascv1.RequestReviewRequest, _ ...grpc.CallOption) (*ascv1.RequestReviewResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, in)
	return &ascv1.RequestReviewResponse{}, nil
}

type mirrorFixture struct {
	pr   *issues_model.PullRequest
	doer *user_model.User
	api  *fakeReview
	deny bool
}

func newMirrorFixture(t *testing.T) *mirrorFixture {
	t.Helper()
	f := &mirrorFixture{api: &fakeReview{}}
	f.pr = setupAscVersions(t, &fakeChanges{versions: threeVersions()})
	f.doer = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	t.Cleanup(UseAscReviewAPI(f.api))
	gate := func(ctx context.Context, _ *user_model.User, _ *repo_model.Repository) (context.Context, bool, error) {
		return ctx, !f.deny, nil
	}
	t.Cleanup(SetAscReadCall(gate))
	t.Cleanup(SetAscUserCall(gate))
	return f
}

func insertComment(t *testing.T, f *mirrorFixture, typ issues_model.CommentType, path string, line, extra int64, commit, body string) *issues_model.Comment {
	t.Helper()
	c := &issues_model.Comment{Type: typ, IssueID: f.pr.IssueID, PosterID: f.doer.ID, TreePath: path, Line: line, ExtraLinesCount: extra, CommitSHA: commit, Content: body}
	_, err := db.GetEngine(t.Context()).NoAutoTime().Insert(c)
	require.NoError(t, err)
	return c
}

// A plain comment of a pull request of an asc stack is a thread on the change as a whole, mirrored under the hub id.
func TestAscMirrorPlainComment(t *testing.T) {
	f := newMirrorFixture(t)
	c := insertComment(t, f, issues_model.CommentTypeComment, "", 0, 0, "", "Looks good to me")
	ascMirrorComment(t.Context(), f.doer, f.pr, c, nil)

	require.Len(t, f.api.added, 1)
	got := f.api.added[0]
	assert.Equal(t, "owner/repo", got.GetRepo().GetName())
	assert.Len(t, got.GetChange(), 16)
	assert.Equal(t, "Looks good to me", got.GetBody())
	assert.Equal(t, ascExternalID(c.ID), got.GetExternalId())
	assert.Nil(t, got.GetAnchor(), "no file, no lines")
	assert.Zero(t, got.GetThreadId())
}

// A code comment is anchored on the version of the commit it was made on, on the side and lines it was made on.
func TestAscMirrorCodeCommentAnchorAndReply(t *testing.T) {
	f := newMirrorFixture(t)
	body := "Use the retry policy here\n```suggestion\nretry(policy, send)\n```"
	root := insertComment(t, f, issues_model.CommentTypeCode, "src/client.rs", 14, 2, gitHexN('b'), body)
	versionOf := ascVersionLookup(t.Context(), f.pr)
	ascMirrorComment(t.Context(), f.doer, f.pr, root, versionOf)

	require.Len(t, f.api.added, 1)
	a := f.api.added[0].GetAnchor()
	assert.Equal(t, "src/client.rs", string(a.GetPath()))
	assert.Equal(t, uint32(2), a.GetVersion(), "the version whose commit the comment was made on")
	assert.Equal(t, ascv1.DiffSide_DIFF_SIDE_NEW, a.GetSide())
	assert.Equal(t, uint32(14), a.GetStartLine())
	assert.Equal(t, uint32(16), a.GetEndLine(), "a comment on three lines")
	assert.Equal(t, "retry(policy, send)", f.api.added[0].GetSuggestion())
	assert.Zero(t, f.api.added[0].GetThreadId(), "the first comment starts a thread")

	// a reply on the same lines joins the thread of the first comment
	reply := insertComment(t, f, issues_model.CommentTypeCode, "src/client.rs", 14, 2, gitHexN('b'), "Done")
	ascMirrorComment(t.Context(), f.doer, f.pr, reply, versionOf)
	require.Len(t, f.api.added, 2)
	assert.Equal(t, f.api.threads[0].GetId(), f.api.added[1].GetThreadId())

	// a comment on the old side
	old := insertComment(t, f, issues_model.CommentTypeCode, "src/client.rs", -7, 0, gitHexN('c'), "Why was this removed?")
	ascMirrorComment(t.Context(), f.doer, f.pr, old, versionOf)
	require.Len(t, f.api.added, 3)
	last := f.api.added[2].GetAnchor()
	assert.Equal(t, ascv1.DiffSide_DIFF_SIDE_OLD, last.GetSide())
	assert.Equal(t, uint32(7), last.GetStartLine())
}

// Resolving a conversation on the hub resolves the thread in asc; reopening reopens it.
func TestAscMirrorResolve(t *testing.T) {
	f := newMirrorFixture(t)
	root := insertComment(t, f, issues_model.CommentTypeCode, "a.rs", 3, 0, gitHexN('a'), "first")
	reply := insertComment(t, f, issues_model.CommentTypeCode, "a.rs", 3, 0, gitHexN('a'), "second")
	ascMirrorComment(t.Context(), f.doer, f.pr, root, nil)

	AscMirrorResolve(t.Context(), f.doer, f.pr, reply, true) // any comment of the conversation names it
	AscMirrorResolve(t.Context(), f.doer, f.pr, root, false)
	require.Len(t, f.api.resolved, 2)
	assert.True(t, f.api.resolved[0].GetResolved())
	assert.False(t, f.api.resolved[1].GetResolved())
	assert.Equal(t, f.api.threads[0].GetId(), f.api.resolved[0].GetThreadId())

	// a conversation that was never mirrored has no thread: nothing is sent
	other := insertComment(t, f, issues_model.CommentTypeCode, "b.rs", 9, 0, gitHexN('a'), "old")
	AscMirrorResolve(t.Context(), f.doer, f.pr, other, true)
	assert.Len(t, f.api.resolved, 2)
}

// A review request on the hub is a review request in asc.
func TestAscMirrorReviewRequest(t *testing.T) {
	f := newMirrorFixture(t)
	reviewer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	ascMirrorReviewRequest(t.Context(), f.doer, f.pr, reviewer, true)
	ascMirrorReviewRequest(t.Context(), f.doer, f.pr, reviewer, false)
	require.Len(t, f.api.requests, 2)
	assert.Equal(t, []string{reviewer.Name}, f.api.requests[0].GetAdd())
	assert.Equal(t, []string{reviewer.Name}, f.api.requests[1].GetRemove())
}

// Nothing is mirrored for a person the hub does not allow, for a pull request that is not a stack, or for an empty
// comment; and a failure of asc never reaches the person.
func TestAscMirrorSkipsAndNeverFails(t *testing.T) {
	f := newMirrorFixture(t)
	c := insertComment(t, f, issues_model.CommentTypeComment, "", 0, 0, "", "hello")

	f.deny = true
	ascMirrorComment(t.Context(), f.doer, f.pr, c, nil)
	ascMirrorComment(t.Context(), nil, f.pr, c, nil)
	f.deny = false
	ascMirrorComment(t.Context(), f.doer, f.pr, &issues_model.Comment{ID: 99, Content: "  "}, nil)
	ascMirrorComment(t.Context(), f.doer, &issues_model.PullRequest{}, c, nil)
	assert.Empty(t, f.api.added)

	f.api.failAdd = true
	ascMirrorComment(t.Context(), f.doer, f.pr, c, nil) // logged, not returned
	assert.Len(t, f.api.added, 1)
}

func TestAscSuggestionOf(t *testing.T) {
	assert.Equal(t, "a\nb", ascSuggestionOf("text\n```suggestion\na\nb\n```\nmore"))
	assert.Empty(t, ascSuggestionOf("```go\nx\n```"))
	assert.Empty(t, ascSuggestionOf("no fence"))
}
