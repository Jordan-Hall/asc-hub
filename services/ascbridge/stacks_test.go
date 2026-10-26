// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbridge

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	changeA = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	changeB = "llllllllllllllllllllllllllllllll"
	changeC = "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm"
)

func id32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

// gitOf is the git id the fake GitMap gives an asc id: its first 20 bytes.
func gitOf(b byte) string {
	return strings.Repeat(string("0123456789abcdef"[b>>4])+string("0123456789abcdef"[b&15]), 20)
}

type fakeStacks struct {
	ascv1.StacksClient // nil: any other call panics

	restackReq  []*ascv1.RestackStackRequest
	restackResp *ascv1.RestackStackResponse
	restackErr  error
	revertReq   []*ascv1.RevertLandedRequest
	revertResp  *ascv1.RevertLandedResponse
	revertErr   error
}

func (f *fakeStacks) Restack(_ context.Context, in *ascv1.RestackStackRequest, _ ...grpc.CallOption) (*ascv1.RestackStackResponse, error) {
	f.restackReq = append(f.restackReq, in)
	return f.restackResp, f.restackErr
}

func (f *fakeStacks) RevertLanded(_ context.Context, in *ascv1.RevertLandedRequest, _ ...grpc.CallOption) (*ascv1.RevertLandedResponse, error) {
	f.revertReq = append(f.revertReq, in)
	return f.revertResp, f.revertErr
}

type fakeStackRepo struct {
	ascv1.RepoClient // nil: any other call panics

	unknown map[string]bool // asc ids GitMap cannot map
	calls   int
}

func (f *fakeStackRepo) GitMap(_ context.Context, in *ascv1.GitMapRequest, _ ...grpc.CallOption) (*ascv1.GitMapResponse, error) {
	f.calls++
	resp := &ascv1.GitMapResponse{}
	for _, a := range in.GetAsc() {
		if f.unknown[string(a)] {
			resp.AscToGit = append(resp.AscToGit, nil)
			continue
		}
		resp.AscToGit = append(resp.AscToGit, bytes.Repeat([]byte{a[0]}, 20))
	}
	return resp, nil
}

func newOps(t *testing.T) (*stackOps, *fakeStacks, *fakeStackRepo) {
	t.Helper()
	s, r := &fakeStacks{}, &fakeStackRepo{}
	return &stackOps{stacks: s, repo: r}, s, r
}

func detailErr(t *testing.T, code codes.Code, detail *ascv1.ErrorDetail) error {
	t.Helper()
	st, err := status.New(code, "secret server message with token abc123").WithDetails(detail)
	require.NoError(t, err)
	return st.Err()
}

func mustChange(t *testing.T, text string) []byte {
	t.Helper()
	b, err := ChangeBytes(text)
	require.NoError(t, err)
	return b
}

func TestChangeRoundTrip(t *testing.T) {
	for _, c := range []string{changeA, changeB, "kzkzkzkzkzkzkzkzkzkzkzkzkzkzkzkz", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		b, err := ChangeBytes(c)
		require.NoError(t, err)
		back, err := ChangeText(b)
		require.NoError(t, err)
		assert.Equal(t, c, back)
	}
	b, err := ChangeBytes("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	require.NoError(t, err)
	assert.Equal(t, byte(0xff), b[0])
	for _, bad := range []string{"", "abc", strings.Repeat("a", 32), strings.Repeat("k", 31), strings.Repeat("k", 33), strings.Repeat("{", 32)} {
		_, err := ChangeBytes(bad)
		assert.Error(t, err, "%q", bad)
	}
	_, err = ChangeText(make([]byte, 15))
	assert.Error(t, err)
}

func TestRestackStackDryRun(t *testing.T) {
	ops, stacks, repo := newOps(t)
	stacks.restackResp = &ascv1.RestackStackResponse{
		Onto: id32(0xaa),
		Items: []*ascv1.RestackItemResult{
			{Change: mustChange(t, changeA), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_REWRITTEN, OldHead: id32(0x11), NewHead: id32(0x22), NewVersion: 3},
			{Change: mustChange(t, changeB), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_CONFLICT, OldHead: id32(0x33),
				Conflicts: []*ascv1.Conflict{{Path: []byte("src/a.go")}, {Path: []byte("bad\x00\xff\npath")}}},
			{Change: mustChange(t, changeC), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_BLOCKED, OldHead: id32(0x44)},
		},
		Applied: true, // a dry run is never applied, whatever the answer says
	}
	res, err := ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeB, DryRun: true})
	require.NoError(t, err)
	require.Len(t, stacks.restackReq, 1)
	assert.True(t, stacks.restackReq[0].GetDryRun())
	assert.Equal(t, "o/r", stacks.restackReq[0].GetRepo().GetName())
	assert.Equal(t, mustChange(t, changeB), stacks.restackReq[0].GetChange())
	assert.False(t, res.Applied)
	assert.Equal(t, gitOf(0xaa), res.Onto)
	require.Len(t, res.Items, 3)
	assert.Equal(t, RestackRewritten, res.Items[0].State)
	assert.Equal(t, gitOf(0x11), res.Items[0].OldHead)
	assert.Empty(t, res.Items[0].NewHead, "a dry run prints no new head")
	assert.EqualValues(t, 3, res.Items[0].NewVersion)
	assert.Equal(t, RestackConflict, res.Items[1].State)
	assert.Equal(t, []string{"src/a.go", "bad���path"}, res.Items[1].Conflicts, "paths are made printable")
	assert.Equal(t, RestackBlocked, res.Items[2].State)
	assert.Equal(t, 2, repo.calls, "the old heads map in one batch and onto in another, not one call per id")
}

