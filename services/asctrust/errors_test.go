// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassifyKindsAndStatuses(t *testing.T) {
	cases := []struct {
		code   codes.Code
		kind   Kind
		status int
	}{
		{codes.NotFound, KindNotFound, http.StatusNotFound},
		{codes.PermissionDenied, KindDenied, http.StatusForbidden},
		{codes.Unauthenticated, KindDenied, http.StatusForbidden},
		{codes.InvalidArgument, KindInvalid, http.StatusBadRequest},
		{codes.FailedPrecondition, KindConflict, http.StatusConflict},
		{codes.Aborted, KindConflict, http.StatusConflict},
		{codes.AlreadyExists, KindConflict, http.StatusConflict},
		{codes.ResourceExhausted, KindLimit, http.StatusTooManyRequests},
		{codes.Unavailable, KindUnavailable, http.StatusServiceUnavailable},
		{codes.DeadlineExceeded, KindUnavailable, http.StatusServiceUnavailable},
		{codes.Unimplemented, KindUnimplemented, http.StatusServiceUnavailable},
		{codes.Internal, KindOther, http.StatusBadGateway},
		{codes.Unknown, KindOther, http.StatusBadGateway},
	}
	for _, c := range cases {
		e := Classify(status.Error(c.code, "text that must never be shown"))
		require.NotNil(t, e, c.code.String())
		assert.Equal(t, c.kind, e.Kind, c.code.String())
		assert.Equal(t, c.status, e.Status(), c.code.String())
		assert.NotContains(t, e.Message(), "never be shown", c.code.String())
		assert.NotContains(t, e.Error(), "never be shown", c.code.String())
		assert.NotEmpty(t, e.Message())
	}
	assert.Nil(t, Classify(nil))
}

func TestClassifyContextAndForeignErrors(t *testing.T) {
	assert.Equal(t, KindUnavailable, Classify(context.DeadlineExceeded).Kind)
	assert.Equal(t, KindUnavailable, Classify(fmt.Errorf("wrapped: %w", context.Canceled)).Kind)
	assert.Equal(t, KindOther, Classify(errors.New("some other error")).Kind)
	e := &Error{Kind: KindDenied, Code: "POLICY_DENIED"}
	assert.Same(t, e, Classify(fmt.Errorf("wrapped: %w", e)), "an already classified error is kept")
}

func TestClassifyReadsTheCodeOfErrorDetail(t *testing.T) {
	mk := func(code string) error {
		st, err := status.New(codes.FailedPrecondition, "x").WithDetails(&ascv1.ErrorDetail{Code: code})
		require.NoError(t, err)
		return st.Err()
	}
	e := Classify(mk("POLICY_LOCKOUT"))
	assert.Equal(t, "POLICY_LOCKOUT", e.Code)
	assert.Contains(t, e.Message(), "nobody who could change it")
	assert.Contains(t, e.Error(), "POLICY_LOCKOUT")

	assert.Contains(t, Classify(mk("VERSION_CONFLICT")).Message(), "trunk moved")
	assert.Contains(t, Classify(mk("SIGNING_KEY_LIMIT")).Message(), "limit")

	// A code that is not a slug is dropped, so it can never reach a page or a log.
	for _, bad := range []string{"lower_case", "HAS SPACE", "<script>", "X" + string(make([]byte, 80)), ""} {
		assert.Empty(t, Classify(mk(bad)).Code, "%q", bad)
	}
	// An unknown but well-formed code falls back to the sentence of its kind.
	e = Classify(mk("SOMETHING_NEW"))
	assert.Equal(t, "SOMETHING_NEW", e.Code)
	assert.Equal(t, "That is not possible in the current state. Reload the page and try again.", e.Message())
}
