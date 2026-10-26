// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"forgejo.org/modules/ascclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestKindAndStatus(t *testing.T) {
	cases := []struct {
		err  error
		kind Kind
		http int
		page int
	}{
		{ascclient.ErrNoCredential, KindNoCredential, http.StatusUnauthorized, http.StatusBadGateway},
		{status.Error(codes.NotFound, "x"), KindNotFound, http.StatusNotFound, http.StatusNotFound},
		{status.Error(codes.PermissionDenied, "x"), KindNotFound, http.StatusNotFound, http.StatusNotFound},
		{status.Error(codes.Unauthenticated, "x"), KindNotFound, http.StatusNotFound, http.StatusNotFound},
		{status.Error(codes.InvalidArgument, "x"), KindBadRequest, http.StatusUnprocessableEntity, http.StatusBadRequest},
		{status.Error(codes.ResourceExhausted, "x"), KindLimited, http.StatusTooManyRequests, http.StatusTooManyRequests},
		{status.Error(codes.DeadlineExceeded, "x"), KindTimeout, http.StatusGatewayTimeout, http.StatusGatewayTimeout},
		{context.DeadlineExceeded, KindTimeout, http.StatusGatewayTimeout, http.StatusGatewayTimeout},
		{status.Error(codes.Unavailable, "x"), KindUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		{status.Error(codes.Unimplemented, "x"), KindUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		{status.Error(codes.Internal, "x"), KindFailed, http.StatusBadGateway, http.StatusBadGateway},
		{errors.New("plain"), KindFailed, http.StatusBadGateway, http.StatusBadGateway},
		{bad("x"), KindBadAnswer, http.StatusBadGateway, http.StatusBadGateway},
		{input("x"), KindBadRequest, http.StatusUnprocessableEntity, http.StatusBadRequest},
		{ErrBadSelector, KindBadRequest, http.StatusUnprocessableEntity, http.StatusBadRequest},
	}
	for _, c := range cases {
		assert.Equal(t, c.kind, KindOf(c.err), "%v", c.err)
		assert.Equal(t, c.http, HTTPStatus(c.err), "%v", c.err)
		assert.Equal(t, c.page, PageStatus(c.err), "%v", c.err)
	}
}

func TestDefaultConnection(t *testing.T) {
	restore := UseConn(nil)
	t.Cleanup(restore)
	old := newConn
	t.Cleanup(func() { newConn = old })

	_, err := Default()
	require.ErrorIs(t, err, ErrDisabled, "the integration is off in tests")

	calls := 0
	newConn = func() (grpc.ClientConnInterface, error) { calls++; return nil, errors.New("down") }
	_, err = Default()
	require.Error(t, err)
	_, err = Default()
	require.Error(t, err)
	assert.Equal(t, 2, calls, "a failed connection is retried, not remembered")
}

func TestPublicMessage(t *testing.T) {
	assert.Equal(t, "the pattern must be 3 to 512 bytes without control characters", PublicMessage(input("the pattern must be 3 to 512 bytes without control characters")))
	assert.Equal(t, "bad repository or owner name", PublicMessage(ErrBadSelector))
	assert.Equal(t, "not found", PublicMessage(status.Error(codes.NotFound, "secret/internal path /srv/x")))
	assert.Equal(t, "code search failed", PublicMessage(status.Error(codes.Internal, "panic at /srv/x:12")))
	assert.Equal(t, "the request was refused", PublicMessage(errors.Join(ErrInput)))
	for _, err := range []error{
		status.Error(codes.Internal, "leak-me"), status.Error(codes.Unavailable, "leak-me"), status.Error(codes.ResourceExhausted, "leak-me"),
		status.Error(codes.DeadlineExceeded, "leak-me"), status.Error(codes.InvalidArgument, "leak-me"), ascclient.ErrNoCredential,
	} {
		assert.NotContains(t, PublicMessage(err), "leak-me")
	}
}

func TestChangeBytes(t *testing.T) {
	b, err := ChangeBytes("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 16)[:0], b[:0])
	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, b, "k is hex f")
	b, err = ChangeBytes("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 16), b)
	for _, bad := range []string{"", "abc", "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK"} {
		_, err = ChangeBytes(bad)
		require.ErrorIs(t, err, ErrBadChange, bad)
	}

	s, err := AtChange("o/r", "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", false, false)
	require.NoError(t, err)
	assert.NotNil(t, s.GetChange())
	assert.Nil(t, s.GetStackTop())
	s, err = AtChange("o/r", "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", true, true)
	require.NoError(t, err)
	assert.NotNil(t, s.GetStackTop())
	assert.True(t, s.GetStackDiffOnly())
	_, err = AtChange("o/r", "nope", false, false)
	require.Error(t, err)
}
