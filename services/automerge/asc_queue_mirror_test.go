// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package automerge

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// The mirror hears about the entries that are in the state now, and only those.
func TestAscQueueMirrorHearsTheEntriesInTheStateOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	var got []string
	t.Cleanup(UseAscQueueMirror(func(_ context.Context, repo *repo_model.Repository, e *AscStackQueueEntry, state string) {
		got = append(got, state+":"+e.AscChange)
	}))
	a := &AscStackQueueEntry{RepoID: 1, Branch: "master", PullID: 9001, AscChange: "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", DoerID: 2, State: AscQueueLanding}
	b := &AscStackQueueEntry{RepoID: 1, Branch: "master", PullID: 9002, AscChange: "llllllllllllllllllllllllllllllll", DoerID: 2, State: AscQueueQueued}
	_, err := db.GetEngine(t.Context()).Insert(a, b)
	require.NoError(t, err)

	ascQueueMirrorState(t.Context(), AscQueueLanding, []int64{9001, 9002})
	assert.Equal(t, []string{"landing:kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"}, got, "an entry the update skipped is not announced")

	got = nil
	ascQueueMirrorState(t.Context(), AscQueueMerged, nil)
	ascQueueMirrorState(t.Context(), AscQueueQueued, []int64{9002})
	assert.Equal(t, []string{"queued:llllllllllllllllllllllllllllllll"}, got)
}

func TestAscQueueChangeRaw(t *testing.T) {
	assert.Nil(t, ascChangeRaw("short"))
	assert.Nil(t, ascChangeRaw("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	assert.Len(t, ascChangeRaw("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"), 16)
}

type fakeMirrorQueue struct {
	ascv1.MergeQueueClient
	calls      []string
	reports    []*ascv1.ReportQueueEntryRequest
	revalidate error
}

func (f *fakeMirrorQueue) Revalidate(_ context.Context, in *ascv1.RevalidateRequest, _ ...grpc.CallOption) (*ascv1.RevalidateResponse, error) {
	f.calls = append(f.calls, "revalidate")
	return &ascv1.RevalidateResponse{}, f.revalidate
}

func (f *fakeMirrorQueue) ReportEntry(_ context.Context, in *ascv1.ReportQueueEntryRequest, _ ...grpc.CallOption) (*ascv1.ReportQueueEntryResponse, error) {
	f.calls = append(f.calls, "report")
	f.reports = append(f.reports, in)
	return &ascv1.ReportQueueEntryResponse{}, nil
}

type fakeMirrorRepo struct {
	ascv1.RepoClient
	toAsc [][]byte
}

func (f *fakeMirrorRepo) GitMap(_ context.Context, in *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	return &ascv1.GitMapResponse{GitToAsc: f.toAsc}, nil
}

// asc lands only an entry that is READY, so the mirror revalidates the entry before it reports that it starts to land, and an entry
// that is not READY is not reported as landing.
func TestAscQueueMirrorRevalidatesBeforeLanding(t *testing.T) {
	q := &fakeMirrorQueue{}
	c := ascQueueClients{queue: q, repo: &fakeMirrorRepo{}}
	require.NoError(t, c.landing(t.Context(), &ascv1.RepoRef{Name: "o/r"}, 7, "main"))
	assert.Equal(t, []string{"revalidate", "report"}, q.calls)
	assert.Equal(t, ascv1.QueueEntryState_QUEUE_ENTRY_STATE_LANDING, q.reports[0].GetState())
	assert.Equal(t, uint64(7), q.reports[0].GetId())

	q = &fakeMirrorQueue{revalidate: errors.New("down")}
	c.queue = q
	require.Error(t, c.landing(t.Context(), &ascv1.RepoRef{Name: "o/r"}, 7, "main"))
	assert.Equal(t, []string{"revalidate"}, q.calls, "nothing is reported for an entry that could not be revalidated")
}

// A landed entry carries the changeset that landed, found from the git commit the top pull request was merged as.
func TestAscQueueMirrorReportsTheLandedChangeset(t *testing.T) {
	cs := make([]byte, 32)
	cs[0] = 9
	q := &fakeMirrorQueue{}
	c := ascQueueClients{queue: q, repo: &fakeMirrorRepo{toAsc: [][]byte{cs}}}
	require.NoError(t, c.landed(t.Context(), &ascv1.RepoRef{Name: "o/r"}, 7, "df6be2e6b68a5a017296c15e035a18cd3270a109"))
	require.Len(t, q.reports, 1)
	assert.Equal(t, ascv1.QueueEntryState_QUEUE_ENTRY_STATE_LANDED, q.reports[0].GetState())
	assert.Equal(t, cs, q.reports[0].GetLandedAs())

	for _, bad := range []string{"", "zz", "df6b"} {
		q = &fakeMirrorQueue{}
		c = ascQueueClients{queue: q, repo: &fakeMirrorRepo{}}
		require.Error(t, c.landed(t.Context(), &ascv1.RepoRef{Name: "o/r"}, 7, bad), bad)
		assert.Empty(t, q.reports, "no changeset, no report: %q", bad)
	}
}
