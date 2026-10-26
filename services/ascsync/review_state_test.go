// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkz" // 15 bytes 0xff, then 0xf0

type fakeChanges struct {
	ascv1.ChangesClient
	mu   sync.Mutex
	reqs []*ascv1.SetReviewStateRequest
	err  error
}

func (f *fakeChanges) SetReviewState(_ context.Context, in *ascv1.SetReviewStateRequest, _ ...grpc.CallOption) (*ascv1.SetReviewStateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, in)
	return &ascv1.SetReviewStateResponse{}, f.err
}

func (f *fakeChanges) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func reviewFixture(t *testing.T) (*fixture, *fakeChanges) {
	t.Helper()
	fx := newFixture(t)
	fc := &fakeChanges{}
	t.Cleanup(UseChangesClient(fc))
	prevDispatch, prevLoad := dispatch, loadFacts
	dispatch = func(f func()) { f() }
	t.Cleanup(func() { dispatch, loadFacts = prevDispatch, prevLoad })
	return fx, fc
}

func (fx *fixture) facts() *PullFacts {
	return &PullFacts{Repo: fx.repo, Index: 7, URL: "https://hub.example/o/r/pulls/7", Change: testChange, Approvals: 2, Checks: ChecksSuccess}
}

func TestAscReviewStateBuild(t *testing.T) {
	fx := newFixture(t)
	now := time.Unix(1700000000, 0)

	f := fx.facts()
	change, st, err := BuildReviewState(f, now)
	require.NoError(t, err)
	assert.Equal(t, append(bytes.Repeat([]byte{0xff}, 15), 0xf0), change)
	assert.Equal(t, &ascv1.ReviewState{Host: "forgejo", Number: 7, Url: f.URL, State: "open", Approvals: 2, Checks: "success", Queue: "none", Updated: 1700000000}, st)

	f.Draft = true
	_, st, _ = BuildReviewState(f, now)
	assert.Equal(t, "draft", st.State)
	assert.True(t, st.Draft)

	f.Queued = true
	_, st, _ = BuildReviewState(f, now)
	assert.Equal(t, "queued", st.Queue)

	f.Closed = true
	_, st, _ = BuildReviewState(f, now)
	assert.Equal(t, "closed", st.State)
	assert.Equal(t, "none", st.Queue, "a closed pull request is not queued")
	assert.False(t, st.Draft)

	f.Merged = true
	_, st, _ = BuildReviewState(f, now)
	assert.Equal(t, "merged", st.State)

	g := fx.facts()
	g.Approvals, g.Checks = -3, "<script>"
	_, st, _ = BuildReviewState(g, now)
	assert.Zero(t, st.Approvals)
	assert.Equal(t, "none", st.Checks, "an unknown checks word is not forwarded")
	g.Approvals = 1 << 40
	_, st, _ = BuildReviewState(g, now)
	assert.EqualValues(t, maxApprovals, st.Approvals)
}

func TestAscReviewStateSend(t *testing.T) {
	fx, fc := reviewFixture(t)
	require.NoError(t, SendReviewState(t.Context(), fx.facts()))
	require.Equal(t, 1, fc.count())
	req := fc.reqs[0]
	assert.Equal(t, "owner/repo", strings.ToLower(req.Repo.Name))
	require.Len(t, req.Changes, 1)
	require.Len(t, req.States, 1)
	assert.EqualValues(t, 7, req.States[0].Number)
}

func TestAscReviewStateSendRefusals(t *testing.T) {
	fx, fc := reviewFixture(t)

	plain := *fx.repo
	plain.StorageBackend = "git"
	f := fx.facts()
	f.Repo = &plain
	require.ErrorIs(t, SendReviewState(t.Context(), f), ErrNotAsc)

	setting.Asc.Enabled = false
	require.ErrorIs(t, SendReviewState(t.Context(), fx.facts()), ErrDisabled)
	setting.Asc.Enabled = true

	for _, bad := range []string{"", "kkkk", strings.Repeat("k", 33), strings.Repeat("K", 32), strings.Repeat("a", 32), strings.Repeat("k", 31) + "\n", "../" + strings.Repeat("k", 29)} {
		g := fx.facts()
		g.Change = bad
		require.ErrorIs(t, SendReviewState(t.Context(), g), ErrBadRequest, bad)
	}
	g := fx.facts()
	g.Index = 0
	require.ErrorIs(t, SendReviewState(t.Context(), g), ErrBadRequest)
	require.ErrorIs(t, SendReviewState(t.Context(), nil), ErrBadRequest)

	assert.Zero(t, fc.count(), "nothing was sent for a refused call")
}

func TestAscReviewStateNotifier(t *testing.T) {
	fx, fc := reviewFixture(t)
	loadFacts = func(_ context.Context, pr *issues_model.PullRequest) (*PullFacts, error) {
		if pr.ID == 1 {
			return fx.facts(), nil
		}
		return nil, nil // not a stacked pull request
	}
	n := &reviewStateNotifier{}
	ctx := t.Context()

	n.NewPullRequest(ctx, &issues_model.PullRequest{ID: 1}, nil)
	n.PullRequestReview(ctx, &issues_model.PullRequest{ID: 1}, nil, nil, nil)
	n.MergePullRequest(ctx, nil, &issues_model.PullRequest{ID: 1})
	n.PullRequestSynchronized(ctx, nil, &issues_model.PullRequest{ID: 1})
	assert.Equal(t, 4, fc.count())

	n.NewPullRequest(ctx, &issues_model.PullRequest{ID: 2}, nil)
	n.NewPullRequest(ctx, nil, nil)
	assert.Equal(t, 4, fc.count(), "a pull request without a change link sends nothing")

	setting.Asc.Enabled = false
	n.MergePullRequest(ctx, nil, &issues_model.PullRequest{ID: 1})
	setting.Asc.Enabled = true
	assert.Equal(t, 4, fc.count(), "disabled sends nothing")
}

func TestAscReviewStateFailureIsDropped(t *testing.T) {
	fx, fc := reviewFixture(t)
	loadFacts = func(context.Context, *issues_model.PullRequest) (*PullFacts, error) { return fx.facts(), nil }
	fc.err = status.Error(codes.Unavailable, "down")
	NotifyPullRequest(t.Context(), &issues_model.PullRequest{ID: 1}) // must not panic or block
	assert.Equal(t, 1, fc.count())

	loadFacts = func(context.Context, *issues_model.PullRequest) (*PullFacts, error) {
		return nil, errors.New("db down")
	}
	NotifyPullRequest(t.Context(), &issues_model.PullRequest{ID: 1})
	assert.Equal(t, 1, fc.count())
}

func TestAscReviewStateWordsAndRefs(t *testing.T) {
	assert.Equal(t, ChecksNone, checksWord(nil))
	for ref, want := range map[string]struct {
		n  int64
		ok bool
	}{
		"refs/pull/12/head":                   {12, true},
		"refs/pull/12/merge":                  {},
		"refs/pull/0/head":                    {},
		"refs/pull/-1/head":                   {},
		"refs/pull/1x/head":                   {},
		"refs/pull//head":                     {},
		"refs/pull/99999999999999999999/head": {},
		"refs/heads/x":                        {},
	} {
		n, ok := pullIndexOfRef(ref)
		assert.Equal(t, want.ok, ok, ref)
		if ok {
			assert.Equal(t, want.n, n, ref)
		}
	}
}
