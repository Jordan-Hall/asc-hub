// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNoCredential is returned when a call carries neither a user token nor WithSystem. The client
// never falls back to the internal token on its own, so a missing user token cannot silently
// widen a user-initiated call into a system call.
var ErrNoCredential = errors.New("ascclient: no credential in context (use WithUserToken or WithSystem)")

// ErrNoInternalToken is returned by a system call when the client has no internal token.
var ErrNoInternalToken = errors.New("ascclient: system call without an internal token")

type (
	credKey struct{}
	opIDKey struct{}
)

// credential is what a call authenticates as.
type credential struct {
	system bool
	token  string
}

// WithUserToken marks the calls made with ctx as user-initiated: the user's own token is
// forwarded, so asc authorises the call as that user.
func WithUserToken(ctx context.Context, token string) (context.Context, error) {
	if err := validateToken(token); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, credKey{}, credential{token: token}), nil
}

// WithSystem marks the calls made with ctx as system calls, which carry the internal token.
func WithSystem(ctx context.Context) context.Context {
	return context.WithValue(ctx, credKey{}, credential{system: true})
}

// WithOpID fixes the x-asc-op-id of a mutating call. Without it the client generates a UUIDv7 once
// per logical call and reuses it across retries.
func WithOpID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, opIDKey{}, id)
}

// validateToken accepts visible ASCII only. That keeps a token usable as an HTTP/2 header value
// and refuses CR, LF, NUL and spaces, which would allow header injection.
func validateToken(token string) error {
	if token == "" {
		return errors.New("empty token")
	}
	if len(token) > MaxTokenBytes {
		return errors.New("token too long")
	}
	for i := range len(token) {
		if c := token[i]; c < 0x21 || c > 0x7e {
			return errors.New("token has a character that is not visible ASCII")
		}
	}
	return nil
}

func (c *Client) bearer(ctx context.Context) (string, error) {
	cred, ok := ctx.Value(credKey{}).(credential)
	switch {
	case !ok:
		return "", ErrNoCredential
	case cred.system:
		if c.cfg.InternalToken == "" {
			return "", ErrNoInternalToken
		}
		return c.cfg.InternalToken, nil
	default:
		return cred.token, nil
	}
}
