// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

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

func withDetail(t *testing.T, c codes.Code, msg string, d *ascv1.ErrorDetail) error {
	t.Helper()
	st, err := status.New(c, msg).WithDetails(d)
	require.NoError(t, err)
	return st.Err()
}

func TestClassifyUsesCodesNeverText(t *testing.T) {
	const planted = "sk-planted-secret-in-the-provider-error"

	t.Run("nil", func(t *testing.T) {
		assert.Nil(t, Classify(nil))
	})

	t.Run("by ErrorDetail code", func(t *testing.T) {
		for code, want := range map[string]struct {
			http     int
			disabled bool
		}{
			"AGENTS_DISABLED":     {http.StatusConflict, true},
			"WORKFLOW_DISABLED":   {http.StatusConflict, true},
			"AGENT_TOKEN_EXPIRED": {http.StatusForbidden, false},
			"AGENT_TOKEN_REVOKED": {http.StatusForbidden, false},
			"AGENT_SCOPE_DENIED":  {http.StatusForbidden, false},
			"BUDGET_EXCEEDED":     {http.StatusTooManyRequests, false},
			"RUN_NOT_ACTIVE":      {http.StatusConflict, false},
			"AI_KEY_UNTESTED":     {http.StatusConflict, false},
			"AI_CAP_REACHED":      {http.StatusTooManyRequests, false},
			"WORKFLOW_INVALID":    {http.StatusBadRequest, false},
		} {
			f := Classify(withDetail(t, codes.FailedPrecondition, planted, &ascv1.ErrorDetail{Code: code}))
			require.NotNil(t, f, code)
			assert.Equal(t, code, f.Code)
			assert.Equal(t, want.http, f.HTTP, code)
			assert.Equal(t, want.disabled, f.Disabled, code)
			assert.NotContains(t, f.Message, planted, code)
			assert.NotEmpty(t, f.Message)
			assert.False(t, f.Unavailable)
		}
	})

	t.Run("scope and workflow details are cleaned and kept", func(t *testing.T) {
		f := Classify(withDetail(t, codes.PermissionDenied, planted, &ascv1.ErrorDetail{
			Code:  "AGENT_SCOPE_DENIED",
			Paths: []string{"infra/limits.tf", "src/\x1b[31m.rs", ""},
		}))
		assert.Equal(t, []string{"infra/limits.tf", "src/[31m.rs"}, f.Details)

		f = Classify(withDetail(t, codes.InvalidArgument, planted, &ascv1.ErrorDetail{Code: "WORKFLOW_INVALID", Paths: []string{"12: unknown key"}}))
		assert.Equal(t, []string{"12: unknown key"}, f.Details)

		// Details of other codes are not passed on.
		f = Classify(withDetail(t, codes.NotFound, planted, &ascv1.ErrorDetail{Code: "RUN_NOT_ACTIVE", Paths: []string{"x"}}))
		assert.Empty(t, f.Details)
	})

	t.Run("budget names which limit", func(t *testing.T) {
		for which, want := range map[string]string{
			"changes": "The limit on changes is reached.",
			"tokens":  "The limit on model tokens is reached.",
			"cost":    "The cost limit is reached.",
			"paths":   "The limit on written paths is reached.",
			"runtime": "The time limit is reached.",
			"other":   "A budget is reached.",
		} {
			f := Classify(withDetail(t, codes.ResourceExhausted, planted, &ascv1.ErrorDetail{Code: "BUDGET_EXCEEDED", Paths: []string{which}}))
			assert.Equal(t, want, f.Message, which)
			assert.Empty(t, f.Details)
		}
	})

	t.Run("an unknown or unsafe code is dropped, the grpc code decides", func(t *testing.T) {
		f := Classify(withDetail(t, codes.NotFound, planted, &ascv1.ErrorDetail{Code: "<script>alert(1)</script>"}))
		assert.Empty(t, f.Code)
		assert.Equal(t, http.StatusNotFound, f.HTTP)
		assert.NotContains(t, f.Message, "script")
	})

	t.Run("by grpc code", func(t *testing.T) {
		for c, want := range map[codes.Code]int{
			codes.NotFound:           http.StatusNotFound,
			codes.InvalidArgument:    http.StatusBadRequest,
			codes.PermissionDenied:   http.StatusForbidden,
			codes.Unauthenticated:    http.StatusForbidden,
			codes.AlreadyExists:      http.StatusConflict,
			codes.Aborted:            http.StatusConflict,
			codes.FailedPrecondition: http.StatusConflict,
			codes.ResourceExhausted:  http.StatusTooManyRequests,
			codes.DeadlineExceeded:   http.StatusGatewayTimeout,
			codes.Canceled:           http.StatusRequestTimeout,
			codes.Unimplemented:      http.StatusNotImplemented,
			codes.Unavailable:        http.StatusBadGateway,
			codes.Internal:           http.StatusBadGateway,
		} {
			f := Classify(status.Error(c, planted))
			assert.Equal(t, want, f.HTTP, c.String())
			assert.NotContains(t, f.Message, planted, c.String())
			assert.Empty(t, f.Details)
		}
		assert.True(t, Classify(status.Error(codes.Unavailable, "x")).Unavailable)
		assert.True(t, Classify(status.Error(codes.Unimplemented, "x")).Disabled, "a server without the feature is explained, not a fault")
		assert.False(t, Classify(status.Error(codes.NotFound, "x")).Unavailable)
	})

	t.Run("not a status", func(t *testing.T) {
		f := Classify(errors.New(planted))
		assert.Equal(t, http.StatusBadGateway, f.HTTP)
		assert.NotContains(t, f.Message, planted)
		assert.True(t, f.Unavailable)

		f = Classify(fmt.Errorf("wrapped: %w", context.DeadlineExceeded))
		assert.Equal(t, http.StatusGatewayTimeout, f.HTTP)
		f = Classify(context.Canceled)
		assert.Equal(t, http.StatusRequestTimeout, f.HTTP)
		assert.False(t, f.Unavailable)

		f = Classify(fmt.Errorf("checking: %w", ErrBadAnswer))
		assert.Equal(t, http.StatusBadGateway, f.HTTP)
		assert.True(t, f.Unavailable)
	})
}
