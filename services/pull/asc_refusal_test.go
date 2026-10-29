// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAscRefusalOfATypedRefusal(t *testing.T) {
	r := ascRefusalOf(status.Error(codes.FailedPrecondition, "POLICY_DENIED: landing on trunk needs the approvals of the policy"))
	if assert.NotNil(t, r) {
		assert.Equal(t, "POLICY_DENIED", r.Code)
		assert.Equal(t, "landing on trunk needs the approvals of the policy", r.Message)
	}
	r = ascRefusalOf(status.Error(codes.PermissionDenied, "not an admin of acme/payments"))
	if assert.NotNil(t, r) {
		assert.Empty(t, r.Code, "a message without a code is a sentence")
		assert.Equal(t, "not an admin of acme/payments", r.Message)
	}
}

func TestAscRefusalOfTheUnreadableEncoding(t *testing.T) {
	err := status.Error(codes.Internal, `grpc-status-details-bin mismatch: grpc-status=FailedPrecondition, grpc-message="POLICY_DENIED: landing on trunk needs the approvals", grpc-status-details-bin=message:"x"`)
	r := ascRefusalOf(err)
	if assert.NotNil(t, r) {
		assert.Equal(t, "POLICY_DENIED", r.Code)
		assert.Equal(t, "landing on trunk needs the approvals", r.Message)
	}
	assert.Nil(t, ascRefusalOf(status.Error(codes.Internal, "database is on fire")), "an ordinary Internal error is not a refusal")
}

func TestAscRefusalIsCleanedAndBounded(t *testing.T) {
	r := ascRefusalOf(status.Error(codes.FailedPrecondition, "X_Y: a\x00b\x1b[31m\n"+string(make([]byte, 0))+"  c"))
	if assert.NotNil(t, r) {
		assert.NotContains(t, r.Message, "\x00")
		assert.NotContains(t, r.Message, "\n")
	}
	long := make([]byte, 2000)
	for i := range long {
		long[i] = 'a'
	}
	r = ascRefusalOf(status.Error(codes.FailedPrecondition, "CODE: "+string(long)))
	if assert.NotNil(t, r) {
		assert.LessOrEqual(t, len(r.Message), maxRefusalBytes+3)
	}
}

func TestAscRefusalOfPlainErrors(t *testing.T) {
	assert.Nil(t, ascRefusalOf(errors.New("plain")))
	assert.Nil(t, ascRefusalOf(status.Error(codes.Unavailable, "down")))
	assert.Nil(t, ascRefusalOf(status.Error(codes.FailedPrecondition, "")))
}
