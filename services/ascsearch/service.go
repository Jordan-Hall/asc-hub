// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// callTimeout bounds one search, one symbol call or one index status call.
	callTimeout = 30 * time.Second
	// fanOut bounds the parallel calls of a fallback or a chunked search.
	fanOut = 8
	// maxResponses bounds the stream messages read from one call.
	maxResponses = 64
)

// Service talks to asc. The context of every call says who asc believes is asking.
type Service struct {
	Cross ascv1.CrossSearchClient
	Intel ascv1.CodeIntelClient
}

// New binds a Service to a connection to asc serve (an *ascclient.Client).
func New(conn grpc.ClientConnInterface) *Service {
	return &Service{Cross: ascv1.NewCrossSearchClient(conn), Intel: ascv1.NewCodeIntelClient(conn)}
}

var (
	connMu  sync.Mutex
	connVal grpc.ClientConnInterface
	// newConn builds the connection of this process; tests replace it.
	newConn = func() (grpc.ClientConnInterface, error) {
		if !setting.Asc.Enabled || setting.Asc.ServerURL == "" {
			return nil, ErrDisabled
		}
		if setting.InternalToken == "" {
			return nil, errors.New("ascsearch: the internal token is not set")
		}
		return ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken})
	}
)

// ErrDisabled is returned while the asc integration is off.
var ErrDisabled = errors.New("ascsearch: the asc integration is not enabled")

// Default returns the Service of this process, connecting on first use.
func Default() (*Service, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if connVal == nil {
		c, err := newConn()
		if err != nil {
			return nil, err
		}
		connVal = c
	}
	return New(connVal), nil
}

// UseConn replaces the connection of this process and returns a function that restores the old one. Tests
// and wiring code use it.
func UseConn(c grpc.ClientConnInterface) (restore func()) {
	connMu.Lock()
	old := connVal
	connVal = c
	connMu.Unlock()
	return func() {
		connMu.Lock()
		connVal = old
		connMu.Unlock()
	}
}

// Kind classifies a failure for the caller, which maps it to a page or a status.
type Kind int

// The kinds of failure.
const (
	// KindFailed is any failure not listed below; the caller shows "not available right now".
	KindFailed Kind = iota
	// KindNoCredential means the call carried no credential: the caller answers 401.
	KindNoCredential
	// KindNotFound means asc does not know the repository or refuses the caller; both look the same.
	KindNotFound
	// KindBadRequest means asc refused the request itself.
	KindBadRequest
	// KindTooBroad means the pattern is shorter than 3 bytes or matches everything.
	KindTooBroad
	// KindLimited means asc rate limited the call.
	KindLimited
	// KindTimeout means the call took too long.
	KindTimeout
	// KindUnavailable means asc is down or lacks the feature.
	KindUnavailable
	// KindBadAnswer means asc answered something that fails a check.
	KindBadAnswer
)

// ErrBadAnswer marks an answer from asc that fails a check.
var ErrBadAnswer = errors.New("ascsearch: bad answer")

// KindOf classifies err. Only the status code and the ErrorDetail code of an asc error are used; its text
// is never looked at.
func KindOf(err error) Kind {
	switch {
	case err == nil:
		return KindFailed
	case errors.Is(err, ascclient.ErrNoCredential):
		return KindNoCredential
	case errors.Is(err, ErrBadAnswer):
		return KindBadAnswer
	case errors.Is(err, ErrInput), errors.Is(err, ErrBadSelector):
		return KindBadRequest
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	}
	switch status.Code(err) {
	case codes.NotFound, codes.PermissionDenied, codes.Unauthenticated:
		return KindNotFound
	case codes.InvalidArgument, codes.OutOfRange:
		if ascclient.ErrorCode(err) == "SEARCH_TOO_BROAD" {
			return KindTooBroad
		}
		return KindBadRequest
	case codes.ResourceExhausted:
		return KindLimited
	case codes.DeadlineExceeded:
		return KindTimeout
	case codes.Unavailable, codes.Unimplemented, codes.FailedPrecondition:
		return KindUnavailable
	}
	return KindFailed
}

// HTTPStatus is the status the API answers with for err.
func HTTPStatus(err error) int {
	switch KindOf(err) {
	case KindNoCredential:
		return http.StatusUnauthorized
	case KindNotFound:
		return http.StatusNotFound
	case KindBadRequest, KindTooBroad:
		return http.StatusUnprocessableEntity
	case KindLimited:
		return http.StatusTooManyRequests
	case KindTimeout:
		return http.StatusGatewayTimeout
	case KindUnavailable:
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

// PageStatus is the status a page answers with for err: a refused request is 400 and the rest as for the API.
func PageStatus(err error) int {
	switch KindOf(err) {
	case KindBadRequest, KindTooBroad:
		return http.StatusBadRequest
	case KindNoCredential:
		return http.StatusBadGateway // a page uses the internal token; a missing one is our fault
	}
	return HTTPStatus(err)
}

// PublicMessage is the text an API client or a page may show for err. It is fixed text, or the text of a
// check made here; the text of an asc error is never used.
func PublicMessage(err error) string {
	var in *inputError
	if errors.As(err, &in) {
		return in.msg
	}
	if errors.Is(err, ErrBadSelector) {
		return "bad repository or owner name"
	}
	switch KindOf(err) {
	case KindNoCredential:
		return "a token is required for code search"
	case KindNotFound:
		return "not found"
	case KindBadRequest:
		return "the request was refused"
	case KindTooBroad:
		return "the search pattern is too broad"
	case KindLimited:
		return "too many requests"
	case KindTimeout:
		return "the search took too long"
	case KindUnavailable:
		return "code search is unavailable"
	}
	return "code search failed"
}

// unimplemented reports that asc lacks the cross-repository service, so the caller falls back to
// one CodeIntel call per repository.
func unimplemented(err error) bool { return status.Code(err) == codes.Unimplemented }

// refused reports a failure that means "nothing from this repository": asc does not know it or refuses the
// caller. It adds nothing and counts nowhere, so a search cannot reveal that a repository exists.
func refused(err error) bool {
	switch status.Code(err) {
	case codes.NotFound, codes.PermissionDenied:
		return true
	}
	return false
}

// bad builds the error of an answer that fails a check.
func bad(msg string) error { return errors.Join(ErrBadAnswer, errors.New(msg)) }