func TestRestackStackApplied(t *testing.T) {
	ops, stacks, _ := newOps(t)
	stacks.restackResp = &ascv1.RestackStackResponse{
		Onto: id32(0xaa), Applied: true,
		Items: []*ascv1.RestackItemResult{
			{Change: mustChange(t, changeA), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_SKIPPED_LANDED},
			{Change: mustChange(t, changeB), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_REWRITTEN, OldHead: id32(0x11), NewHead: id32(0x22), NewVersion: 2},
			// A patch-identical rewrite is reported as unchanged and may carry its new head.
			{Change: mustChange(t, changeC), State: ascv1.RestackItemState_RESTACK_ITEM_STATE_UNCHANGED, OldHead: id32(0x33), NewHead: id32(0x44)},
		},
	}
	res, err := ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeA})
	require.NoError(t, err)
	assert.True(t, res.Applied)
	assert.False(t, stacks.restackReq[0].GetDryRun())
	assert.Equal(t, RestackSkippedLanded, res.Items[0].State)
	assert.Equal(t, gitOf(0x22), res.Items[1].NewHead)
	assert.Equal(t, gitOf(0x11), res.Items[1].OldHead)
	assert.Equal(t, RestackUnchanged, res.Items[2].State)
	assert.Equal(t, gitOf(0x44), res.Items[2].NewHead)
}

func TestRestackStackRefusesBadInputAndBadAnswers(t *testing.T) {
	ops, stacks, repo := newOps(t)
	_, err := ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: "nope"})
	require.ErrorIs(t, err, ErrStackBadRequest)
	assert.Empty(t, stacks.restackReq, "a bad change id never reaches asc")

	good := mustChange(t, changeA)
	for name, resp := range map[string]*ascv1.RestackStackResponse{
		"unknown state": {Items: []*ascv1.RestackItemResult{{Change: good, State: 99}}},
		"bad change":    {Items: []*ascv1.RestackItemResult{{Change: []byte{1, 2}, State: ascv1.RestackItemState_RESTACK_ITEM_STATE_UNCHANGED}}},
		"rewritten without a head": {Applied: true, Items: []*ascv1.RestackItemResult{
			{Change: good, State: ascv1.RestackItemState_RESTACK_ITEM_STATE_REWRITTEN, OldHead: id32(1)}}},
		"short head": {Applied: true, Items: []*ascv1.RestackItemResult{
			{Change: good, State: ascv1.RestackItemState_RESTACK_ITEM_STATE_REWRITTEN, OldHead: id32(1), NewHead: []byte{1}}}},
	} {
		stacks.restackResp = resp
		_, err := ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeA})
		require.ErrorIs(t, err, ErrBadAnswer, name)
	}

	// Too many items.
	big := &ascv1.RestackStackResponse{}
	for range MaxStackItems + 1 {
		big.Items = append(big.Items, &ascv1.RestackItemResult{Change: good, State: ascv1.RestackItemState_RESTACK_ITEM_STATE_UNCHANGED})
	}
	stacks.restackResp = big
	_, err = ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeA})
	require.ErrorIs(t, err, ErrBadAnswer)

	// A head asc cannot map to git is a bad answer, not an empty head.
	repo.unknown = map[string]bool{string(id32(7)): true}
	stacks.restackResp = &ascv1.RestackStackResponse{Items: []*ascv1.RestackItemResult{
		{Change: good, State: ascv1.RestackItemState_RESTACK_ITEM_STATE_UNCHANGED, OldHead: id32(7)}}}
	_, err = ops.RestackStack(t.Context(), RestackStackRequest{Repo: "o/r", Change: changeA})
	require.ErrorIs(t, err, ErrBadAnswer)
}

