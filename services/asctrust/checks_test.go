// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(n int) int64 { return time.Date(2026, 3, n, 0, 0, 0, 0, time.UTC).Unix() }

func TestHistoryOfBuildsTheStrip(t *testing.T) {
	h, err := HistoryOf(&ascv1.FlakyHistoryResponse{
		Buckets: []*ascv1.FlakyBucket{
			{Start: day(1), Runs: 4, Failures: 0},
			{Start: day(2), Runs: 5, Failures: 2, FlakyFlips: 1},
			{Start: day(3), Runs: 3, Failures: 3},
			{Start: day(4)},
		},
		FlakeRate: 0.0833, Flaky: true, FirstFlaky: day(2),
	})
	require.NoError(t, err)
	require.Len(t, h.Cells, 4)
	assert.Equal(t, []string{"ok", "wn", "no", "ro"}, []string{h.Cells[0].Tone, h.Cells[1].Tone, h.Cells[2].Tone, h.Cells[3].Tone})
	assert.Equal(t, "2 of 5 failed, flipped 1 times", h.Cells[1].Label, "every cell has words")
	assert.Equal(t, "no runs", h.Cells[3].Label)
	assert.Equal(t, 40, h.Cells[1].Percent)
	assert.EqualValues(t, 12, h.Runs)
	assert.EqualValues(t, 5, h.Failures)
	assert.EqualValues(t, 1, h.Flips)
	assert.Equal(t, "8.3", h.RatePercent)
	assert.True(t, h.Flaky)
	assert.False(t, h.FirstFlaky.IsZero())
}

func TestHistoryOfKeepsTheNewestCellsAndAllTheTotals(t *testing.T) {
	var buckets []*ascv1.FlakyBucket
	for i := 0; i < 100; i++ {
		buckets = append(buckets, &ascv1.FlakyBucket{Start: day(1) + int64(i)*86400, Runs: 1})
	}
	h, err := HistoryOf(&ascv1.FlakyHistoryResponse{Buckets: buckets})
	require.NoError(t, err)
	assert.Len(t, h.Cells, maxStripCells)
	assert.EqualValues(t, 100, h.Runs, "older days are in the totals")
	assert.Equal(t, time.Unix(day(1)+99*86400, 0).UTC(), h.Cells[len(h.Cells)-1].Day, "the strip ends on the newest day")
}

