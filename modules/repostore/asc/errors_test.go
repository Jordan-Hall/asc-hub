// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package asc

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
)

func TestPolicyRefusal(t *testing.T) {
	wrapped := fmt.Errorf("merge: %w", &Error{Op: "UpdateRefs", Code: codes.PermissionDenied, Detail: "POLICY_DENIED", Msg: "POLICY_DENIED: 0 of 1 approvals"})
	msg, ok := PolicyRefusal(wrapped)
	assert.True(t, ok)
	assert.Equal(t, "POLICY_DENIED: 0 of 1 approvals", msg)

	_, ok = PolicyRefusal(&Error{Op: "UpdateRefs", Code: codes.Unavailable, Msg: "down"})
	assert.False(t, ok, "an outage is not a refusal")
	_, ok = PolicyRefusal(&Error{Op: "UpdateRefs", Code: codes.PermissionDenied})
	assert.False(t, ok, "a refusal without a sentence says nothing")
	_, ok = PolicyRefusal(fmt.Errorf("other"))
	assert.False(t, ok)
}