func TestStackErrorsAreMappedAndDoNotLeak(t *testing.T) {
	ops, stacks, _ := newOps(t)
	req := RestackStackRequest{Repo: "o/r", Change: changeA}
	for name, tc := range map[string]struct {
		err  error
		want error
	}{
		"frozen":        {detailErr(t, codes.FailedPrecondition, &ascv1.ErrorDetail{Code: "CHANGE_FROZEN"}), ErrStackFrozen},
		"land conflict": {detailErr(t, codes.Aborted, &ascv1.ErrorDetail{Code: "LAND_CONFLICT", Paths: []string{"x.go"}}), ErrStackConflict},
		"not landed":    {detailErr(t, codes.FailedPrecondition, &ascv1.ErrorDetail{Code: "STACK_NOT_LANDED"}), ErrStackNotLanded},
		"unimplemented": {status.Error(codes.Unimplemented, "secret server message with token abc123"), ErrStacksUnavailable},
		"denied":        {status.Error(codes.PermissionDenied, "secret server message with token abc123"), ErrStackDenied},
		"unauth":        {status.Error(codes.Unauthenticated, "secret server message with token abc123"), ErrStackDenied},
		"not found":     {status.Error(codes.NotFound, "secret server message with token abc123"), ErrStackNotFound},
		"bad request":   {status.Error(codes.InvalidArgument, "secret server message with token abc123"), ErrStackBadRequest},
	} {
		stacks.restackErr = tc.err
		_, err := ops.RestackStack(t.Context(), req)
		require.ErrorIs(t, err, tc.want, name)
		assert.NotContains(t, err.Error(), "abc123", name)
		assert.NotContains(t, err.Error(), "secret", name)
	}

	// Anything else keeps the status code and nothing of the message.
	stacks.restackErr = status.Error(codes.Unavailable, "secret server message with token abc123")
	_, err := ops.RestackStack(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unavailable")
	assert.NotContains(t, err.Error(), "abc123")

	// The conflict paths of a LAND_CONFLICT ride along.
	stacks.restackErr = detailErr(t, codes.Aborted, &ascv1.ErrorDetail{Code: "LAND_CONFLICT", Paths: []string{"x.go"},
		Conflicts: []*ascv1.Conflict{{Path: []byte("y.go")}}})
	_, err = ops.RestackStack(t.Context(), req)
	var ce *ConflictError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, []string{"y.go", "x.go"}, ce.Paths)
}

func TestConflictPathsAreBounded(t *testing.T) {
	var cs []*ascv1.Conflict
	for range MaxConflictPaths + 50 {
		cs = append(cs, &ascv1.Conflict{Path: []byte("p")})
	}
	assert.Len(t, conflictPaths(cs), MaxConflictPaths)
	long := printable([]byte(strings.Repeat("é", MaxPathBytes)))
	assert.LessOrEqual(t, len(long), MaxPathBytes)
	assert.Equal(t, "a�b", printable([]byte("a\x01b")))
}