func TestHistoryOfRefusesBadAnswers(t *testing.T) {
	for name, resp := range map[string]*ascv1.FlakyHistoryResponse{
		"rate above one":         {FlakeRate: 1.5},
		"negative rate":          {FlakeRate: -0.1},
		"more failures than run": {Buckets: []*ascv1.FlakyBucket{{Start: day(1), Runs: 1, Failures: 2}}},
	} {
		_, err := HistoryOf(resp)
		assert.ErrorIs(t, err, ErrBadAnswer, name)
	}
	_, err := HistoryOf(&ascv1.FlakyHistoryResponse{Buckets: make([]*ascv1.FlakyBucket, maxBuckets+1)})
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestGroupDetailOfCleansRunnerOutput(t *testing.T) {
	d, err := GroupDetailOf(&ascv1.GetFailureGroupResponse{
		Group: &ascv1.FailureGroup{Id: 4, Signature: "ab12", Title: "the client panics\u202e on zero", TestName: "retry::tests::zero_attempts", JobName: "unit", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, Occurrences: 2, ChangesAffected: 1, Flaky: false},
		Occurrences: []*ascv1.FailureOccurrence{{
			RunId: 77, JobId: 3, Version: 4, Attempt: 2, At: day(3), Head: []byte{0x9b, 0x2e, 0x41, 0xc0, 0xff, 0xee},
			Message: "thread 'x' panicked\r\n\x1b[31mattempt to divide by zero\x1b[0m\n" + strings.Repeat("y", 10000),
		}},
		NextPageToken: "n_3",
	})
	require.NoError(t, err)
	assert.Equal(t, "the client panics on zero", d.Group.Title)
	assert.Equal(t, "ab12", d.Group.Signature)
	require.Len(t, d.Occurrences, 1)
	o := d.Occurrences[0]
	assert.NotContains(t, o.Message, "\x1b", "escape sequences of a runner are removed")
	assert.NotContains(t, o.Message, "\r")
	assert.Contains(t, o.Message, "attempt to divide by zero")
	assert.LessOrEqual(t, len(o.Message), MaxMessageBytes)
	assert.Equal(t, "9b2e41c0ffee", o.Head)
	assert.Equal(t, "n_3", d.Next)
}

func TestGroupDetailAndGroupsRefuseBadAnswers(t *testing.T) {
	_, err := GroupDetailOf(&ascv1.GetFailureGroupResponse{Group: &ascv1.FailureGroup{Id: 0}})
	assert.ErrorIs(t, err, ErrBadAnswer)
	_, err = GroupDetailOf(&ascv1.GetFailureGroupResponse{Group: &ascv1.FailureGroup{Id: 1}, NextPageToken: "bad token"})
	assert.ErrorIs(t, err, ErrBadAnswer)
	_, err = GroupDetailOf(&ascv1.GetFailureGroupResponse{Group: &ascv1.FailureGroup{Id: 1}, Occurrences: make([]*ascv1.FailureOccurrence, maxOccurrences+1)})
	assert.ErrorIs(t, err, ErrBadAnswer)
	_, err = GroupsPageOf(&ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{nil}})
	assert.ErrorIs(t, err, ErrBadAnswer)
	_, _, err = FlakyOf(&ascv1.ListFlakyTestsResponse{Tests: []*ascv1.FlakyTest{{TestName: "t", FlakeRate: 2}}})
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestFlakyOf(t *testing.T) {
	rows, next, err := FlakyOf(&ascv1.ListFlakyTestsResponse{
		Tests:         []*ascv1.FlakyTest{{TestName: "refund_webhook_timeout", JobName: "integration", FlakeRate: 0.3, Runs: 20, LastFlip: day(5), GroupId: 12}},
		NextPageToken: "x_1",
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "30.0", rows[0].RatePercent)
	assert.EqualValues(t, 12, rows[0].GroupID)
	assert.Equal(t, "x_1", next)
}

func TestChecksInputChecks(t *testing.T) {
	for _, in := range []string{"unit", "unit tests (linux)", "a/b"} {
		_, err := CheckName(in)
		assert.NoError(t, err, in)
	}
	for _, in := range []string{"", "  ", strings.Repeat("n", 513), "a\x00b", "bi\u202ex"} {
		_, err := CheckName(in)
		assert.ErrorIs(t, err, ErrBadInput, "%q", in)
	}
	_, err := CheckGroupNote(strings.Repeat("n", 1025))
	assert.ErrorIs(t, err, ErrBadInput)
	n, err := CheckGroupNote("flaky since the runner upgrade\r\nsee issue 12")
	require.NoError(t, err)
	assert.Contains(t, n, "issue 12")

	for in, want := range map[string]uint32{"": 30, "7": 7, "14": 14, "30": 30, "90": 90, "180": 180} {
		got, ok := WindowDays(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"0", "8", "365", "-1", "abc"} {
		_, ok := WindowDays(in)
		assert.False(t, ok, in)
	}
	_, ok := GroupStateOf("quarantined")
	assert.True(t, ok)
	_, ok = GroupStateOf("deleted")
	assert.False(t, ok)
}

func TestLoadChecksAsksForOpenAndAcknowledgedByDefault(t *testing.T) {
	f, c := startFake(t)
	f.listGroups = func(req *ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{{Id: 1, Title: "boom", State: ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN}}, NextPageToken: "n_2"}, nil
	}
	f.listFlaky = func(req *ascv1.ListFlakyTestsRequest) (*ascv1.ListFlakyTestsResponse, error) {
		return &ascv1.ListFlakyTestsResponse{Tests: []*ascv1.FlakyTest{{TestName: "t", FlakeRate: 0.1, Runs: 10, GroupId: 1}}}, nil
	}
	page, err := LoadChecks(System(t.Context()), c, "acme/payments", ChecksQuery{FlakyOnly: true, Job: "unit", Test: "retry"}, 14)
	require.NoError(t, err)
	assert.Equal(t, "boom", page.Groups.Rows[0].Title)
	assert.Equal(t, "n_2", page.Groups.Next)
	require.Len(t, page.Flaky, 1)
	call := f.last(t, "ListFailureGroups")
	assert.Equal(t, "Bearer "+testInternalToken, call.Auth, "reads of public data use the hub's credential")
	req := call.Req.(*ascv1.ListFailureGroupsRequest)
	assert.Equal(t, []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_OPEN, ascv1.FailureGroupState_FAILURE_GROUP_STATE_ACKNOWLEDGED}, req.GetStates())
	assert.True(t, req.GetFlakyOnly())
	assert.Equal(t, "unit", req.GetJobName())
	assert.Equal(t, "retry", req.GetTestName())
	assert.EqualValues(t, 14, f.last(t, "ListFlakyTests").Req.(*ascv1.ListFlakyTestsRequest).GetWindowDays())

	// A later page does not ask for the flaky list again.
	n := f.count("ListFlakyTests")
	_, err = LoadChecks(System(t.Context()), c, "acme/payments", ChecksQuery{PageToken: "n_2", State: "resolved"}, 30)
	require.NoError(t, err)
	assert.Equal(t, n, f.count("ListFlakyTests"))
	assert.Equal(t, []ascv1.FailureGroupState{ascv1.FailureGroupState_FAILURE_GROUP_STATE_RESOLVED}, f.last(t, "ListFailureGroups").Req.(*ascv1.ListFailureGroupsRequest).GetStates())

	for _, q := range []ChecksQuery{{State: "bogus"}, {Job: "a\x00"}, {Test: strings.Repeat("t", 600)}, {PageToken: "no good"}} {
		_, err := LoadChecks(System(t.Context()), c, "acme/payments", q, 30)
		assert.ErrorIs(t, err, ErrBadInput, "%+v", q)
	}
}

func TestLoadGroupWithHistory(t *testing.T) {
	f, c := startFake(t)
	f.getGroup = func(req *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		return &ascv1.GetFailureGroupResponse{Group: &ascv1.FailureGroup{Id: req.GetId(), TestName: "t", JobName: "unit"}}, nil
	}
	f.history = func(req *ascv1.FlakyHistoryRequest) (*ascv1.FlakyHistoryResponse, error) {
		return &ascv1.FlakyHistoryResponse{Buckets: []*ascv1.FlakyBucket{{Start: day(1), Runs: 2}}, FlakeRate: 0.01}, nil
	}
	d, h, err := LoadGroup(System(t.Context()), c, "acme/payments", 5, "", 30)
	require.NoError(t, err)
	assert.EqualValues(t, 5, d.Group.ID)
	require.NotNil(t, h)
	hreq := f.last(t, "FlakyHistory").Req.(*ascv1.FlakyHistoryRequest)
	assert.Equal(t, "t", hreq.GetTestName())
	assert.EqualValues(t, 30, hreq.GetWindowDays())

	// A group the server answers for another id is refused.
	f.getGroup = func(req *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		return &ascv1.GetFailureGroupResponse{Group: &ascv1.FailureGroup{Id: req.GetId() + 1}}, nil
	}
	_, _, err = LoadGroup(System(t.Context()), c, "acme/payments", 5, "", 30)
	assert.ErrorIs(t, err, ErrBadAnswer)

	// A failed history leaves the group: the strip is advice.
	f.getGroup = func(req *ascv1.GetFailureGroupRequest) (*ascv1.GetFailureGroupResponse, error) {
		return &ascv1.GetFailureGroupResponse{Group: &ascv1.FailureGroup{Id: req.GetId(), TestName: "t"}}, nil
	}
	f.history = nil
	d, h, err = LoadGroup(System(t.Context()), c, "acme/payments", 5, "", 30)
	require.NoError(t, err)
	assert.NotNil(t, d)
	assert.Nil(t, h)

	_, _, err = LoadGroup(System(t.Context()), c, "acme/payments", 0, "", 30)
	assert.ErrorIs(t, err, ErrBadInput)
}

func TestSetGroupState(t *testing.T) {
	f, c := startFake(t)
	f.setGroup = func(req *ascv1.SetFailureGroupStateRequest) (*ascv1.SetFailureGroupStateResponse, error) {
		return &ascv1.SetFailureGroupStateResponse{Group: &ascv1.FailureGroup{Id: req.GetId(), State: req.GetState(), Note: req.GetNote()}}, nil
	}
	op := NewOpID()
	g, err := SetGroupState(userCtx(t, "tok-maintainer"), c, "acme/payments", 8, "quarantined", "flaky since upgrade", op)
	require.NoError(t, err)
	assert.Equal(t, "quarantined", g.StateKey)
	call := f.last(t, "SetFailureGroupState")
	assert.Equal(t, "Bearer tok-maintainer", call.Auth, "a state change is made as the person")
	assert.Equal(t, op, call.OpID)

	before := f.count("SetFailureGroupState")
	for _, bad := range []struct {
		id    uint64
		state string
		note  string
		op    string
	}{{0, "open", "", op}, {8, "gone", "", op}, {8, "open", strings.Repeat("n", 2000), op}, {8, "open", "", "x"}} {
		_, err := SetGroupState(userCtx(t, "t"), c, "acme/payments", bad.id, bad.state, bad.note, bad.op)
		assert.ErrorIs(t, err, ErrBadInput, "%+v", bad)
	}
	assert.Equal(t, before, f.count("SetFailureGroupState"))
}
