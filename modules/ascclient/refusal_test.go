// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascclient

import (
	"errors"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRefusalOfReadsBothForms(t *testing.T) {
	// The form of an asc that packs the ErrorDetail as an Any in a google.rpc.Status.
	st, err := status.New(codes.FailedPrecondition, "a secret was found").
		WithDetails(&ascv1.ErrorDetail{Code: "SECRET_DETECTED", Paths: []string{"creds.txt", "deploy/key.pem"}})
	require.NoError(t, err)
	r, ok := RefusalOf(st.Err())
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, r.Status)
	assert.Equal(t, "SECRET_DETECTED", r.Code)
	assert.Equal(t, []string{"creds.txt", "deploy/key.pem"}, r.Paths)
	assert.Equal(t, "SECRET_DETECTED: a secret was found (creds.txt, deploy/key.pem)", r.Text())

	// A text that names the code and the file already is not told twice.
	st, err = status.New(codes.FailedPrecondition, "SECRET_DETECTED: creds.txt:1 aws-access-key-id").
		WithDetails(&ascv1.ErrorDetail{Code: "SECRET_DETECTED", Paths: []string{"creds.txt"}})
	require.NoError(t, err)
	r, ok = RefusalOf(st.Err())
	require.True(t, ok)
	assert.Equal(t, "SECRET_DETECTED: creds.txt:1 aws-access-key-id", r.Text())

	// Older asc: Internal, with the status and text inside the text.
	r, ok = RefusalOf(status.Error(codes.Internal,
		`grpc-status-details-bin mismatch: grpc-status=FailedPrecondition, grpc-message="SECRET_DETECTED: creds.txt:1 aws-access-key-id", grpc-status-details-bin=message:"x"`))
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, r.Status)
	assert.Equal(t, "SECRET_DETECTED", r.Code)
	assert.Equal(t, "SECRET_DETECTED: creds.txt:1 aws-access-key-id", r.Text())
}

func TestRefusalOfIgnoresWhatIsNoRefusal(t *testing.T) {
	for _, err := range []error{
		errors.New("dial failed"),
		status.Error(codes.Internal, "internal error"),
		status.Error(codes.Unavailable, "down"),
		status.Error(codes.NotFound, "no such repository"),
	} {
		_, ok := RefusalOf(err)
		assert.False(t, ok, "%v", err)
	}
}