func TestRevertLandedDryRunAndApplied(t *testing.T) {
	ops, stacks, _ := newOps(t)
	stacks.revertResp = &ascv1.RevertLandedResponse{
		Ref: "refs/users/someone/reverts",
		Reverts: []*ascv1.RevertedChange{
			{Original: mustChange(t, changeB), Conflicts: []*ascv1.Conflict{{Path: []byte("a.go")}}},
			{Original: mustChange(t, changeA)},
		},
		Applied: true,
	}
	res, err := ops.RevertLanded(t.Context(), RevertLandedRequest{Repo: "o/r", Change: changeB, Bottom: changeA, Squash: true, Message: "m", DryRun: true})
	require.NoError(t, err)
	got := stacks.revertReq[0]
	assert.True(t, got.GetDryRun())
	assert.True(t, got.GetSquash())
	assert.Equal(t, mustChange(t, changeB), got.GetChange())
	assert.Equal(t, mustChange(t, changeA), got.GetBottom())
	assert.Equal(t, []byte("m"), got.GetMessage())
	assert.False(t, res.Applied, "a dry run is never applied")
	require.Len(t, res.Reverts, 2)
	assert.Equal(t, []string{"a.go"}, res.Reverts[0].Conflicts)
	assert.Empty(t, res.Reverts[0].RevertGit)

	// Applied.
	stacks.revertResp = &ascv1.RevertLandedResponse{
		Ref: "refs/users/someone/reverts", StackTop: mustChange(t, changeC), Applied: true,
		Reverts: []*ascv1.RevertedChange{
			{Original: mustChange(t, changeB), RevertChange: mustChange(t, changeC), RevertChangeset: id32(0x55)},
		},
	}
	res, err = ops.RevertLanded(t.Context(), RevertLandedRequest{Repo: "o/r", Change: changeB})
	require.NoError(t, err)
	assert.True(t, res.Applied)
	assert.Equal(t, "refs/users/someone/reverts", res.Ref)
	assert.Equal(t, changeC, res.StackTop)
	assert.Equal(t, changeC, res.Reverts[0].RevertChange)
	assert.Equal(t, gitOf(0x55), res.Reverts[0].RevertGit)
	assert.Equal(t, changeB, res.Reverts[0].Original)
	assert.Empty(t, stacks.revertReq[1].GetBottom())
}

func TestRevertLandedErrorsAndBadAnswers(t *testing.T) {
	ops, stacks, _ := newOps(t)
	req := RevertLandedRequest{Repo: "o/r", Change: changeB}

	stacks.revertErr = detailErr(t, codes.Aborted, &ascv1.ErrorDetail{Code: "REVERT_CONFLICT", Conflicts: []*ascv1.Conflict{{Path: []byte("z.go")}}})
	_, err := ops.RevertLanded(t.Context(), req)
	require.ErrorIs(t, err, ErrRevertConflict)
	var ce *ConflictError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, []string{"z.go"}, ce.Paths)
	assert.NotContains(t, err.Error(), "abc123")

	stacks.revertErr = detailErr(t, codes.FailedPrecondition, &ascv1.ErrorDetail{Code: "STACK_NOT_LANDED"})
	_, err = ops.RevertLanded(t.Context(), req)
	require.ErrorIs(t, err, ErrStackNotLanded)
	stacks.revertErr = nil

	_, err = ops.RevertLanded(t.Context(), RevertLandedRequest{Repo: "o/r", Change: "bad"})
	require.ErrorIs(t, err, ErrStackBadRequest)
	_, err = ops.RevertLanded(t.Context(), RevertLandedRequest{Repo: "o/r", Change: changeB, Bottom: "bad"})
	require.ErrorIs(t, err, ErrStackBadRequest)

	orig := mustChange(t, changeB)
	for name, resp := range map[string]*ascv1.RevertLandedResponse{
		"applied with no reverts": {Applied: true},
		"no changeset":            {Applied: true, Reverts: []*ascv1.RevertedChange{{Original: orig, RevertChange: mustChange(t, changeC)}}},
		"no revert change":        {Applied: true, Reverts: []*ascv1.RevertedChange{{Original: orig, RevertChangeset: id32(1)}}},
		"bad original":            {Reverts: []*ascv1.RevertedChange{{Original: []byte{1}}}},
	} {
		stacks.revertResp = resp
		_, err := ops.RevertLanded(t.Context(), req)
		require.ErrorIs(t, err, ErrBadAnswer, name)
	}

	// An applied answer that carries a conflict is refused: nothing is opened from a conflicted revert.
	stacks.revertResp = &ascv1.RevertLandedResponse{Applied: true, Reverts: []*ascv1.RevertedChange{
		{Original: orig, RevertChange: mustChange(t, changeC), RevertChangeset: id32(1), Conflicts: []*ascv1.Conflict{{Path: []byte("c")}}}}}
	_, err = ops.RevertLanded(t.Context(), req)
	require.ErrorIs(t, err, ErrRevertConflict)
}

func TestUseStackOpsAndClient(t *testing.T) {
	fake := &stackOps{}
	restore := UseStackOps(fake)
	got, err := StackClient()
	require.NoError(t, err)
	assert.Same(t, fake, got)
	restore()

	// Without an internal token the real constructor refuses, and nothing is cached.
	prev := newStackOps
	defer func() { newStackOps = prev }()
	newStackOps = func() (StackOps, error) { return nil, errors.New("no token") }
	_, err = StackClient()
	require.Error(t, err)
	_, err = StackClient()
	require.Error(t, err)
}
