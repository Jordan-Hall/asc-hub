// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A typed refusal of the gate (a secret, a missing review) on the call that moves a ref is a rejected push with the
// reason, in the native form and in the text form of an older asc.
func TestPublishErrShowsTheTypedRefusal(t *testing.T) {
	st, err := status.New(codes.FailedPrecondition, "a secret was found").
		WithDetails(&ascv1.ErrorDetail{Code: "SECRET_DETECTED", Paths: []string{"creds.txt"}})
	require.NoError(t, err)
	var rejected *git.ErrPushRejected
	require.ErrorAs(t, publishErr("UpdateRefs", st.Err()), &rejected)
	assert.Equal(t, "SECRET_DETECTED: a secret was found (creds.txt)", rejected.Message)

	legacy := status.Error(codes.Internal, `grpc-status-details-bin mismatch: grpc-status=FailedPrecondition, grpc-message="SECRET_DETECTED: creds.txt:1 aws-access-key-id", grpc-status-details-bin=message:"x"`)
	require.ErrorAs(t, publishErr("UpdateRefs", legacy), &rejected)
	assert.Equal(t, "SECRET_DETECTED: creds.txt:1 aws-access-key-id", rejected.Message)

	// A conflict is still an out-of-date push, and an internal error is neither.
	st, err = status.New(codes.FailedPrecondition, "ref moved").WithDetails(&ascv1.ErrorDetail{Code: "REF_CONFLICT"})
	require.NoError(t, err)
	var stale *git.ErrPushOutOfDate
	require.ErrorAs(t, publishErr("UpdateRefs", st.Err()), &stale)
	assert.NotErrorAs(t, publishErr("UpdateRefs", status.Error(codes.Internal, "internal error")), &rejected)
}
